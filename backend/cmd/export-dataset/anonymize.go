package main

import "regexp"

// Rules are applied in order: emails first (they may contain digits), then
// cards before IIN (a 16-digit card contains 12 consecutive digits) and IIN
// before phones (an IIN starting with 8 looks like a phone prefix).
var (
	reEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+`)
	// 13-19 digits: either grouped by 4 (with spaces or dashes) or contiguous.
	reCard = regexp.MustCompile(`\b(?:\d{4}[ \-]){3}\d{4}(?:[ \-]?\d{1,3})?\b|\b\d{13,19}\b`)
	reIIN  = regexp.MustCompile(`\b\d{12}\b`)
	// +7 or 8, then 10 digits split by optional spaces, dashes or brackets.
	rePhone = regexp.MustCompile(`(?:\+7|\b8)[\s\-()]*\d{3}[\s\-()]*\d{3}[\s\-]*\d{2}[\s\-]*\d{2}\b`)
	reDate  = regexp.MustCompile(`\b\d{1,2}[./\-]\d{1,2}[./\-](?:19|20)\d{2}\b`)
	// Introduction phrase (case-insensitive, whole word) + 1-2 capitalized words.
	reName = regexp.MustCompile(`(^|[^\p{L}])((?i:меня\s+зовут|мо[её]\s+имя|менің\s+атым|атым|my\s+name\s+is))(\s*[:,\-—]?\s*)\p{Lu}[\p{L}'\-]*(?:\s+\p{Lu}[\p{L}'\-]*)?`)
)

// Anonymize masks personal data in a message before it goes into a training set.
func Anonymize(s string) string {
	s = reEmail.ReplaceAllString(s, "[EMAIL]")
	s = reCard.ReplaceAllString(s, "[КАРТА]")
	s = reIIN.ReplaceAllString(s, "[ИИН]")
	s = rePhone.ReplaceAllString(s, "[ТЕЛЕФОН]")
	s = reDate.ReplaceAllString(s, "[ДАТА]")
	s = reName.ReplaceAllString(s, "${1}${2}${3}[ИМЯ]")
	return s
}
