package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

type redRule struct {
	Lang string   `json:"lang"`
	All  []string `json:"all"`
}

type TriageRules struct {
	Red              []redRule           `json:"red"`
	Yellow           map[string][]string `json:"yellow"`
	RedIfPregnant    []redRule           `json:"red_if_pregnant"`
	YellowIfPregnant map[string][]string `json:"yellow_if_pregnant"`
	AskPregnancyIf   map[string][]string `json:"ask_pregnancy_if"`
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
	for _, rules := range [][]redRule{t.Red, t.RedIfPregnant} { // rules go through the same normalization as input
		for i := range rules {
			for j := range rules[i].All {
				rules[i].All[j] = normalize(rules[i].All[j])
			}
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
// With pregnant=true the red_if_pregnant rules are checked too.
func (t *TriageRules) CheckRed(text string, pregnant bool) (phrase, lang string, ok bool) {
	rules := t.Red
	if pregnant {
		rules = append(append([]redRule{}, t.Red...), t.RedIfPregnant...)
	}
	n := normalize(text)
	for _, r := range rules {
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

// Pregnancy markers (on normalized text). A bare "апта"/"неделя" is NOT enough:
// "екі апта бойы тамағым ауырады" = "throat hurts for two weeks".
var pregnancyRe = []*regexp.Regexp{
	regexp.MustCompile(`беремен`),
	regexp.MustCompile(`в положении`),
	regexp.MustCompile(`жду ребенка`),
	regexp.MustCompile(`срок\D{0,20}\d+`),
	regexp.MustCompile(`\d+\s*(й|я|ой)?\s*недел\S*\s+(срок|беремен)`),
	regexp.MustCompile(`на\s+\d+\s*(й|ой)?\s*неделе`),
	regexp.MustCompile(`жүкті`),
	regexp.MustCompile(`екіқабат|екі қабат`),
	regexp.MustCompile(`аяғым ауыр|аяғы ауыр`),
	regexp.MustCompile(`мерзім\D{0,20}\d+\s*апта`),
	regexp.MustCompile(`\d+\s*(аптадамын|аптасындамын|аптадамыз)`),
	regexp.MustCompile(`\d+\s*апталық`),
}

// Negations are removed before matching: "я не беременна" must not set the flag.
var pregnancyNegRe = regexp.MustCompile(`не\s+беремен\S*|беременност\S*\s+(нет|исключена)|нет\s+беременност\S*|жүкті\s+емес\S*|жүктілік\s+жоқ|жүктілігім\s+жоқ`)

var weeksRe = regexp.MustCompile(`(\d{1,2})\s*(й|я|ой|ші|шы|інші|ыншы)?\s*(недел|апта)`)

// DetectPregnancy: does the text say the patient is pregnant, and the gestation week if mentioned.
func DetectPregnancy(text string) (bool, *int) {
	n := pregnancyNegRe.ReplaceAllString(normalize(text), " ")
	found := false
	for _, re := range pregnancyRe {
		if re.MatchString(n) {
			found = true
			break
		}
	}
	if !found {
		return false, nil
	}
	if m := weeksRe.FindStringSubmatch(n); m != nil {
		if w, err := strconv.Atoi(m[1]); err == nil && w >= 1 && w <= 42 {
			return true, &w
		}
	}
	return true, nil
}

// NeedsPregnancyQuestion: ob/gyn symptoms that require asking about pregnancy (safety net over the LLM).
func (t *TriageRules) NeedsPregnancyQuestion(text string) bool {
	n := normalize(text)
	for _, list := range t.AskPregnancyIf {
		for _, p := range list {
			if strings.Contains(n, normalize(p)) {
				return true
			}
		}
	}
	return false
}
