package main

// Chat pipeline tests: real Postgres (TEST_DATABASE_URL, default = compose db on :55433,
// database clinic_test) + a fake OpenAI-compatible LLM server with scripted replies.
// Skipped if Postgres is not reachable: run `docker compose up -d db` first.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeLLM answers /chat/completions: extraction requests (with response_format) pop from
// extractions, answer requests get a fixed text.
type fakeLLM struct {
	mu          sync.Mutex
	extractions []string
	extractN    int
	answerN     int
	models      []string // model per request: "extract:<m>" / "answer:<m>"
}

func (f *fakeLLM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	defer f.mu.Unlock()
	content := "Рекомендуем обратиться к специалисту."
	m, _ := body["model"].(string)
	if _, ok := body["response_format"]; ok {
		f.models = append(f.models, "extract:"+m)
		f.extractN++
		content = "not json"
		if len(f.extractions) > 0 {
			content, f.extractions = f.extractions[0], f.extractions[1:]
		}
	} else {
		f.models = append(f.models, "answer:"+m)
		f.answerN++
	}
	json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}}})
}

func (f *fakeLLM) calls() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.extractN, f.answerN
}

// exWith patches an extraction JSON with extra fields (pregnant, ask_pregnancy, ...).
func exWith(base string, extra map[string]any) string {
	var m map[string]any
	json.Unmarshal([]byte(base), &m)
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// ex builds an extraction JSON; spec "" means null.
func ex(intent, spec string, clarify bool, question, urgency string) string {
	m := map[string]any{"language": "ru", "intent": intent, "specialty_id": nil, "need_clarification": clarify,
		"clarifying_question": nil, "urgency": urgency, "urgency_reason": "тест", "summary": "summary: " + intent,
		"pregnant": nil, "gestation_weeks": nil, "ask_pregnancy": false, "risk_factors": []string{}}
	if spec != "" {
		m["specialty_id"] = spec
	}
	if question != "" {
		m["clarifying_question"] = question
	}
	b, _ := json.Marshal(m)
	return string(b)
}

type testEnv struct {
	t   *testing.T
	a   *App
	llm *fakeLLM
	srv *httptest.Server
}

func setup(t *testing.T, extractions ...string) *testEnv {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	admin := env("TEST_ADMIN_URL", "postgres://clinic:clinic@localhost:55433/clinic?sslmode=disable")
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Skipf("postgres not reachable (%v): run `docker compose up -d db`", err)
	}
	conn.Exec(ctx, "create database clinic_test") // error = already exists
	conn.Close(ctx)

	db, err := pgxpool.New(ctx, env("TEST_DATABASE_URL", "postgres://clinic:clinic@localhost:55433/clinic_test?sslmode=disable"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, schema+"; truncate dialogs cascade;"); err != nil {
		t.Fatal(err)
	}
	cat, err := LoadCatalog("../catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := LoadTriage("../triage_rules.json")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeLLM{extractions: extractions}
	llmSrv := httptest.NewServer(f)
	a := &App{db: db, cat: cat, triage: tr, kb: &Knowledge{}, llm: NewLLM(llmSrv.URL, "fake"), secret: []byte("test")}
	srv := httptest.NewServer(a.routes())
	t.Cleanup(func() { srv.Close(); llmSrv.Close(); db.Close() })
	return &testEnv{t, a, f, srv}
}

func (e *testEnv) do(method, path, tok string, body any, out any) int {
	e.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (e *testEnv) patient() string { return e.a.issue("p-"+e.t.Name()+time.Now().String(), "patient") }
func (e *testEnv) operator() string {
	return e.a.issue("operator1", "operator")
}

func (e *testEnv) chat(tok, dialogID, msg string) ChatResponse {
	e.t.Helper()
	var r ChatResponse
	if code := e.do("POST", "/api/chat", tok, ChatRequest{dialogID, msg}, &r); code != 200 {
		e.t.Fatalf("chat %q: status %d", msg, code)
	}
	return r
}

func (e *testEnv) openTickets(dialogID string) int {
	var n int
	e.a.db.QueryRow(context.Background(), `select count(*) from tickets where dialog_id=$1 and status='open'`, dialogID).Scan(&n)
	return n
}

func has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestRedFlagSkipsLLMAndGoesToOperator(t *testing.T) {
	e := setup(t)
	tok := e.patient()
	r := e.chat(tok, "", "У меня сильная боль в груди")
	if n, m := e.llm.calls(); n+m != 0 {
		t.Fatalf("LLM must not be called on red, got %d+%d calls", n, m)
	}
	if r.Urgency != "red" || r.Status != "operator" || !has(r.Actions, "call_103") || !strings.Contains(r.Reply.Content, "103") {
		t.Fatalf("unexpected red response: %+v", r)
	}
	if len(r.Services) != 0 || r.TicketID == nil || e.openTickets(r.DialogID) != 1 {
		t.Fatalf("red must stop matching and create one ticket: %+v", r)
	}
}

func TestRedFlagInKazakhAnswersInKazakh(t *testing.T) {
	e := setup(t)
	r := e.chat(e.patient(), "", "Кеудем қатты ауырып тұр")
	if r.Urgency != "red" || r.Language != "kk" || !strings.Contains(r.Reply.Content, "103-ке") {
		t.Fatalf("kk red: %+v", r)
	}
}

func TestRedFlagOnClarificationAnswer(t *testing.T) {
	e := setup(t, ex("find_service", "", true, "Что беспокоит?", "green"))
	tok := e.patient()
	r1 := e.chat(tok, "", "Мне плохо")
	if r1.Reply.Content != "Что беспокоит?" || r1.Urgency != "green" {
		t.Fatalf("expected clarification: %+v", r1)
	}
	r2 := e.chat(tok, r1.DialogID, "стало трудно дышать")
	if r2.Urgency != "red" || !has(r2.Actions, "call_103") {
		t.Fatalf("red must fire on clarification answer: %+v", r2)
	}
	if n, _ := e.llm.calls(); n != 1 {
		t.Fatalf("second message must not reach LLM, extraction calls = %d", n)
	}
}

func TestSpecialtyReturnsCatalogServicesAndDoctorsWithSlots(t *testing.T) {
	e := setup(t, ex("find_service", "ophthalmologist", false, "", "green"))
	r := e.chat(e.patient(), "", "плохо вижу вдаль")
	if r.Status != "bot" || r.Urgency != "green" || len(r.Services) == 0 {
		t.Fatalf("expected services: %+v", r)
	}
	for _, s := range r.Services {
		if s.SpecialtyID != "ophthalmologist" {
			t.Fatalf("foreign service %+v", s)
		}
	}
	// the only ophthalmologist in catalog.json has no free slots
	if len(r.Doctors) != 0 {
		t.Fatalf("doctors without slots must be hidden: %+v", r.Doctors)
	}
	if _, m := e.llm.calls(); m != 1 {
		t.Fatalf("answer must be phrased by LLM once, got %d", m)
	}
	var data botData
	json.Unmarshal(r.Reply.Data, &data)
	if len(data.Services) != len(r.Services) {
		t.Fatalf("cards must be stored in message data for history: %s", r.Reply.Data)
	}
}

func TestUnknownSpecialtyIsIgnored(t *testing.T) {
	e := setup(t, ex("find_service", "cosmetologist", false, "", "green"))
	r := e.chat(e.patient(), "", "нужен косметолог")
	if len(r.Services) != 0 {
		t.Fatalf("specialty outside catalog must not produce services: %+v", r.Services)
	}
}

func TestMaxQuestionsThenHandoff(t *testing.T) {
	q := ex("find_service", "", true, "Уточните?", "green")
	e := setup(t, q, q, q, q)
	tok := e.patient()
	r := e.chat(tok, "", "плохо")
	r = e.chat(tok, r.DialogID, "очень плохо")
	r = e.chat(tok, r.DialogID, "совсем плохо")
	if r.Reply.Content != "Уточните?" || r.Status != "bot" {
		t.Fatalf("third clarification expected: %+v", r)
	}
	r = e.chat(tok, r.DialogID, "не знаю")
	if r.Status != "operator" || r.TicketID == nil {
		t.Fatalf("after %d clarifications without specialty must hand off: %+v", maxQuestions, r)
	}
	if d, _ := e.a.getDialog(context.Background(), r.DialogID); d.Clarifications != maxQuestions {
		t.Fatalf("clarifications = %d", d.Clarifications)
	}
}

func TestClarificationLimitThenPicksSpecialty(t *testing.T) {
	e := setup(t,
		ex("find_service", "", true, "Где болит?", "green"),
		ex("find_service", "", true, "Как давно?", "green"),
		ex("find_service", "", true, "Тошнота есть?", "green"),
		ex("find_service", "gastroenterologist", true, "Ещё вопрос?", "green"))
	tok := e.patient()
	r := e.chat(tok, "", "болит")
	r = e.chat(tok, r.DialogID, "живот")
	r = e.chat(tok, r.DialogID, "неделю")
	r = e.chat(tok, r.DialogID, "нет")
	if r.Status != "bot" || len(r.Services) == 0 || r.Services[0].SpecialtyID != "gastroenterologist" {
		t.Fatalf("after the limit the specialty is picked, no more questions: %+v", r)
	}
}

func TestInvalidJSONRetriesOnceThenSafeFallback(t *testing.T) {
	e := setup(t, "not json", "{broken")
	r := e.chat(e.patient(), "", "болит горло")
	if n, m := e.llm.calls(); n != 2 || m != 0 {
		t.Fatalf("expected exactly 2 extraction attempts and no answer call, got %d/%d", n, m)
	}
	if r.Urgency != "yellow" || r.Status != "operator" || r.TicketID == nil {
		t.Fatalf("fallback must be yellow + operator: %+v", r)
	}
}

func TestRetrySucceedsOnSecondAttempt(t *testing.T) {
	e := setup(t, "oops", ex("find_service", "ent", false, "", "green"))
	r := e.chat(e.patient(), "", "болит горло")
	if r.Status != "bot" || len(r.Services) == 0 {
		t.Fatalf("second attempt should be used: %+v", r)
	}
}

func TestYellowOffersOperatorAndShowsServices(t *testing.T) {
	e := setup(t, ex("find_service", "therapist", false, "", "yellow"))
	r := e.chat(e.patient(), "", "температура 39 третий день")
	if r.Urgency != "yellow" || !has(r.Actions, "contact_operator") || len(r.Services) == 0 {
		t.Fatalf("yellow: %+v", r)
	}
	if !strings.HasPrefix(r.Reply.Content, texts["yellow"]["ru"]) {
		t.Fatalf("yellow warning must come first: %q", r.Reply.Content)
	}
}

func TestUrgencyNeverGoesDown(t *testing.T) {
	e := setup(t, ex("find_service", "therapist", false, "", "yellow"), ex("find_service", "therapist", false, "", "green"))
	tok := e.patient()
	r := e.chat(tok, "", "температура 39")
	r = e.chat(tok, r.DialogID, "а сколько стоит анализ крови?")
	if r.Urgency != "yellow" {
		t.Fatalf("urgency went down to %q", r.Urgency)
	}
}

func TestOperatorIntentHandsOffWithSummary(t *testing.T) {
	e := setup(t, ex("operator", "", false, "", "green"))
	r := e.chat(e.patient(), "", "соедините с человеком")
	if r.Status != "operator" || r.TicketID == nil {
		t.Fatalf("operator intent: %+v", r)
	}
	var q []Ticket
	e.do("GET", "/api/operator/queue", e.operator(), nil, &q)
	if len(q) != 1 || q[0].Summary != "summary: operator" || q[0].Reason == "" {
		t.Fatalf("ticket must carry summary and reason: %+v", q)
	}
}

func TestBotSilentWhileWithOperator(t *testing.T) {
	e := setup(t, ex("operator", "", false, "", "green"))
	tok := e.patient()
	r := e.chat(tok, "", "оператора")
	r = e.chat(tok, r.DialogID, "алло?")
	if r.Reply != nil {
		t.Fatalf("bot must be silent while operator handles dialog: %+v", r.Reply)
	}
	if n, _ := e.llm.calls(); n != 1 {
		t.Fatalf("LLM must not be called while with operator, extraction calls = %d", n)
	}
	if e.openTickets(r.DialogID) != 1 {
		t.Fatal("must keep a single open ticket")
	}
}

func TestQueueOrderRedYellowGreen(t *testing.T) {
	e := setup(t, ex("operator", "", false, "", "green"), ex("operator", "", false, "", "yellow"))
	e.chat(e.patient(), "", "позовите оператора")    // green
	e.chat(e.patient(), "", "оператора, пожалуйста") // yellow
	e.chat(e.patient(), "", "потерял сознание")      // red, created last
	var q []Ticket
	e.do("GET", "/api/operator/queue", e.operator(), nil, &q)
	got := []string{}
	for _, t := range q {
		got = append(got, t.Urgency)
	}
	if strings.Join(got, ",") != "red,yellow,green" {
		t.Fatalf("queue order = %v", got)
	}
}

func TestAccessControl(t *testing.T) {
	e := setup(t, ex("find_service", "ent", false, "", "green"))
	owner, other, op := e.patient(), e.patient(), e.operator()
	r := e.chat(owner, "", "болит горло")

	if code := e.do("GET", "/api/dialogs/"+r.DialogID, owner, nil, nil); code != 200 {
		t.Fatalf("owner: %d", code)
	}
	if code := e.do("GET", "/api/dialogs/"+r.DialogID, other, nil, nil); code != 404 {
		t.Fatalf("other patient must not see dialog: %d", code)
	}
	if code := e.do("POST", "/api/chat", other, ChatRequest{r.DialogID, "привет"}, nil); code != 404 {
		t.Fatalf("other patient must not write into dialog: %d", code)
	}
	if code := e.do("GET", "/api/dialogs/"+r.DialogID, op, nil, nil); code != 404 {
		t.Fatalf("operator must not see a dialog that was not handed off: %d", code)
	}
	if code := e.do("GET", "/api/operator/queue", owner, nil, nil); code != 403 {
		t.Fatalf("patient must not read queue: %d", code)
	}
	if code := e.do("POST", "/api/chat", "", ChatRequest{"", "x"}, nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	if code := e.do("POST", "/api/chat", owner, ChatRequest{"", "   "}, nil); code != 400 {
		t.Fatalf("empty message: %d", code)
	}
}

func TestOperatorReplyAndClose(t *testing.T) {
	e := setup(t, ex("operator", "", false, "", "yellow"))
	tok, op := e.patient(), e.operator()
	r := e.chat(tok, "", "оператор")

	var out map[string]any
	if code := e.do("POST", "/api/operator/reply", op, map[string]any{"dialog_id": r.DialogID, "message": "Здравствуйте!", "close": true}, &out); code != 200 {
		t.Fatalf("reply: %d", code)
	}
	var d struct {
		Dialog   Dialog    `json:"dialog"`
		Messages []Message `json:"messages"`
	}
	e.do("GET", "/api/dialogs/"+r.DialogID, tok, nil, &d)
	last := d.Messages[len(d.Messages)-1]
	if last.Role != "operator" || last.Content != "Здравствуйте!" {
		t.Fatalf("patient must see operator reply: %+v", last)
	}
	if d.Dialog.Status != "bot" || d.Dialog.Urgency != "yellow" || e.openTickets(r.DialogID) != 0 {
		t.Fatalf("close: back to bot, ticket closed, urgency kept: %+v", d.Dialog)
	}
}

func TestContactOperatorButton(t *testing.T) {
	e := setup(t, ex("find_service", "therapist", false, "", "yellow"))
	tok := e.patient()
	r := e.chat(tok, "", "температура 39")
	var out map[string]any
	if code := e.do("POST", "/api/chat/operator", tok, map[string]string{"dialog_id": r.DialogID}, &out); code != 200 {
		t.Fatalf("contact operator: %d", code)
	}
	if out["status"] != "operator" || e.openTickets(r.DialogID) != 1 {
		t.Fatalf("button must create a ticket: %+v", out)
	}
}

func (e *testEnv) dialog(id string) *Dialog {
	e.t.Helper()
	d, err := e.a.getDialog(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

// Pain first, pregnancy later: red must fire on the second message via the whole history.
func TestPregnancyLaterMakesEarlierPainRed(t *testing.T) {
	e := setup(t, exWith(ex("find_service", "gynecologist", false, "", "green"), map[string]any{"ask_pregnancy": true}))
	tok := e.patient()
	r := e.chat(tok, "", "тянет низ живота второй день")
	if r.Urgency == "red" {
		t.Fatalf("without pregnancy this is not red: %+v", r)
	}
	if r.Reply.Content != texts["ask_pregnancy"]["ru"] {
		t.Fatalf("expected pregnancy question, got %q", r.Reply.Content)
	}
	r = e.chat(tok, r.DialogID, "да, я на 32 неделе")
	if r.Urgency != "red" || !has(r.Actions, "call_103") || !has(r.Actions, "urgent_operator") {
		t.Fatalf("pregnant + earlier pain must be red with two buttons: %+v", r)
	}
	if n, _ := e.llm.calls(); n != 1 {
		t.Fatalf("red must be decided before LLM on the second message, extraction calls = %d", n)
	}
	d := e.dialog(r.DialogID)
	if !d.Pregnant || d.GestationWeeks == nil || *d.GestationWeeks != 32 {
		t.Fatalf("pregnancy flag/weeks: %+v", d)
	}
	var q []Ticket
	e.do("GET", "/api/operator/queue", e.operator(), nil, &q)
	if len(q) != 1 || !q[0].Pregnant || !strings.Contains(q[0].Summary, "срок 32") {
		t.Fatalf("queue must show pregnancy: %+v", q)
	}
}

func TestPregnancyRedInOneMessageKazakh(t *testing.T) {
	e := setup(t)
	r := e.chat(e.patient(), "", "Мен жүктімін, 30 аптадамын, су кетті")
	if r.Urgency != "red" || r.Language != "kk" || !has(r.Actions, "urgent_operator") {
		t.Fatalf("kk pregnant red: %+v", r)
	}
}

func TestPlainRedHasOnlyAmbulanceButton(t *testing.T) {
	e := setup(t)
	r := e.chat(e.patient(), "", "боль в груди")
	if has(r.Actions, "urgent_operator") {
		t.Fatalf("not pregnant: only call_103 expected: %v", r.Actions)
	}
}

func TestLLMPregnantTrueSetsFlagAndRechecksRed(t *testing.T) {
	// "жду малыша" is not in the phrase list — LLM reports pregnancy, red_if_pregnant must then fire.
	e := setup(t, exWith(ex("find_service", "gynecologist", false, "", "yellow"), map[string]any{"pregnant": true, "gestation_weeks": 20}))
	r := e.chat(e.patient(), "", "жду малыша, появились кровянистые выделения")
	if r.Urgency != "red" || !has(r.Actions, "urgent_operator") {
		t.Fatalf("LLM pregnancy must trigger red_if_pregnant: %+v", r)
	}
	if d := e.dialog(r.DialogID); !d.Pregnant || d.GestationWeeks == nil || *d.GestationWeeks != 20 {
		t.Fatalf("flag from LLM: %+v", d)
	}
}

func TestPregnancyFlagNeverCleared(t *testing.T) {
	// flag comes only from the LLM (no phrase), then the LLM says "not pregnant" — the flag must stay
	e := setup(t,
		exWith(ex("find_service", "gynecologist", false, "", "green"), map[string]any{"pregnant": true}),
		exWith(ex("find_service", "therapist", false, "", "green"), map[string]any{"pregnant": false}))
	tok := e.patient()
	r := e.chat(tok, "", "жду малыша, хочу встать на учёт")
	r = e.chat(tok, r.DialogID, "а ещё насморк")
	if !e.dialog(r.DialogID).Pregnant {
		t.Fatal("pregnancy flag must never be cleared")
	}
}

func TestPregnancyQuestionAskedOnceAndNotCounted(t *testing.T) {
	// "тошнота" is not in ask_pregnancy_if: the LLM decides, and is trusted with a gyn/gastro specialty
	ask := exWith(ex("find_service", "gastroenterologist", true, "Как давно?", "green"), map[string]any{"ask_pregnancy": true})
	e := setup(t, ask, ask)
	tok := e.patient()
	r := e.chat(tok, "", "тошнота по утрам")
	if r.Reply.Content != texts["ask_pregnancy"]["ru"] {
		t.Fatalf("first: pregnancy question, got %q", r.Reply.Content)
	}
	r = e.chat(tok, r.DialogID, "не знаю")
	if r.Reply.Content != "Как давно?" {
		t.Fatalf("pregnancy question must be asked only once, got %q", r.Reply.Content)
	}
	if d := e.dialog(r.DialogID); !d.PregnancyAsked || d.Clarifications != 1 {
		t.Fatalf("pregnancy question must not use the clarification limit: %+v", d)
	}
}

func TestUrgentOperatorButtonAfterRed(t *testing.T) {
	e := setup(t)
	tok := e.patient()
	r := e.chat(tok, "", "беременна, отошли воды")
	var out map[string]any
	if code := e.do("POST", "/api/chat/operator", tok, map[string]string{"dialog_id": r.DialogID}, &out); code != 200 || out["urgent"] != true {
		t.Fatalf("urgent operator: %d %+v", code, out)
	}
	var q []Ticket
	e.do("GET", "/api/operator/queue", e.operator(), nil, &q)
	if len(q) != 1 || !strings.HasPrefix(q[0].Reason, "СРОЧНО") {
		t.Fatalf("one ticket, urgent reason: %+v", q)
	}
}

func TestPregnancyQuestionEvenIfLLMMissedIt(t *testing.T) {
	e := setup(t, ex("find_service", "gynecologist", false, "", "green")) // ask_pregnancy=false
	r := e.chat(e.patient(), "", "Тянет низ живота второй день")
	if r.Reply.Content != texts["ask_pregnancy"]["ru"] {
		t.Fatalf("safety net must ask about pregnancy, got %q", r.Reply.Content)
	}
}

func TestNoPregnancyQuestionIfPatientSaidNotPregnant(t *testing.T) {
	e := setup(t, exWith(ex("find_service", "gynecologist", false, "", "green"), map[string]any{"pregnant": false}))
	r := e.chat(e.patient(), "", "Тянет низ живота, беременности нет")
	if r.Reply.Content == texts["ask_pregnancy"]["ru"] {
		t.Fatal("must not ask when the patient already said she is not pregnant")
	}
}

func TestEnglishConversation(t *testing.T) {
	e := setup(t, ex("find_service", "ent", false, "", "green"))
	tok := e.patient()
	r := e.chat(tok, "", "I have a sore throat for three days")
	if r.Language != "en" || len(r.Services) == 0 {
		t.Fatalf("en: %+v", r)
	}
	r = e.chat(tok, r.DialogID, "and now chest pain")
	if r.Urgency != "red" || r.Reply.Content != texts["red"]["en"] {
		t.Fatalf("en red: %+v", r)
	}
}

func TestNoPregnancyQuestionForUnrelatedSpecialty(t *testing.T) {
	e := setup(t, exWith(ex("find_service", "therapist", false, "", "yellow"), map[string]any{"ask_pregnancy": true}))
	r := e.chat(e.patient(), "", "Тамағым ауырады, қызуым бар")
	if r.Reply.Content == texts["ask_pregnancy"]["kk"] {
		t.Fatal("sore throat must not trigger the pregnancy question")
	}
}

func TestRussianWithoutKazakhLettersStaysRussian(t *testing.T) {
	e := setup(t, exWith(ex("find_service", "gastroenterologist", false, "", "green"), map[string]any{"language": "kk"}))
	r := e.chat(e.patient(), "", "Тянет живот после еды")
	if r.Language != "ru" {
		t.Fatalf("model said kk for Russian text: got %q", r.Language)
	}
}

func TestKazakhDialogKeepsKazakhWithoutSpecialLetters(t *testing.T) {
	kk := exWith(ex("find_service", "", true, "Қашаннан бері?", "green"), map[string]any{"language": "kk"})
	e := setup(t, kk, exWith(ex("find_service", "neurologist", false, "", "green"), map[string]any{"language": "kk"}))
	tok := e.patient()
	r := e.chat(tok, "", "Басым қатты айналады")
	r = e.chat(tok, r.DialogID, "бес кун болды")
	if r.Language != "kk" {
		t.Fatalf("Kazakh dialog must stay Kazakh: %q", r.Language)
	}
}

func TestLLMPregnancyQuestionIgnoredWithoutGynSpecialty(t *testing.T) {
	e := setup(t, exWith(ex("find_service", "", true, "Қашаннан бері?", "yellow"), map[string]any{"ask_pregnancy": true}))
	r := e.chat(e.patient(), "", "Тамағым ауырады, қызуым бар")
	if r.Reply.Content == texts["ask_pregnancy"]["kk"] {
		t.Fatal("no gyn specialty and no ob/gyn phrase: must not ask about pregnancy")
	}
}

func TestKazakhAnswersUseKazakhModel(t *testing.T) {
	kk := exWith(ex("find_service", "ent", false, "", "green"), map[string]any{"language": "kk"})
	e := setup(t, kk, ex("find_service", "ent", false, "", "green"))
	e.a.llm.ModelKK = "kazllm"
	e.chat(e.patient(), "", "Тамағым ауырады")
	e.chat(e.patient(), "", "Болит горло")
	got := strings.Join(e.llm.models, ",")
	if got != "extract:fake,answer:kazllm,extract:fake,answer:fake" {
		t.Fatalf("model routing = %s", got)
	}
}

func TestAskFirstBeforeUrgencyAndServices(t *testing.T) {
	y := ex("find_service", "therapist", false, "", "yellow")
	e := setup(t, y, y, y, y)
	e.a.askFirst = true
	tok := e.patient()
	r := e.chat(tok, "", "температура 39")
	if r.Urgency != "" || len(r.Services) != 0 || r.Reply.Content != texts["ask_first"]["ru"] || len(r.Actions) != 0 {
		t.Fatalf("first turn must only ask, without level or services: %+v", r)
	}
	var d struct {
		Dialog Dialog `json:"dialog"`
	}
	e.do("GET", "/api/dialogs/"+r.DialogID, tok, nil, &d)
	if d.Dialog.Urgency != "" {
		t.Fatalf("patient must not see the level before assessment: %q", d.Dialog.Urgency)
	}
	r = e.chat(tok, r.DialogID, "второй день, на 7 из 10")
	if r.Urgency != "" || len(r.Services) != 0 || r.Reply.Content != texts["ask_second"]["ru"] {
		t.Fatalf("second turn: second question: %+v", r)
	}
	r = e.chat(tok, r.DialogID, "слабость, для меня")
	if r.Urgency != "" || r.Reply.Content != texts["ask_third"]["ru"] {
		t.Fatalf("third turn: third question: %+v", r)
	}
	r = e.chat(tok, r.DialogID, "нет, ничего не принимала")
	if r.Urgency != "yellow" || len(r.Services) == 0 || !has(r.Actions, "contact_operator") {
		t.Fatalf("after three answers: level and services: %+v", r)
	}
}

func TestAskFirstDoesNotRepeatTheSameQuestion(t *testing.T) {
	q := ex("find_service", "ent", false, "Как давно болит горло?", "green")
	e := setup(t, q, q, q)
	e.a.askFirst = true
	tok := e.patient()
	r := e.chat(tok, "", "болит горло")
	r = e.chat(tok, r.DialogID, "три дня")
	if r.Reply.Content == "Как давно болит горло?" {
		t.Fatal("the same question must not be asked twice")
	}
}

func TestAskFirstDoesNotDelayRed(t *testing.T) {
	e := setup(t)
	e.a.askFirst = true
	r := e.chat(e.patient(), "", "сильная боль в груди")
	if r.Urgency != "red" || !has(r.Actions, "call_103") {
		t.Fatalf("red must stay immediate: %+v", r)
	}
}

func testKB() *Knowledge {
	k := &Knowledge{Entries: []KBEntry{
		{ID: "hours", Keywords: map[string][]string{"ru": {"режим работы", "во сколько"}, "kk": {"жұмыс уақыты"}},
			Answer: map[string]string{"ru": "Мы работаем с 8:00 до 20:00.", "kk": "Біз 8:00-ден 20:00-ге дейін жұмыс істейміз."}},
		{ID: "prep-us", Keywords: map[string][]string{"ru": {"подготов", "узи брюшн"}, "kk": {"дайындал"}},
			Answer: map[string]string{"ru": "Натощак, 6–8 часов без еды.", "kk": "Аш қарынға, 6–8 сағат тамақ ішпеу керек."}, ServiceIDs: []string{"gastro-us"}},
	}}
	for i := range k.Entries {
		for lang, list := range k.Entries[i].Keywords {
			for j := range list {
				k.Entries[i].Keywords[lang][j] = normalize(list[j])
			}
		}
	}
	return k
}

func TestKnowledgeAnswerVerbatimWithoutLLMAnswer(t *testing.T) {
	e := setup(t, exWith(ex("other", "", false, "", "green"), map[string]any{"language": "kk"}))
	e.a.kb = testKB()
	r := e.chat(e.patient(), "", "Клиниканың жұмыс уақыты қандай?")
	if r.Reply.Content != "Біз 8:00-ден 20:00-ге дейін жұмыс істейміз." {
		t.Fatalf("KB answer expected, got %q", r.Reply.Content)
	}
	if _, m := e.llm.calls(); m != 0 {
		t.Fatalf("KB answer must not be rewritten by the LLM, answer calls = %d", m)
	}
	if r.Urgency != "" {
		t.Fatalf("an info question is not an urgency assessment: %q", r.Urgency)
	}
}

func TestKnowledgePreparationAttachesServiceCards(t *testing.T) {
	e := setup(t, ex("service_info", "gastroenterologist", false, "", "green"))
	e.a.kb = testKB()
	e.a.askFirst = true
	r := e.chat(e.patient(), "", "Как подготовиться к УЗИ брюшной полости?")
	if r.Reply.Content != "Натощак, 6–8 часов без еды." || len(r.Services) != 1 || r.Services[0].ID != "gastro-us" || len(r.Doctors) == 0 {
		t.Fatalf("prep answer + card: %+v", r)
	}
}

func TestComplaintWithOneKeywordStaysInComplaintFlow(t *testing.T) {
	e := setup(t, ex("find_service", "ent", false, "", "green"))
	e.a.kb = testKB()
	r := e.chat(e.patient(), "", "болит горло, во сколько можно прийти?")
	if strings.Contains(r.Reply.Content, "8:00") {
		t.Fatalf("one keyword inside a complaint must not hijack the flow: %q", r.Reply.Content)
	}
}

func TestRedBeatsKnowledge(t *testing.T) {
	e := setup(t)
	e.a.kb = testKB()
	r := e.chat(e.patient(), "", "во сколько вы работаете? у меня сильно болит грудь")
	if r.Urgency != "red" {
		t.Fatalf("red first: %+v", r)
	}
}

func TestKnowledgeBeatsMislabelledOperatorIntent(t *testing.T) {
	e := setup(t, ex("operator", "", false, "", "green"), ex("operator", "", false, "", "green"))
	e.a.kb = testKB()
	r := e.chat(e.patient(), "", "Во сколько вы работаете?")
	if r.Status != "bot" || r.Reply.Content != "Мы работаем с 8:00 до 20:00." {
		t.Fatalf("KB must answer, not hand off: %+v", r)
	}
	r = e.chat(e.patient(), "", "Соедините с оператором, во сколько вы работаете?")
	if r.Status != "operator" {
		t.Fatalf("an explicit request for a person goes to the operator: %+v", r)
	}
}

func TestPregnantWithoutSpecialtyGoesToObGyn(t *testing.T) {
	e := setup(t, ex("find_service", "", false, "", "green"))
	r := e.chat(e.patient(), "", "Я беременна, 20 недель, ноги отекают к вечеру")
	if len(r.Services) == 0 || r.Services[0].SpecialtyID != "gynecologist" {
		t.Fatalf("pregnant patient must be routed to the obstetrician-gynecologist: %+v", r)
	}
}

func TestPregnantRiskFactorsRaiseToYellowAndReachOperator(t *testing.T) {
	e := setup(t,
		exWith(ex("find_service", "gynecologist", false, "", "green"), map[string]any{"risk_factors": []string{"hypertension", "diabetes", "made_up"}}),
		ex("operator", "", false, "", "green"))
	tok := e.patient()
	r := e.chat(tok, "", "Я беременна, 28 недель, у меня гипертония и диабет, болит голова")
	if r.Urgency != "yellow" || !has(r.Actions, "contact_operator") {
		t.Fatalf("pregnancy + risk factors must be at least yellow: %+v", r)
	}
	r = e.chat(tok, r.DialogID, "соедините с оператором")
	var q []Ticket
	e.do("GET", "/api/operator/queue", e.operator(), nil, &q)
	if len(q) != 1 || !strings.Contains(q[0].Summary, "гипертензия") || !strings.Contains(q[0].Summary, "диабет") || len(q[0].Risks) != 2 {
		t.Fatalf("operator must see risk factors (unknown ids dropped): %+v", q)
	}
}

func TestRiskFactorsOnlyAdded(t *testing.T) {
	e := setup(t,
		exWith(ex("find_service", "gynecologist", false, "", "green"), map[string]any{"risk_factors": []string{"anemia"}}),
		ex("find_service", "gynecologist", false, "", "green"))
	tok := e.patient()
	r := e.chat(tok, "", "беременна, анемия")
	r = e.chat(tok, r.DialogID, "а когда сдавать скрининг?")
	if d := e.dialog(r.DialogID); len(d.RiskFactors) != 1 || d.RiskFactors[0] != "anemia" {
		t.Fatalf("risk factors must persist: %+v", d.RiskFactors)
	}
}

func TestPregnancyKnowledgeOnlyForPregnant(t *testing.T) {
	e := setup(t, ex("find_service", "neurologist", false, "", "green"), ex("service_info", "gynecologist", false, "", "green"))
	kb, err := LoadKnowledge("../knowledge.json")
	if err != nil {
		t.Fatal(err)
	}
	e.a.kb = kb
	r := e.chat(e.patient(), "", "болит спина и поясница")
	if strings.Contains(r.Reply.Content, "беремен") {
		t.Fatalf("a non-pregnant patient got a pregnancy answer: %q", r.Reply.Content)
	}
	r = e.chat(e.patient(), "", "Опасен ли гастрит при беременности?")
	if !strings.Contains(strings.ToLower(r.Reply.Content), "гастрит") {
		t.Fatalf("pregnant question must get the pregnancy entry: %q", r.Reply.Content)
	}
}

// LM Studio runs the model with a 4096-token context. Cyrillic is ~1 token per 2.5 characters, so the
// system prompt must stay well under that (measured: 8884 characters = 4211 tokens) to leave room for 8 dialog messages and the JSON answer.
func TestExtractionPromptFitsContext(t *testing.T) {
	c, _ := LoadCatalog("../catalog.json")
	tr, _ := LoadTriage("../triage_rules.json")
	for _, lang := range []string{"ru", "kk", "en"} {
		p := extractionPrompt(c, tr, 0, &Dialog{Pregnant: true, PregnancyAsked: true}, lang)
		if n := len([]rune(p)); n > 4800 {
			t.Errorf("%s prompt is %d characters, keep it under 4800", lang, n)
		}
	}
}

func TestGreetingLabelledOperatorIsNotHandedOff(t *testing.T) {
	e := setup(t, ex("operator", "", false, "", "green"))
	r := e.chat(e.patient(), "", "привет")
	if r.Status != "bot" || r.TicketID != nil || r.Urgency != "" {
		t.Fatalf("a greeting must not go to the operator: %+v", r)
	}
}

func TestKnowledgeAnswerKeepsYellowForRiskyPregnancy(t *testing.T) {
	e := setup(t, exWith(ex("find_service", "gynecologist", false, "", "green"), map[string]any{"risk_factors": []string{"hypertension"}}))
	kb, _ := LoadKnowledge("../knowledge.json")
	e.a.kb = kb
	r := e.chat(e.patient(), "", "Я беременна, 20 недель, у меня гипертония, отекают ноги")
	if r.Urgency != "yellow" || !has(r.Actions, "contact_operator") || !strings.HasPrefix(r.Reply.Content, texts["yellow"]["ru"]) {
		t.Fatalf("pregnancy + hypertension: yellow with the KB answer: %+v", r)
	}
}

func TestPregnantVagueQuestionDoesNotReofferGynecologist(t *testing.T) {
	e := setup(t, ex("service_info", "", false, "", "green"))
	r := e.chat(e.patient(), "", "я беременна, у меня ещё вопрос")
	if len(r.Services) != 0 {
		t.Fatalf("a vague question must not bring the gynecologist cards: %+v", r.Services)
	}
}

func TestAskFirstAlsoForComplaintLabelledServiceInfo(t *testing.T) {
	e := setup(t, ex("service_info", "dentist", false, "Зуб реагирует на холодное?", "green"), ex("service_info", "dentist", false, "", "green"))
	e.a.askFirst = true
	r := e.chat(e.patient(), "", "Болит зуб")
	if r.Reply.Content != "Зуб реагирует на холодное?" || len(r.Services) != 0 {
		t.Fatalf("a complaint labelled service_info still gets questions: %+v", r)
	}
	r = e.chat(e.patient(), "", "Сколько стоит консультация стоматолога?")
	if len(r.Services) == 0 {
		t.Fatalf("a direct price question is answered at once: %+v", r)
	}
}

func TestKazakhWithoutSpecialLettersTrustsModel(t *testing.T) {
	e := setup(t, exWith(ex("find_service", "neurologist", false, "", "green"), map[string]any{"language": "kk"}))
	r := e.chat(e.patient(), "", "Басым ауырып жатыр")
	if r.Language != "kk" {
		t.Fatalf("Kazakh text without Kazakh-only letters and without Russian words: %q", r.Language)
	}
}

func TestDropForeignScript(t *testing.T) {
	in := "Запишитесь к терапевту 01.10 в 09:00. Прием建议您在就诊前充分休息，多喝水。Если станет хуже — звоните 103."
	got := dropForeignScript(in)
	if strings.ContainsAny(got, "建议休息") || !strings.Contains(got, "Запишитесь к терапевту") || !strings.Contains(got, "звоните 103.") {
		t.Fatalf("got %q", got)
	}
	if dropForeignScript("Обычный ответ. Без иероглифов.") != "Обычный ответ. Без иероглифов." {
		t.Fatal("clean text must stay unchanged")
	}
}

func TestInLanguage(t *testing.T) {
	cases := []struct {
		q, lang string
		want    bool
	}{
		{"Есть ли температура или тошнота?", "ru", true},
		{"Бұл болқанда және келесі күндегі температура сипаттамасы келеді?", "ru", false},
		{"Қызуыңыз бар ма?", "kk", true},
		{"Есть ли у вас температура?", "kk", false},
		{"Do you have a fever?", "en", true},
		{"是否发烧?", "ru", false},
	}
	for _, c := range cases {
		if got := inLanguage(c.q, c.lang); got != c.want {
			t.Errorf("inLanguage(%q, %s) = %v", c.q, c.lang, got)
		}
	}
}
