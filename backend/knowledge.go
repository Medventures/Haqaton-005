package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// KBEntry is one knowledge-base answer (address, hours, test preparation, ...). Prices never live here.
type KBEntry struct {
	ID         string              `json:"id"`
	Keywords   map[string][]string `json:"keywords"`
	Answer     map[string]string   `json:"answer"`
	ServiceIDs []string            `json:"service_ids"`
}

type Knowledge struct {
	Entries []KBEntry `json:"entries"`
}

// LoadKnowledge: a missing file gives an empty base (the bot then works from the catalog only).
func LoadKnowledge(path string) (*Knowledge, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Knowledge{}, nil
	}
	if err != nil {
		return nil, err
	}
	var k Knowledge
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, err
	}
	for i := range k.Entries {
		for lang, list := range k.Entries[i].Keywords {
			for j := range list {
				k.Entries[i].Keywords[lang][j] = normalize(list[j])
			}
		}
	}
	return &k, nil
}

// Search returns the entry with the most keyword hits (keywords of all languages) and the hit count, or nil.
// Ties go to the entry whose matched keywords are longer (more specific). allow filters entries (nil = all).
func (k *Knowledge) Search(text string, allow ...func(*KBEntry) bool) (*KBEntry, int) {
	n := normalize(text)
	var best *KBEntry
	bestScore, bestLen := 0, 0
next:
	for i := range k.Entries {
		for _, ok := range allow {
			if !ok(&k.Entries[i]) {
				continue next
			}
		}
		score, length := 0, 0
		for _, list := range k.Entries[i].Keywords {
			for _, kw := range list {
				if kw != "" && strings.Contains(n, kw) {
					score++
					length += len([]rune(kw))
				}
			}
		}
		if score > bestScore || score == bestScore && score > 0 && length > bestLen {
			best, bestScore, bestLen = &k.Entries[i], score, length
		}
	}
	return best, bestScore
}

// AnswerIn returns the entry's answer in lang, falling back to Russian.
func (e *KBEntry) AnswerIn(lang string) string {
	if a := e.Answer[lang]; a != "" {
		return a
	}
	return e.Answer["ru"]
}
