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
		"en": "Your symptoms may need urgent medical help. Call 103 now or go to the nearest emergency department. Do not wait for a doctor's appointment. We have also passed your conversation to an operator.",
	},
	"yellow": {
		"ru": "По вашему описанию стоит обратиться за медицинской помощью в ближайшее время. Если станет хуже — звоните 103.",
		"kk": "Сипаттамаңызға қарағанда, жақын арада медициналық көмекке жүгінген жөн. Жағдайыңыз нашарласа — 103-ке қоңырау шалыңыз.",
		"en": "Based on your description, you should seek medical help soon. If you feel worse, call 103.",
	},
	"handoff": {
		"ru": "Передаю ваш вопрос оператору — он ответит здесь, в этом чате.",
		"kk": "Сұрағыңызды операторға жібердім — ол осы чатта жауап береді.",
		"en": "I am passing your question to an operator — they will reply here in this chat.",
	},
	"ask_pregnancy": {
		"ru": "Есть ли у вас беременность или её вероятность?",
		"kk": "Сізде жүктілік бар ма немесе болуы мүмкін бе?",
		"en": "Are you pregnant, or could you be pregnant?",
	},
	"ask_first": {
		"ru": "Уточните, пожалуйста: как давно это началось и насколько сильно беспокоит по шкале от 1 до 10?",
		"kk": "Нақтылаңызшы: бұл қашаннан бері басталды және 1-ден 10-ға дейінгі шкала бойынша қаншалықты қатты мазалайды?",
		"en": "Could you tell me when it started and how bad it is on a scale from 1 to 10?",
	},
	"clarify": {
		"ru": "Уточните, пожалуйста: что именно беспокоит, где и как давно?",
		"kk": "Нақтылап жазыңызшы: не мазалайды, қай жерде және қашаннан бері?",
		"en": "Could you please clarify: what exactly bothers you, where, and for how long?",
	},
	"fallback": {
		"ru": "Вам подойдёт специалист: %s. Ниже — услуги с ценами и свободные слоты врачей.",
		"kk": "Сізге қажетті маман: %s. Төменде — қызметтер бағасымен және дәрігерлердің бос уақыттары.",
		"en": "The right specialist for you: %s. Below are the services with prices and doctors' free slots.",
	},
}

var langInstr = map[string]string{
	"ru": "Ответь на русском языке.",
	"kk": "Жауапты тек қазақ тілінде жаз. Отвечай ТОЛЬКО на казахском языке.",
	"en": "Answer in English only.",
}

