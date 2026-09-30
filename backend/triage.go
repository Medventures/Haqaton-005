package main

import (
	"encoding/json"
	"os"
	"strings"
	"unicode"
)

type redRule struct {
	Lang string   `json:"lang"`
	All  []string `json:"all"`
}

type TriageRules struct {
	Red    []redRule           `json:"red"`
	Yellow map[string][]string `json:"yellow"`
}

func LoadTriage(path string) (*TriageRules, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t TriageRules
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	for i := range t.Red { // rules go through the same normalization as input
		for j := range t.Red[i].All {
			t.Red[i].All[j] = normalize(t.Red[i].All[j])
		}
	}
	return &t, nil
}

// normalize: lower case, ё→е, punctuation → space, collapse spaces.
func normalize(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// CheckRed returns the matched rule and its language, or ok=false.
func (t *TriageRules) CheckRed(text string) (phrase, lang string, ok bool) {
	n := normalize(text)
	for _, r := range t.Red {
		all := len(r.All) > 0
		for _, p := range r.All {
			if !strings.Contains(n, p) {
				all = false
				break
			}
		}
		if all {
			return strings.Join(r.All, " + "), r.Lang, true
		}
	}
	return "", "", false
}

var urgencyRank = map[string]int{"green": 0, "yellow": 1, "red": 2}

// maxUrgency: the dialog level only goes up.
func maxUrgency(a, b string) string {
	if urgencyRank[b] > urgencyRank[a] {
		return b
	}
	return a
}

func hasKazakhLetters(s string) bool {
	return strings.ContainsAny(strings.ToLower(s), "әғқңөұүһі")
}
