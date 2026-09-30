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
	RedExclude       map[string][]string `json:"red_exclude"`
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
	for lang, list := range t.RedExclude {
		for i, p := range list {
			end := strings.HasSuffix(p, "$")
			p = normalize(strings.TrimSuffix(p, "$"))
			if end {
				p += "$"
			}
			t.RedExclude[lang][i] = p
		}
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

// kkFold maps Kazakh-specific letters to the Russian look-alikes patients type on a Russian keyboard
// ("есинен танып калды" = "есінен танып қалды"). Rules, keywords and input are all folded, so both spellings match.
var kkFold = strings.NewReplacer("ә", "а", "ғ", "г", "қ", "к", "ң", "н", "ө", "о", "ұ", "у", "ү", "у", "һ", "х", "і", "и")

// normalize: lower case, ё→е, Kazakh letters folded (kkFold), punctuation → space, collapse spaces.
func normalize(s string) string {
	s = kkFold.Replace(strings.ReplaceAll(strings.ToLower(s), "ё", "е"))
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// clauseSplit cuts a message into parts that are checked separately:
// "боли в груди нет, болит горло" -> the negation belongs only to the first part.
var clauseSplit = regexp.MustCompile(`[.,!?;:\n]+|\s+(но|а|but|бірақ|алайда)\s+`)

var ellipsis = regexp.MustCompile(`\.{2,}|…`)

// CheckRed returns the matched rule and its language, or ok=false.
// With pregnant=true the red_if_pregnant rules are checked too.
// A part with an exclusion (negation, distant past, history, "грудной ребёнок") is not red — the LLM assesses it.
func (t *TriageRules) CheckRed(text string, pregnant bool) (phrase, lang string, ok bool) {
	rules := t.Red
	if pregnant {
		rules = append(append([]redRule{}, t.Red...), t.RedIfPregnant...)
	}
	// an ellipsis is a pause, not a clause boundary: "мне трудно... дышать"
	text = ellipsis.ReplaceAllString(strings.ToLower(text), " ")
	for _, part := range clauseSplit.Split(text, -1) {
		n := normalize(part)
		if n == "" {
			continue
		}
		for _, r := range rules {
			all := len(r.All) > 0
			for _, p := range r.All {
				if !strings.Contains(n, p) {
					all = false
					break
				}
			}
			if all && !t.excluded(n, r.All) {
				return strings.Join(r.All, " + "), r.Lang, true
			}
		}
	}
	return "", "", false
}

// excluded: an exclusion phrase occurs in the part. Phrases match from a word start; a last word shorter
// than 5 letters must match whole. A trailing "$" means "at the end of the part": "боли в груди нет" is
// excluded, "нет воздуха" is not. A "$" phrase is skipped when the matched rule itself ends with it:
// in "мама упала сознания нет" / "анам есі жоқ" the "нет"/"жоқ" is the symptom, not a negation.
func (t *TriageRules) excluded(part string, rule []string) bool {
	words := " " + part + " "
	for _, list := range t.RedExclude {
		for _, p := range list {
			last := p
			if i := strings.LastIndex(p, " "); i >= 0 {
				last = p[i+1:]
			}
			if strings.HasSuffix(p, "$") { // only at the end of the part
				e := strings.TrimSuffix(p, "$")
				own := false
				for _, f := range rule {
					own = own || strings.HasSuffix(" "+f, " "+e)
				}
				if !own && strings.HasSuffix(words, " "+e+" ") {
					return true
				}
				continue
			}
			needle := " " + p
			if len([]rune(last)) < 5 {
				needle += " "
			}
			if strings.Contains(words, needle) {
				return true
			}
		}
	}
	return false
}

var urgencyRank = map[string]int{"green": 0, "yellow": 1, "red": 2}

// maxUrgency: the dialog level only goes up.
func maxUrgency(a, b string) string {
	if urgencyRank[b] > urgencyRank[a] {
		return b
	}
	return a
}

// Kazakh words that do not occur in Russian, folded (kkFold): Kazakh typed without the special letters
// ("басым ауырады", "калай жазылуга болады") or mixed with Russian ("кеудем болит"). Matched as word prefixes.
var kkWords = []string{"ауыр", "жатыр", "керек", "рахмет", "калай", "кайда", "кашан", "канша", "болады", "келеди",
	"келмейди", "алмай", "истеймин", "истейсиз", "кеуде", "журег", "тамаг", "дариге", "емхана", "жукти", "аптадамын",
	"комектес", "салем", "басым"}

// Latin transliteration: Kazakh ("kudem auyrady") -> kk, Russian ("bolit grud") -> "" (the LLM/dialog decides).
var translitKK = []string{"auyr", "zhatyr", "kerek", "rahmet", "rakhmet", "salem", "kalay", "qalay", "kaida", "qaida",
	"kudem", "keudem", "dariger", "zhukti", "bolady", "tynys", "esinen", "komektes"}
var translitRU = []string{"bolit", "grud", "nuzhen", "nuzhno", "vrach", "pozhal", "pomogite", "zdravstv", "spasibo",
	"menya", "srochno", "dyshat", "soznan", "beremen", "skolko", "stoit", "zapisa", "golova", "zhivot", "gorlo"}

func hasWordPrefix(words []string, stems []string) bool {
	for _, w := range words {
		for _, st := range stems {
			if strings.HasPrefix(w, st) {
				return true
			}
		}
	}
	return false
}

// detectLanguage: kk by Kazakh-specific letters or Kazakh-only words, en by Latin script (unless it is
// a transliteration of Kazakh/Russian); "" = let the LLM decide.
func detectLanguage(s string) string {
	if hasKazakhLetters(s) {
		return "kk"
	}
	words := strings.Fields(normalize(s))
	latin, cyr := 0, 0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Latin, r):
			latin++
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
		}
	}
	if latin > 0 && latin > cyr*3 {
		switch {
		case hasWordPrefix(words, translitKK):
			return "kk"
		case hasWordPrefix(words, translitRU):
			return ""
		}
		return "en"
	}
	if hasWordPrefix(words, kkWords) {
		return "kk"
	}
	return ""
}

