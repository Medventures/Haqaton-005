package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// LLM is a client for an OpenAI-compatible API (LM Studio).
type LLM struct {
	BaseURL, Model string
	ModelKK        string // optional: model that writes free-text answers in Kazakh (e.g. KazLLM)
	hc             *http.Client
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func NewLLM(baseURL, model string) *LLM {
	return &LLM{BaseURL: strings.TrimRight(baseURL, "/"), Model: model, hc: &http.Client{Timeout: 60 * time.Second}}
}

// Chat calls POST {base}/chat/completions. If schema != nil, uses response_format json_schema (strict).
func (l *LLM) Chat(ctx context.Context, msgs []chatMsg, schema map[string]any) (string, error) {
	return l.chatWith(ctx, l.Model, msgs, schema)
}

// Answer writes a free-text reply; for Kazakh it uses ModelKK when configured.
func (l *LLM) Answer(ctx context.Context, lang string, msgs []chatMsg) (string, error) {
	if lang == "kk" && l.ModelKK != "" {
		return l.chatWith(ctx, l.ModelKK, msgs, nil)
	}
	return l.chatWith(ctx, l.Model, msgs, nil)
}

func (l *LLM) chatWith(ctx context.Context, model string, msgs []chatMsg, schema map[string]any) (string, error) {
	body := map[string]any{"model": model, "messages": msgs, "temperature": 0, "stream": false}
	if schema != nil {
		body["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "extraction", "strict": true, "schema": schema},
		}
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", l.BaseURL+"/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message chatMsg `json:"message"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("llm %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != 200 || len(out.Choices) == 0 {
		return "", fmt.Errorf("llm %d: %v", resp.StatusCode, out.Error)
	}
	return out.Choices[0].Message.Content, nil
}

type Extraction struct {
	Language           string   `json:"language"`
	Intent             string   `json:"intent"`
	SpecialtyID        *string  `json:"specialty_id"`
	NeedClarification  bool     `json:"need_clarification"`
	ClarifyingQuestion *string  `json:"clarifying_question"`
	Urgency            string   `json:"urgency"`
	UrgencyReason      string   `json:"urgency_reason"`
	Pregnant           *bool    `json:"pregnant"`
	GestationWeeks     *int     `json:"gestation_weeks"`
	AskPregnancy       bool     `json:"ask_pregnancy"`
	RiskFactors        []string `json:"risk_factors"`
	Summary            string   `json:"summary"`
}

func extractionSchema(c *Catalog) map[string]any {
	ids := []any{}
	for _, s := range c.Specialties {
		ids = append(ids, s.ID)
	}
	ids = append(ids, nil)
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"language":            map[string]any{"type": "string", "enum": []string{"ru", "kk", "en"}},
			"intent":              map[string]any{"type": "string", "enum": []string{"find_service", "service_info", "operator", "other"}},
			"specialty_id":        map[string]any{"enum": ids},
			"need_clarification":  map[string]any{"type": "boolean"},
			"clarifying_question": map[string]any{"type": []string{"string", "null"}},
			"urgency":             map[string]any{"type": "string", "enum": []string{"green", "yellow"}},
			"urgency_reason":      map[string]any{"type": "string"},
			"pregnant":            map[string]any{"type": []string{"boolean", "null"}},
			"gestation_weeks":     map[string]any{"type": []string{"integer", "null"}},
			"ask_pregnancy":       map[string]any{"type": "boolean"},
			"risk_factors":        map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": riskFactorIDs}},
			"summary":             map[string]any{"type": "string"},
		},
		"additionalProperties": false,
		"required":             []string{"language", "intent", "specialty_id", "need_clarification", "clarifying_question", "urgency", "urgency_reason", "pregnant", "gestation_weeks", "ask_pregnancy", "risk_factors", "summary"},
	}
}

// promptTreats: complaint keywords per specialty in the prompt. The prompt must fit a 4096-token context
// together with the dialog (see TestExtractionPromptFitsContext).
const promptTreats = 2 // per language: 2 Russian + 2 Kazakh complaint keywords

// promptTreatsOf keeps both languages: Kazakh keywords sit at the end of the list, and a plain
// cut dropped them ("тамағым ауырады" went to the gastroenterologist).
func promptTreatsOf(treats []string) []string {
	var ru, kk []string
	for _, x := range treats {
		if hasKazakhLetters(x) {
			if len(kk) < promptTreats {
				kk = append(kk, x)
			}
		} else if len(ru) < promptTreats {
			ru = append(ru, x)
		}
	}
	return append(ru, kk...)
}

func extractionPrompt(c *Catalog, t *TriageRules, clarLeft int, d *Dialog, lang string) string {
	var sb strings.Builder
	sb.WriteString(`Разбери сообщение пациента клиники. Верни ТОЛЬКО JSON по схеме. Диагноз не ставь.
- language: kk (казахский), en (английский), иначе ru.
- intent: find_service (жалоба, подобрать врача), service_info (вопрос об услуге/цене/враче/подготовке), operator (просит человека), other (приветствие, не по теме).
- specialty_id: id строго из списка ниже или null.
- need_clarification: true только если жалоба слишком общая («мне плохо»).
- clarifying_question: один короткий вопрос на языке пациента о СОПУТСТВУЮЩИХ симптомах именно для этой жалобы, которого ещё не было в диалоге. Примеры: головная боль → есть ли температура, давление, тошнота, травма головы; горло → температура, кашель, трудно глотать; живот → где именно болит, тошнота, стул; кашель → температура, одышка, мокрота; сыпь → зуд, где, после чего появилась. Не про диагноз. null, только если всё уже выяснено.
- urgency: yellow — нужна помощь в ближайшие 1–2 дня (сильная/нарастающая боль, высокая температура, кровь, травма, беременность, ребёнок с температурой), иначе green. urgency_reason: коротко, по-русски.
- pregnant: true/false, если сказано; иначе null. gestation_weeks: срок в неделях или null.
- ask_pregnancy: true, если женщина описывает боль внизу живота, кровянистые выделения, тошноту или задержку, а о беременности не сказано.
- risk_factors: только названные пациенткой: hypertension, diabetes, anemia, heart_disease, kidney_disease, endocrine, multiple_pregnancy, preterm_risk, previous_complications (прошлые выкидыш/кесарево/преэклампсия); иначе [].
- summary: 1–2 предложения по-русски для оператора: что хочет пациент, что выяснено (беременность и срок, если известны).

Специальности (id (название): типичные жалобы):
`)
	for _, s := range c.Specialties {
		treats := promptTreatsOf(s.Treats)
		fmt.Fprintf(&sb, "- %s (%s): %s\n", s.ID, s.Name, strings.Join(treats, ", "))
	}
	if lang == "" {
		lang = "ru"
	}
	fmt.Fprintf(&sb, "\nЯзык пациента: %s — clarifying_question пиши только на нём.", map[string]string{"ru": "русский", "kk": "қазақ тілі", "en": "English"}[lang])
	sb.WriteString("\nОриентиры для urgency=yellow: " + strings.Join(t.Yellow[lang], ", "))
	if d.Pregnant {
		sb.WriteString("\n\nИЗВЕСТНО: пациентка беременна")
		if d.GestationWeeks != nil {
			fmt.Fprintf(&sb, " (срок %d нед.)", *d.GestationWeeks)
		}
		sb.WriteString(". pregnant=true, ask_pregnancy=false. При беременности urgency=yellow и для таких жалоб: " + strings.Join(t.YellowIfPregnant[lang], ", "))
		sb.WriteString(". Пациентка беременна: specialty_id=gynecologist (акушер-гинеколог) для любых жалоб, кроме явно другой области (зубы, глаза, кожа, ЛОР).")
	} else if d.PregnancyAsked {
		sb.WriteString("\n\nВопрос о беременности уже задан: ask_pregnancy=false.")
	}
	if clarLeft <= 0 {
		sb.WriteString("\n\nЛимит уточнений исчерпан: need_clarification=false, выбери наиболее подходящую специальность или null.")
	}
	return sb.String()
}

const answerSystemPrompt = `Ты — вежливый администратор медицинской клиники в чате. Отвечай кратко (3-5 предложений), на языке пациента (%s: ru = русский, kk = қазақ тілі, en = English).
Строгие правила:
- НЕ ставь диагноз и не называй возможные болезни.
- НЕ назначай лечение, лекарства, дозировки, процедуры.
- Используй ТОЛЬКО услуги, цены, врачей и слоты из переданных ДАННЫХ. Ничего не придумывай. Если нужного нет в данных — так и скажи и предложи связаться с оператором.
- Цены пиши в тенге (₸).
- Карточки услуг пациент увидит отдельно, поэтому кратко подведи: к какому специалисту стоит обратиться и ближайшие свободные слоты.
- Добавь 1–2 общих безопасных совета до приёма (отдых, больше пить воды, не терпеть сильную боль, при каких признаках сразу звонить 103) — без названий лекарств, дозировок и диагнозов.`

// Extract runs the extraction with one retry on unparsable output; after that it returns
// the safe fallback (urgency=yellow, intent=operator) and ok=false.
func (l *LLM) Extract(ctx context.Context, msgs []chatMsg, c *Catalog) (ex Extraction, ok bool) {
	for attempt := 1; attempt <= 2; attempt++ {
		raw, err := l.Chat(ctx, msgs, extractionSchema(c))
		if err == nil {
			raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "```json"), "```"))
			if err = json.Unmarshal([]byte(raw), &ex); err == nil {
				log.Printf("extraction: %s", raw)
				return ex, true
			}
		}
		log.Printf("extraction attempt %d failed: %v (raw=%q)", attempt, err, raw)
	}
	return Extraction{Language: "ru", Intent: "operator", Urgency: "yellow",
		UrgencyReason: "ИИ не вернул корректный ответ — безопасный вариант"}, false
}

// riskFactorIDs: obstetric risk factors (AnaCare risk-oriented route).
var riskFactorIDs = []string{"hypertension", "diabetes", "anemia", "heart_disease", "kidney_disease", "endocrine",
	"multiple_pregnancy", "preterm_risk", "previous_complications"}

var riskFactorNames = map[string]string{
	"hypertension": "гипертензия", "diabetes": "диабет", "anemia": "анемия", "heart_disease": "болезни сердца",
	"kidney_disease": "болезни почек", "endocrine": "эндокринные нарушения", "multiple_pregnancy": "многоплодная беременность",
	"preterm_risk": "угроза преждевременных родов", "previous_complications": "осложнения прошлых беременностей",
}
