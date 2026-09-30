package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// QuestionBank: curated clarifying questions per complaint type (questions.json). The model's Kazakh
// questions were often broken, so for known complaint types the bot asks these instead.
type QuestionBank struct {
	Categories []struct {
		ID        string              `json:"id"`
		Keywords  []string            `json:"keywords"`
		Questions []map[string]string `json:"questions"`
	} `json:"categories"`
}

func LoadQuestions(path string) (*QuestionBank, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &QuestionBank{}, nil
	}
	if err != nil {
		return nil, err
	}
	var q QuestionBank
	if err := json.Unmarshal(b, &q); err != nil {
		return nil, err
	}
	for i := range q.Categories {
		for j, k := range q.Categories[i].Keywords {
			q.Categories[i].Keywords[j] = normalize(k)
		}
	}
	return &q, nil
}

// Question returns the n-th not-yet-answered question for the complaint type that best matches text, or "".
// A question is skipped when everything it asks about was already mentioned (fever, cough, swelling…).
func (q *QuestionBank) Question(text string, n int, lang string) (category, question string) {
	words := " " + normalize(text)
	best, bestHits := -1, 0
	for i, c := range q.Categories {
		hits := 0
		for _, k := range c.Keywords {
			if k != "" && strings.Contains(words, " "+k) {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = i, hits
		}
	}
	if best < 0 {
		return "", ""
	}
	qs := q.Categories[best].Questions
	skipped := 0
	for i := 0; i < len(qs); i++ {
		if alreadyAnswered(qs[i], words) {
			continue
		}
		if skipped == n {
			if s := qs[i][lang]; s != "" {
				return q.Categories[best].ID, s
			}
			return q.Categories[best].ID, qs[i]["ru"]
		}
		skipped++
	}
	return q.Categories[best].ID, ""
}

// symptom topics: a question about a topic the patient already mentioned is not asked again
var answerTopics = [][]string{
	{"температур", "кызу", "кызуы", "fever"},
	{"кашел", "кашл", "жотел", "cough"},
	{"насморк", "нос залож", "мурын", "тумау", "runny"},
	{"отек", "отеч", "исин", "swell"},
	{"тошн", "рвот", "журек айн", "кусу", "nause", "vomit"},
	{"давлен", "кысым", "pressure"},
}

func alreadyAnswered(q map[string]string, patient string) bool {
	qt := normalize(q["ru"] + " " + q["kk"] + " " + q["en"])
	asked, known := 0, 0
	for _, topic := range answerTopics {
		inQ, inP := false, false
		for _, w := range topic {
			if strings.Contains(qt, w) {
				inQ = true
			}
			if strings.Contains(patient, w) {
				inP = true
			}
		}
		if inQ {
			asked++
			if inP {
				known++
			}
		}
	}
	return asked > 0 && asked == known
}

// Next returns the first question of the matching complaint type that the bot has not asked yet
// and whose topics the patient has not already covered.
func (q *QuestionBank) Next(patientText string, asked []string, lang string) (category, question string) {
	cat, _ := q.Question(patientText, 0, lang)
	if cat == "" {
		return "", ""
	}
	words := " " + normalize(patientText)
	was := map[string]bool{}
	for _, a := range asked {
		was[strings.TrimSpace(a)] = true
	}
	for _, c := range q.Categories {
		if c.ID != cat {
			continue
		}
		for _, qs := range c.Questions {
			text := qs[lang]
			if text == "" {
				text = qs["ru"]
			}
			if was[text] || alreadyAnswered(qs, words) {
				continue
			}
			return cat, text
		}
	}
	return cat, ""
}
