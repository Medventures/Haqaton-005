package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var texts = map[string]map[string]string{
	"red": {
		"ru": "Ваши симптомы могут требовать срочной медицинской помощи. Срочно звоните 103 или обратитесь в ближайшее приёмное отделение. Не ждите записи к врачу. Мы также передали ваш диалог оператору.",
		"kk": "Сіздегі белгілер шұғыл медициналық көмекті қажет етуі мүмкін. Дереу 103-ке қоңырау шалыңыз немесе жақын жердегі қабылдау бөлімшесіне барыңыз. Дәрігерге жазылуды күтпеңіз. Диалогыңызды операторға да жібердік.",
	},
	"yellow": {
		"ru": "По вашему описанию стоит обратиться за медицинской помощью в ближайшее время. Если станет хуже — звоните 103.",
		"kk": "Сипаттамаңызға қарағанда, жақын арада медициналық көмекке жүгінген жөн. Жағдайыңыз нашарласа — 103-ке қоңырау шалыңыз.",
	},
	"handoff": {
		"ru": "Передаю ваш вопрос оператору — он ответит здесь, в этом чате.",
		"kk": "Сұрағыңызды операторға жібердім — ол осы чатта жауап береді.",
	},
	"clarify": {
		"ru": "Уточните, пожалуйста: что именно беспокоит, где и как давно?",
		"kk": "Нақтылап жазыңызшы: не мазалайды, қай жерде және қашаннан бері?",
	},
	"fallback": {
		"ru": "Вам подойдёт специалист: %s. Ниже — услуги с ценами и свободные слоты врачей.",
		"kk": "Сізге қажетті маман: %s. Төменде — қызметтер бағасымен және дәрігерлердің бос уақыттары.",
	},
}

var langInstr = map[string]string{
	"ru": "Ответь на русском языке.",
	"kk": "Жауапты тек қазақ тілінде жаз. Отвечай ТОЛЬКО на казахском языке.",
}

func t(key, lang string) string {
	if lang != "kk" {
		lang = "ru"
	}
	return texts[key][lang]
}

type ChatRequest struct {
	DialogID string `json:"dialog_id"`
	Message  string `json:"message"`
}

type ChatResponse struct {
	DialogID      string    `json:"dialog_id"`
	Status        string    `json:"status"`
	Urgency       string    `json:"urgency"`
	UrgencyReason string    `json:"urgency_reason"`
	Language      string    `json:"language"`
	Reply         *Message  `json:"reply"` // null when the dialog is with an operator
	Actions       []string  `json:"actions"`
	Services      []Service `json:"services"`
	Doctors       []Doctor  `json:"doctors"`
	TicketID      *int64    `json:"ticket_id"`
}

// botData is stored in messages.data so the UI can re-render cards/badges from history.
type botData struct {
	Urgency  string    `json:"urgency"`
	Actions  []string  `json:"actions"`
	Services []Service `json:"services,omitempty"`
	Doctors  []Doctor  `json:"doctors,omitempty"`
}

// POST /api/chat
func (a *App) chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	var in ChatRequest
	if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Message) == "" {
		writeErr(w, 400, "message is required")
		return
	}
	if len(in.Message) > 4000 {
		writeErr(w, 400, "message too long")
		return
	}

	var d *Dialog
	var err error
	if in.DialogID == "" {
		var id string
		if err = a.db.QueryRow(ctx, `insert into dialogs(patient_id) values($1) returning id`, u.ID).Scan(&id); err == nil {
			d, err = a.getDialog(ctx, id)
		}
	} else if d, err = a.getDialog(ctx, in.DialogID); err == nil && d.PatientID != u.ID {
		writeErr(w, 404, "dialog not found")
		return
	}
	if err != nil {
		writeErr(w, 404, "dialog not found")
		return
	}
	if _, err := a.addMessage(ctx, d.ID, "patient", u.ID, in.Message, nil); err != nil {
		writeErr(w, 500, "db error")
		return
	}

	res, err := a.process(ctx, d, in.Message)
	if err != nil {
		log.Printf("chat %s: %v", d.ID, err)
		writeErr(w, 500, "internal error")
		return
	}
	writeJSON(w, 200, res)
}

