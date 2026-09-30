package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed openapi.yaml
var openapiSpec []byte

type App struct {
	db     *pgxpool.Pool
	cat    *Catalog
	triage *TriageRules
	llm    *LLM
	secret []byte
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
	db, err := connectDB(context.Background(), env("DATABASE_URL", "postgres://clinic:clinic@localhost:5432/clinic?sslmode=disable"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	a := &App{db: db, cat: cat, triage: tr,
		llm:    NewLLM(env("LLM_BASE_URL", "http://host.docker.internal:1234/v1"), env("LLM_MODEL", "")),
		secret: []byte(env("JWT_SECRET", "change-me-hackathon"))}
	log.Printf("catalog: %d specialties, %d services, %d doctors; %d red rules; model %s",
		len(cat.Specialties), len(cat.Services), len(cat.Doctors), len(tr.Red), a.llm.Model)

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
	r.Get("/api/catalog", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.cat) })
	r.Post("/api/auth/patient", a.patientToken)
	r.Post("/api/auth/login", a.login)
	r.With(a.auth("patient")).Post("/api/chat", a.chat)
	r.With(a.auth("patient")).Post("/api/chat/operator", a.requestOperator)
	r.With(a.auth("patient", "operator")).Get("/api/dialogs/{id}", a.getDialogHandler)
	r.With(a.auth("operator")).Get("/api/operator/queue", a.queue)
	r.With(a.auth("operator")).Post("/api/operator/reply", a.operatorReply)

	return r
}