func t(key, lang string) string {
	if lang != "kk" && lang != "en" {
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
		data.Urgency = d.VisibleUrgency()
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
		res.Status, res.Urgency, res.Language = d.Status, d.VisibleUrgency(), d.Language
		if d.Assessed {
			res.UrgencyReason = d.UrgencyReason
		}
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

	hist, err := a.messages(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	patientMsgs := []string{}
	for _, m := range hist {
		if m.Role == "patient" {
			patientMsgs = append(patientMsgs, m.Content)
		}
	}

	// Pregnancy flag: set by code from any message, never cleared.
	for _, m := range patientMsgs {
		if ok, weeks := DetectPregnancy(m); ok {
			d.Pregnant = true
			if weeks != nil {
				d.GestationWeeks = weeks
			}
		}
	}

	// 1. Red flags: deterministic, before any LLM call, on every message.
	if r, done, err := a.redCheck(ctx, d, text, patientMsgs, res, finish); done {
		return r, err
	}

	// Dialog already with an operator: bot stays silent, operator sees the message.
	if d.Status == "operator" {
		return finish("", botData{})
	}

	// 2. LLM extraction.
	var transcript strings.Builder
	if len(hist) > 12 {
		hist = hist[len(hist)-12:]
	}
	for _, m := range hist {
		who := map[string]string{"patient": "Пациент", "bot": "Бот", "operator": "Оператор"}[m.Role]
		fmt.Fprintf(&transcript, "%s: %s\n", who, m.Content)
	}
	ex, ok := a.llm.Extract(ctx, []chatMsg{
		{"system", extractionPrompt(a.cat, a.triage, 2-d.Clarifications, d)},
		{"user", "Диалог:\n" + transcript.String()},
	}, a.cat)
	if !ok && d.Summary == "" {
		d.Summary = "Сообщение пациента: «" + text + "». ИИ не смог разобрать запрос."
	}

	// Cyrillic without Kazakh-specific letters is Russian, unless the dialog is already in Kazakh
	// ("басым ауырады" has no special letters): the model misjudges this too often.
	if l := detectLanguage(text); l != "" {
		ex.Language = l
	} else if ex.Language == "kk" && d.Language == "kk" {
		ex.Language = "kk"
	} else {
		ex.Language = "ru"
	}
	d.Language = ex.Language
	if ex.SpecialtyID != nil && a.cat.Specialty(*ex.SpecialtyID) == nil {
		ex.SpecialtyID = nil // only ids from the catalog
	}
	if ex.Summary != "" {
		d.Summary = ex.Summary
	}
	if ok && ex.Pregnant != nil && *ex.Pregnant && !d.Pregnant {
		// LLM found a pregnancy the phrase rules missed: re-run the deterministic red check with it.
		d.Pregnant = true
		if d.GestationWeeks == nil && ex.GestationWeeks != nil && *ex.GestationWeeks >= 1 && *ex.GestationWeeks <= 42 {
			d.GestationWeeks = ex.GestationWeeks
		}
		if r, done, err := a.redCheck(ctx, d, text, patientMsgs, res, finish); done {
			return r, err
		}
	}
	if ex.Urgency == "yellow" && d.Urgency == "green" {
		d.UrgencyReason = ex.UrgencyReason
	}
	if ex.Urgency == "yellow" {
		d.Urgency = maxUrgency(d.Urgency, "yellow") // never goes down
	}

	// 6a. Explicit request for a human.
	if !ok {
		return handoff("Бот не справился: ИИ не вернул корректный JSON (2 попытки)", botData{})
	}
	// Knowledge base: practical questions (address, hours, test preparation, ...) get the curated answer
	// from knowledge.json as is — no LLM rewriting, so Kazakh stays correct. A complaint needs 2+ keyword hits.
	// Checked before the operator intent: the model labelled "Где вы находитесь?" as operator.
	if kb, score := a.kb.Search(text); kb != nil && !asksForHuman(text) && (ex.Intent != "find_service" || score >= 2) {
		data := botData{}
		seen := map[string]bool{}
		for _, id := range kb.ServiceIDs {
			if sv := a.cat.Service(id); sv != nil {
				data.Services = append(data.Services, *sv)
				if !seen[sv.SpecialtyID] {
					seen[sv.SpecialtyID] = true
					data.Doctors = append(data.Doctors, a.cat.DoctorsFor(sv.SpecialtyID)...)
				}
			}
		}
		if data.Services != nil && data.Doctors == nil {
			data.Doctors = []Doctor{}
		}
		return finish(kb.AnswerIn(d.Language), data)
	}

	if ex.Intent == "operator" {
		return handoff("Пациент попросил оператора", botData{})
	}

	// 4 (ob/gyn). Pregnancy question: asked once, does not count toward the clarification limit.
	// The model's ask_pregnancy is trusted only with a gyn/gastro specialty (it asked about pregnancy for a sore throat);
	// otherwise the ask_pregnancy_if phrases decide.
	llmAsk := ex.AskPregnancy && ex.SpecialtyID != nil && (*ex.SpecialtyID == "gynecologist" || *ex.SpecialtyID == "gastroenterologist")
	if (llmAsk || a.triage.NeedsPregnancyQuestion(text)) && !d.Pregnant && !d.PregnancyAsked && !(ex.Pregnant != nil && !*ex.Pregnant) {
		d.PregnancyAsked = true
		return finish(t("ask_pregnancy", d.Language), botData{})
	}

	// Ask first: for a complaint, one clarifying question before any urgency level or services are shown
	// (red is still decided immediately above). Counts toward the 2-clarification limit.
	if a.askFirst && !d.Assessed && ex.Intent == "find_service" && d.Clarifications == 0 {
		q := ""
		if ex.ClarifyingQuestion != nil {
			q = strings.TrimSpace(*ex.ClarifyingQuestion)
		}
		if q == "" {
			q = t("ask_first", d.Language)
		}
		d.Clarifications++
		return finish(q, botData{})
	}

	// From here the urgency level is shown to the patient.
	d.Assessed = true
	var actions []string
	prefix := ""
	if d.Urgency == "yellow" {
		actions = []string{"contact_operator"}
		prefix = t("yellow", d.Language) + "\n\n"
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
		specName, specDesc := spec.Localized(d.Language)
		llmSvcs := []map[string]any{}
		for _, sv := range svcs {
			n, desc := sv.Localized(d.Language)
			llmSvcs = append(llmSvcs, map[string]any{"name": n, "price_kzt": sv.Price, "description": desc})
		}
		data, _ := json.Marshal(map[string]any{"specialty": map[string]string{"name": specName, "description": specDesc},
			"services": llmSvcs, "doctors_with_free_slots": humanSlots(docs)})
		reply, err := a.llm.Answer(ctx, d.Language, []chatMsg{
			{"system", fmt.Sprintf(answerSystemPrompt, d.Language)},
			{"user", fmt.Sprintf("Запрос пациента: %s\nПоследнее сообщение: %s\n\nДАННЫЕ КАТАЛОГА:\n%s\n\n%s", d.Summary, text, data, langInstr[d.Language])},
		})
		if err != nil || strings.TrimSpace(reply) == "" {
			log.Printf("compose failed: %v", err)
			reply = fmt.Sprintf(t("fallback", d.Language), specName)
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
	reply, err := a.llm.Answer(ctx, d.Language, []chatMsg{
		{"system", fmt.Sprintf(answerSystemPrompt, d.Language) + "\nЕсли вопрос не про запись к врачу — вежливо скажи, что ты помогаешь подобрать врача и услугу, и попроси описать жалобу."},
		{"user", fmt.Sprintf("Сообщение пациента: %s\n\nДАННЫЕ: специалисты клиники: %s\n\n%s", text, strings.Join(names, ", "), langInstr[d.Language])},
	})
	if err != nil {
		return handoff("Бот не справился: ошибка ИИ", botData{Actions: actions})
	}
	return finish(prefix+strings.TrimSpace(reply), botData{Actions: actions})
}

// redCheck runs red (+ red_if_pregnant when the flag is set) over ALL patient messages.
// It fires when the current message matches, or when an older one matches and the dialog is not red yet
// (e.g. pain first, "I'm at week 32" later). done=true means the response is ready.
func (a *App) redCheck(ctx context.Context, d *Dialog, text string, patientMsgs []string, res *ChatResponse,
	finish func(string, botData) (*ChatResponse, error)) (*ChatResponse, bool, error) {
	phrase, lang, hit := a.triage.CheckRed(text, d.Pregnant)
	matched := text
	if !hit && d.Urgency != "red" {
		for _, m := range patientMsgs {
			if phrase, lang, hit = a.triage.CheckRed(m, d.Pregnant); hit {
				matched = m
				break
			}
		}
	}
	if !hit {
		return nil, false, nil
	}
	if l := detectLanguage(text); l != "" {
		lang = l
	}
	d.Language = lang
	d.Urgency = "red"
	d.Assessed = true
	d.UrgencyReason = "красный флаг: " + phrase
	preg := ""
	if d.Pregnant {
		preg = " Беременность: да"
		if d.GestationWeeks != nil {
			preg += fmt.Sprintf(", срок %d нед.", *d.GestationWeeks)
		}
		preg += "."
	}
	d.Summary = strings.TrimSpace(fmt.Sprintf("КРАСНЫЙ ФЛАГ («%s»). Сообщение пациента: «%s».%s %s", phrase, matched, preg, d.Summary))
	id, err := a.handoff(ctx, d, "Красный флаг: "+phrase+" — пациенту рекомендовано звонить 103")
	if err != nil {
		return nil, true, err
	}
	res.TicketID = &id
	actions := []string{"call_103"}
	if d.Pregnant {
		actions = append(actions, "urgent_operator")
	}
	r, err := finish(t("red", lang), botData{Actions: actions})
	return r, true, err
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
	var urgent bool
	reason := "Пациент нажал «Связаться с оператором»"
	if d.Urgency == "red" {
		urgent = true
		reason = "СРОЧНО: пациент нажал «Срочно связаться с оператором» после красного флага"
	}
	id, err := a.handoff(ctx, d, reason)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	m, _ := a.addMessage(ctx, d.ID, "bot", "bot", t("handoff", d.Language), botData{Urgency: d.VisibleUrgency(), Actions: []string{}})
	writeJSON(w, 200, map[string]any{"ticket_id": id, "status": d.Status, "urgent": urgent, "reply": m})
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
	if u.Role == "patient" {
		d.Urgency = d.VisibleUrgency()
		if !d.Assessed {
			d.UrgencyReason = ""
		}
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

var humanWords = []string{"оператор", "администратор", "живой человек", "живым человеком", "с человеком",
	"адаммен", "операторға", "operator", "human", "real person"}

// asksForHuman: the patient explicitly asks for a person — never answer that from the knowledge base.
func asksForHuman(text string) bool {
	n := normalize(text)
	for _, w := range humanWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}