func (a *App) process(ctx context.Context, d *Dialog, text string) (*ChatResponse, error) {
	res := &ChatResponse{DialogID: d.ID, Actions: []string{}, Services: []Service{}, Doctors: []Doctor{}}
	finish := func(reply string, data botData) (*ChatResponse, error) {
		data.Urgency = d.Urgency
		if data.Actions == nil {
			data.Actions = []string{}
		}
		if err := a.saveDialog(ctx, d); err != nil {
			return nil, err
		}
		if reply != "" {
			m, err := a.addMessage(ctx, d.ID, "bot", "bot", reply, data)
			if err != nil {
				return nil, err
			}
			res.Reply = m
		}
		res.Status, res.Urgency, res.UrgencyReason, res.Language = d.Status, d.Urgency, d.UrgencyReason, d.Language
		res.Actions = data.Actions
		if data.Services != nil {
			res.Services, res.Doctors = data.Services, data.Doctors
		}
		return res, nil
	}
	handoff := func(reason string, data botData) (*ChatResponse, error) {
		id, err := a.handoff(ctx, d, reason)
		if err != nil {
			return nil, err
		}
		res.TicketID = &id
		return finish(t("handoff", d.Language), data)
	}

	// 1. Red flags: deterministic, before any LLM call, on every message.
	if phrase, lang, ok := a.triage.CheckRed(text); ok {
		if hasKazakhLetters(text) {
			lang = "kk"
		}
		d.Language = lang
		d.Urgency = "red"
		d.UrgencyReason = "красный флаг: " + phrase
		d.Summary = strings.TrimSpace(fmt.Sprintf("КРАСНЫЙ ФЛАГ («%s»). Сообщение пациента: «%s». %s", phrase, text, d.Summary))
		id, err := a.handoff(ctx, d, "Красный флаг: "+phrase+" — пациенту рекомендовано звонить 103")
		if err != nil {
			return nil, err
		}
		res.TicketID = &id
		return finish(t("red", lang), botData{Actions: []string{"call_103"}})
	}

	// Dialog already with an operator: bot stays silent, operator sees the message.
	if d.Status == "operator" {
		return finish("", botData{})
	}

	// 2. LLM extraction.
	hist, err := a.messages(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	var transcript strings.Builder
	if len(hist) > 12 {
		hist = hist[len(hist)-12:]
	}
	for _, m := range hist {
		who := map[string]string{"patient": "Пациент", "bot": "Бот", "operator": "Оператор"}[m.Role]
		fmt.Fprintf(&transcript, "%s: %s\n", who, m.Content)
	}
	ex, ok := a.llm.Extract(ctx, []chatMsg{
		{"system", extractionPrompt(a.cat, a.triage, 2-d.Clarifications)},
		{"user", "Диалог:\n" + transcript.String()},
	}, a.cat)
	if !ok && d.Summary == "" {
		d.Summary = "Сообщение пациента: «" + text + "». ИИ не смог разобрать запрос."
	}

	if hasKazakhLetters(text) {
		ex.Language = "kk"
	} else if ex.Language != "kk" {
		ex.Language = "ru"
	}
	d.Language = ex.Language
	if ex.SpecialtyID != nil && a.cat.Specialty(*ex.SpecialtyID) == nil {
		ex.SpecialtyID = nil // only ids from the catalog
	}
	if ex.Summary != "" {
		d.Summary = ex.Summary
	}
	if ex.Urgency == "yellow" && d.Urgency == "green" {
		d.UrgencyReason = ex.UrgencyReason
	}
	if ex.Urgency == "yellow" {
		d.Urgency = maxUrgency(d.Urgency, "yellow") // never goes down
	}

	var actions []string
	prefix := ""
	if d.Urgency == "yellow" {
		actions = []string{"contact_operator"}
		prefix = t("yellow", d.Language) + "\n\n"
	}

	// 6a. Explicit request for a human.
	if !ok {
		return handoff("Бот не справился: ИИ не вернул корректный JSON (2 попытки)", botData{Actions: actions})
	}
	if ex.Intent == "operator" {
		return handoff("Пациент попросил оператора", botData{Actions: actions})
	}

	// 3. Clarification (max 2 per dialog).
	q := ""
	if ex.ClarifyingQuestion != nil {
		q = strings.TrimSpace(*ex.ClarifyingQuestion)
	}
	wantsService := ex.Intent == "find_service" || ex.Intent == "service_info"
	if q == "" && d.Clarifications < 2 && ex.SpecialtyID == nil && (ex.NeedClarification || wantsService) {
		q = t("clarify", d.Language) // model asked for clarification but gave no question
	}
	if (ex.NeedClarification || ex.SpecialtyID == nil && wantsService) && q != "" && d.Clarifications < 2 {
		d.Clarifications++
		return finish(prefix+q, botData{Actions: actions})
	}

	// 4-5. Specialty found: services + doctors from the catalog, LLM phrases the answer.
	if ex.SpecialtyID != nil {
		spec := a.cat.Specialty(*ex.SpecialtyID)
		svcs, docs := a.cat.ServicesFor(spec.ID), a.cat.DoctorsFor(spec.ID)
		data, _ := json.Marshal(map[string]any{"specialty": spec, "services": svcs, "doctors_with_free_slots": humanSlots(docs)})
		reply, err := a.llm.Chat(ctx, []chatMsg{
			{"system", fmt.Sprintf(answerSystemPrompt, d.Language)},
			{"user", fmt.Sprintf("Запрос пациента: %s\nПоследнее сообщение: %s\n\nДАННЫЕ КАТАЛОГА:\n%s\n\n%s", d.Summary, text, data, langInstr[d.Language])},
		}, nil)
		if err != nil || strings.TrimSpace(reply) == "" {
			log.Printf("compose failed: %v", err)
			reply = fmt.Sprintf(t("fallback", d.Language), spec.Name)
		}
		return finish(prefix+strings.TrimSpace(reply), botData{Actions: actions, Services: svcs, Doctors: docs})
	}

	// Wanted a service but we could not determine one — hand over.
	if wantsService {
		return handoff("Не удалось подобрать специальность (уточнений: "+fmt.Sprint(d.Clarifications)+")", botData{Actions: actions})
	}

	// intent=other: short answer limited to what the clinic offers.
	names := []string{}
	for _, s := range a.cat.Specialties {
		names = append(names, s.Name)
	}
	reply, err := a.llm.Chat(ctx, []chatMsg{
		{"system", fmt.Sprintf(answerSystemPrompt, d.Language) + "\nЕсли вопрос не про запись к врачу — вежливо скажи, что ты помогаешь подобрать врача и услугу, и попроси описать жалобу."},
		{"user", fmt.Sprintf("Сообщение пациента: %s\n\nДАННЫЕ: специалисты клиники: %s\n\n%s", text, strings.Join(names, ", "), langInstr[d.Language])},
	}, nil)
	if err != nil {
		return handoff("Бот не справился: ошибка ИИ", botData{Actions: actions})
	}
	return finish(prefix+strings.TrimSpace(reply), botData{Actions: actions})
}

// POST /api/chat/operator — patient pressed «Связаться с оператором».
func (a *App) requestOperator(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		DialogID string `json:"dialog_id"`
	}
	json.NewDecoder(r.Body).Decode(&in)
	d, err := a.getDialog(ctx, in.DialogID)
	if err != nil || d.PatientID != userFrom(r).ID {
		writeErr(w, 404, "dialog not found")
		return
	}
	id, err := a.handoff(ctx, d, "Пациент нажал «Связаться с оператором»")
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	m, _ := a.addMessage(ctx, d.ID, "bot", "bot", t("handoff", d.Language), botData{Urgency: d.Urgency, Actions: []string{}})
	writeJSON(w, 200, map[string]any{"ticket_id": id, "status": d.Status, "reply": m})
}

