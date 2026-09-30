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
	body := map[string]any{"model": l.Model, "messages": msgs, "temperature": 0, "stream": false}
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
	Language           string  `json:"language"`
	Intent             string  `json:"intent"`
	SpecialtyID        *string `json:"specialty_id"`
	NeedClarification  bool    `json:"need_clarification"`
	ClarifyingQuestion *string `json:"clarifying_question"`
	Urgency            string  `json:"urgency"`
	UrgencyReason      string  `json:"urgency_reason"`
	Summary            string  `json:"summary"`
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
			"language":            map[string]any{"type": "string", "enum": []string{"ru", "kk"}},
			"intent":              map[string]any{"type": "string", "enum": []string{"find_service", "service_info", "operator", "other"}},
			"specialty_id":        map[string]any{"enum": ids},
			"need_clarification":  map[string]any{"type": "boolean"},
			"clarifying_question": map[string]any{"type": []string{"string", "null"}},
			"urgency":             map[string]any{"type": "string", "enum": []string{"green", "yellow"}},
			"urgency_reason":      map[string]any{"type": "string"},
			"summary":             map[string]any{"type": "string"},
		},
		"additionalProperties": false,
		"required":             []string{"language", "intent", "specialty_id", "need_clarification", "clarifying_question", "urgency", "urgency_reason", "summary"},
	}
}

func extractionPrompt(c *Catalog, t *TriageRules, clarLeft int) string {
	var sb strings.Builder
	sb.WriteString(`Ты — модуль разбора сообщений пациента медицинской клиники. Верни ТОЛЬКО JSON по схеме.
Поля:
- language: "kk" если пациент пишет по-казахски, иначе "ru".
- intent: find_service (хочет подобрать врача/услугу по жалобе), service_info (спрашивает о конкретной услуге, цене, враче), operator (просит живого человека/оператора/администратора), other (приветствие, не по теме).
- specialty_id: id специальности СТРОГО из списка ниже, которая подходит под жалобу; null если непонятно.
- need_clarification: true, только если жалоба слишком общая и специальность выбрать нельзя (например, «мне плохо»). Если жалоба явно подходит под специальность (горло/нос → ent, сыпь → dermatologist) — false.
- clarifying_question: один короткий вопрос о симптомах (что беспокоит, где, как давно) на языке пациента, или null. Никогда не спрашивай про диагноз или заболевание. Не спрашивай то, что уже известно.
- urgency: "yellow", если стоит обратиться за помощью в ближайшее время (сутки-двое): сильная или нарастающая боль, высокая температура, кровь, травма, беременность, ребёнок с температурой; иначе "green".
- urgency_reason: коротко почему (на русском).
- summary: 1-2 предложения на русском для оператора: что хочет пациент и что выяснено.
Не ставь диагноз.

Специальности (id — название: описание; жалобы):
`)
	for _, s := range c.Specialties {
		fmt.Fprintf(&sb, "- %s — %s: %s; %s\n", s.ID, s.Name, s.Description, strings.Join(s.Treats, ", "))
	}
	sb.WriteString("\nОриентиры для urgency=yellow (ru): " + strings.Join(t.Yellow["ru"], ", "))
	sb.WriteString("\nОриентиры для urgency=yellow (kk): " + strings.Join(t.Yellow["kk"], ", "))
	if clarLeft <= 0 {
		sb.WriteString("\n\nЛимит уточнений исчерпан: need_clarification=false, выбери наиболее подходящую специальность или null.")
	}
	return sb.String()
}

const answerSystemPrompt = `Ты — вежливый администратор медицинской клиники в чате. Отвечай кратко (2-4 предложения), на языке пациента (%s: ru = русский, kk = қазақ тілі).
Строгие правила:
- НЕ ставь диагноз и не называй возможные болезни.
- НЕ назначай лечение, лекарства, дозировки, процедуры.
- Используй ТОЛЬКО услуги, цены, врачей и слоты из переданных ДАННЫХ. Ничего не придумывай. Если нужного нет в данных — так и скажи и предложи связаться с оператором.
- Цены пиши в тенге (₸).
- Карточки услуг пациент увидит отдельно, поэтому просто кратко подведи: к какому специалисту стоит обратиться и ближайшие свободные слоты.`

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
