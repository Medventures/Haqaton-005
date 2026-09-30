package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed openapi.yaml
var openapiSpec []byte

type App struct {
	db        *pgxpool.Pool
	cat       *Catalog
	triage    *TriageRules
	llm       *LLM
	kb        *Knowledge
	questions *QuestionBank
	secret    []byte
	// askFirst: one clarifying question before showing urgency/services (ASK_FIRST=false disables)
	askFirst bool
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func main() {
	cat, err := LoadCatalog(env("CATALOG_PATH", "../catalog.json"))
	if err != nil {
		log.Fatalf("catalog: %v", err)
	}
	tr, err := LoadTriage(env("TRIAGE_PATH", "../triage_rules.json"))
	if err != nil {
		log.Fatalf("triage rules: %v", err)
	}
	kb, err := LoadKnowledge(env("KNOWLEDGE_PATH", "../knowledge.json"))
	if err != nil {
		log.Fatalf("knowledge base: %v", err)
	}
	qb, err := LoadQuestions(env("QUESTIONS_PATH", "../questions.json"))
	if err != nil {
		log.Fatalf("questions: %v", err)
	}
	db, err := connectDB(context.Background(), env("DATABASE_URL", "postgres://clinic:clinic@localhost:5432/clinic?sslmode=disable"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	llm := NewLLM(env("LLM_BASE_URL", "http://host.docker.internal:1234/v1"), env("LLM_MODEL", ""))
	llm.ModelKK = os.Getenv("LLM_MODEL_KK")
	a := &App{db: db, cat: cat, triage: tr, kb: kb, questions: qb,
		llm: llm, askFirst: env("ASK_FIRST", "true") != "false",
		secret: secretFromEnv()}
	operators = loadOperators()
	if err := a.hideBookedSlots(context.Background()); err != nil {
		log.Fatalf("appointments: %v", err)
	}
	log.Printf("catalog: %d specialties, %d services, %d doctors; %d red rules; model %s, kk answers %q",
		len(cat.Specialties), len(cat.Services), len(cat.Doctors), len(tr.Red), a.llm.Model, a.llm.ModelKK)
	log.Printf("knowledge base: %d entries", len(kb.Entries))

	r := a.routes()
	addr := ":" + env("PORT", "8080")
	log.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}

func (a *App) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openapiSpec)
	})
	r.Get("/api/catalog", a.catalogHandler)
	r.Post("/api/auth/patient", a.patientToken)
	r.Post("/api/auth/login", a.login)
	r.With(a.auth("patient")).Post("/api/chat", a.chat)
	r.With(a.auth("patient")).Post("/api/chat/operator", a.requestOperator)
	r.With(a.auth("patient", "operator")).Get("/api/dialogs/{id}", a.getDialogHandler)
	r.With(a.auth("patient")).Post("/api/chat/attachments", a.uploadAttachment)
	r.With(a.auth("patient", "operator")).Get("/api/attachments/{id}", a.getAttachment)
	r.With(a.auth("operator")).Get("/api/operator/queue", a.queue)
	r.With(a.auth("operator")).Post("/api/operator/reply", a.operatorReply)
	r.With(a.auth("patient")).Post("/api/appointments", a.createAppointment)
	r.With(a.auth("patient", "operator")).Get("/api/appointments", a.listAppointments)
	r.With(a.auth("patient", "operator")).Post("/api/appointments/{id}/cancel", a.cancelAppointment)
	r.With(integrationAuth).Put("/api/integration/catalog", a.integrationCatalog)
	r.With(integrationAuth).Put("/api/integration/doctors/{id}/slots", a.integrationDoctorSlots)
	r.With(integrationAuth).Get("/api/integration/appointments", a.integrationAppointments)

	// Optional: serve the built frontend (SPA) from the same process, e.g. STATIC_DIR=../frontend/dist.
	if dir := os.Getenv("STATIC_DIR"); dir != "" {
		files := http.FileServer(http.Dir(dir))
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeErr(w, 404, "not found")
				return
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.Clean(r.URL.Path))); err != nil {
				http.ServeFile(w, r, filepath.Join(dir, "index.html")) // SPA routes like /operator
				return
			}
			files.ServeHTTP(w, r)
		})
	}

	return r
}