// GET /api/dialogs/{id} — patient: only own dialog; operator: dialogs that were handed off.
func (a *App) getDialogHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	d, err := a.getDialog(ctx, chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, 404, "dialog not found")
		return
	}
	if u.Role == "patient" && d.PatientID != u.ID {
		writeErr(w, 404, "dialog not found")
		return
	}
	rows, err := a.db.Query(ctx, `select `+ticketCols+` from tickets t join dialogs d on d.id=t.dialog_id where t.dialog_id=$1 order by t.id`, d.ID)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	tickets, err := scanTickets(rows)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	if u.Role == "operator" && len(tickets) == 0 {
		writeErr(w, 404, "dialog not found")
		return
	}
	msgs, err := a.messages(ctx, d.ID)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	resp := map[string]any{"dialog": d, "messages": msgs}
	if u.Role == "operator" {
		resp["tickets"] = tickets
	}
	writeJSON(w, 200, resp)
}

// GET /api/operator/queue?status=open|closed|all — sorted red → yellow → green, then oldest first.
func (a *App) queue(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	rows, err := a.db.Query(r.Context(), `select `+ticketCols+` from tickets t join dialogs d on d.id=t.dialog_id
		where $1='all' or t.status=$1
		order by case d.urgency when 'red' then 0 when 'yellow' then 1 else 2 end, t.created_at`, status)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	out, err := scanTickets(rows)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	writeJSON(w, 200, out)
}

// POST /api/operator/reply
func (a *App) operatorReply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		DialogID string `json:"dialog_id"`
		Message  string `json:"message"`
		Close    bool   `json:"close"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || (strings.TrimSpace(in.Message) == "" && !in.Close) {
		writeErr(w, 400, "message is required")
		return
	}
	d, err := a.getDialog(ctx, in.DialogID)
	if err != nil {
		writeErr(w, 404, "dialog not found")
		return
	}
	var m *Message
	if strings.TrimSpace(in.Message) != "" {
		if m, err = a.addMessage(ctx, d.ID, "operator", userFrom(r).ID, in.Message, map[string]any{"urgency": d.Urgency}); err != nil {
			writeErr(w, 500, "db error")
			return
		}
	}
	if in.Close {
		// ticket closed, bot takes the dialog back; urgency is kept (never goes down)
		a.db.Exec(ctx, `update tickets set status='closed', updated_at=now() where dialog_id=$1 and status='open'`, d.ID)
		d.Status = "bot"
		a.saveDialog(ctx, d)
	}
	writeJSON(w, 200, map[string]any{"message": m, "status": d.Status})
}

// humanSlots turns "2026-10-01T09:00" into "01.10 09:00" so the LLM reads them correctly.
func humanSlots(docs []Doctor) []map[string]any {
	out := []map[string]any{}
	for _, d := range docs {
		slots := []string{}
		for _, s := range d.Slots {
			if tm, err := time.Parse("2006-01-02T15:04", s); err == nil {
				s = tm.Format("02.01 15:04")
			}
			slots = append(slots, s)
		}
		out = append(out, map[string]any{"doctor": d.Name, "free_slots": slots})
	}
	return out
}