func hasKazakhLetters(s string) bool {
	return strings.ContainsAny(strings.ToLower(s), "әғқңөұүһі")
}

// kre compiles a pattern written with Kazakh letters against folded (normalized) text.
func kre(p string) *regexp.Regexp { return regexp.MustCompile(kkFold.Replace(p)) }

// Pregnancy markers (on normalized text). A bare "апта"/"неделя" is NOT enough:
// "екі апта бойы тамағым ауырады" = "throat hurts for two weeks".
// Abbreviations: "бер-ть", "берем 25 нед" (a bare "берем" is "we take"), "ж/ты", "preg"; transliteration: "beremenna", "zhuktimin".
var pregnancyRe = []*regexp.Regexp{
	kre(`беремен`),
	kre(`(^|\s)бер ть(\s|$)`),
	kre(`(^|\s)берем\s+\d{1,2}\s*(н|нед\S*|апт\S*|w)(\s|$)`),
	kre(`\d{1,2}\s*(нед|апт)\S*\s+берем(\s|$)`),
	kre(`в положении`),
	kre(`жду ребенка`),
	kre(`срок\D{0,20}\d+`),
	kre(`\d+\s*(й|я|ой)?\s*недел\S*\s+(срок|беремен)`),
	kre(`на\s+\d+\s*(й|ой)?\s*неделе`),
	kre(`жүкті`),
	kre(`(^|\s)ж ты(м|мын)?(\s|$)`),
	kre(`екіқабат|екі қабат`),
	kre(`аяғы(м)? ауыр(\s|$)`), // "аяғым ауыр" = pregnant; "аяғым ауырады" = my leg hurts
	kre(`мерзім\D{0,20}\d+\s*апта`),
	kre(`\d+\s*(аптадамын|аптасындамын|аптадамыз)`),
	kre(`\d+\s*апталық`),
	kre(`\bpregnan`),
	kre(`(^|\s)preg(\s|$)`),
	kre(`\bexpecting a baby`),
	kre(`\d+\s*weeks?\s+(pregnant|along)`),
	kre(`beremen|zhukti|zhykti|jukti`),
}

// Negations and non-pregnancy uses are removed before matching: "я не беременна", "хочу забеременеть",
// "планирую беременность", "тест на беременность отрицательный" must not set the (never cleared) flag.
var pregnancyNegRe = kre(`не\s+беремен\S*|беременност\S*\s+(нет|исключена)|нет\s+беременност\S*|не\s+в\s+положении|` +
	`\S*забеременет\S*|планир\S*\s+беременн\S*|беременн\S*\s+планир\S*|тест\S*\s+на\s+беременн\S*\s+отриц\S*|` +
	`жүкті\s+емес\S*|жүктілік\s+жоқ|жүктілігім\s+жоқ|жүкті\s+бол(ғым|ғысы|а\s+алмай)\S*|жүктілікті\s+жоспарла\S*|` +
	`not\s+pregnant|no\s+pregnancy|(trying|want|wants|planning)\s+to\s+(get|become)\s+pregnant|can\s*t\s+get\s+pregnant`)

var weeksRe = kre(`(\d{1,2})\s*(й|я|ой|ші|шы|інші|ыншы)?\s*(нед|апт|week|wk|w(\s|$)|nedel|apta)`)

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
