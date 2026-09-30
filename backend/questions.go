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

// Question returns the n-th question (0-based) for the complaint type that best matches text, or "".
// Keywords match from a word start ("ног" is in "ноги", not in "много").
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
	if best < 0 || n >= len(q.Categories[best].Questions) {
		return "", ""
	}
	qs := q.Categories[best].Questions[n]
	if s := qs[lang]; s != "" {
		return q.Categories[best].ID, s
	}
	return q.Categories[best].ID, qs["ru"]
}
