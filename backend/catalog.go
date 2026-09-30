package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
)

// I18n holds translations: {"kk": {"name": ..., "description": ...}, "en": {...}}; base fields are Russian.
type I18n map[string]map[string]string

type Specialty struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Treats      []string `json:"treats"`
	I18n        I18n     `json:"i18n,omitempty"`
}

type Service struct {
	ID          string `json:"id"`
	SpecialtyID string `json:"specialty_id"`
	Name        string `json:"name"`
	Price       int    `json:"price"`
	Description string `json:"description"`
	I18n        I18n   `json:"i18n,omitempty"`
}

type Doctor struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	SpecialtyID string   `json:"specialty_id"`
	Slots       []string `json:"slots"`
}

// Catalog is shared between requests and can be changed at runtime (bookings, MIS sync): readers take
// mu.RLock; writers never modify slices in place but swap in new ones under mu.Lock (copy-on-write).
type Catalog struct {
	mu          sync.RWMutex
	Specialties []Specialty `json:"specialties"`
	Services    []Service   `json:"services"`
	Doctors     []Doctor    `json:"doctors"`
}

func LoadCatalog(path string) (*Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Catalog
	return &c, json.Unmarshal(b, &c)
}

func (c *Catalog) Specialty(id string) *Specialty {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Specialties {
		if c.Specialties[i].ID == id {
			return &c.Specialties[i]
		}
	}
	return nil
}

func (c *Catalog) Service(id string) *Service {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Services {
		if c.Services[i].ID == id {
			return &c.Services[i]
		}
	}
	return nil
}

func (c *Catalog) ServicesFor(specID string) []Service {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []Service{}
	for _, s := range c.Services {
		if s.SpecialtyID == specID {
			out = append(out, s)
		}
	}
	return out
}

// DoctorsFor returns only doctors that have free slots.
func (c *Catalog) DoctorsFor(specID string) []Doctor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []Doctor{}
	for _, d := range c.Doctors {
		if d.SpecialtyID == specID && len(d.Slots) > 0 {
			out = append(out, d)
		}
	}
	return out
}

// GET /api/catalog — a consistent snapshot of the catalog.
func (a *App) catalogHandler(w http.ResponseWriter, r *http.Request) {
	a.cat.mu.RLock()
	snap := struct {
		Specialties []Specialty `json:"specialties"`
		Services    []Service   `json:"services"`
		Doctors     []Doctor    `json:"doctors"`
	}{a.cat.Specialties, a.cat.Services, a.cat.Doctors}
	a.cat.mu.RUnlock()
	writeJSON(w, 200, snap)
}

// Localized returns name/description in lang (ru = base fields).
func (s Service) Localized(lang string) (string, string) {
	if tr, ok := s.I18n[lang]; ok && tr["name"] != "" {
		return tr["name"], tr["description"]
	}
	return s.Name, s.Description
}

func (s Specialty) Localized(lang string) (string, string) {
	if tr, ok := s.I18n[lang]; ok && tr["name"] != "" {
		return tr["name"], tr["description"]
	}
	return s.Name, s.Description
}

// GuessSpecialty picks the specialty whose complaint phrases (treats, ru/kk) best match the text:
// a phrase counts when every word stem of it (first 4 letters; «бол…» for all forms of боль) occurs in the text
// («боль в горле» ~ «болит горло»). A fallback when the model returns no specialty; ties and no hits give "".
func (c *Catalog) GuessSpecialty(text string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	have := map[string]bool{}
	for _, st := range wordStems(text) {
		have[st] = true
	}
	best, bestScore, tie := "", 0, false
	for _, s := range c.Specialties {
		score := 0
		for _, tr := range s.Treats {
			stems := wordStems(tr)
			ok := len(stems) > 0
			for _, st := range stems {
				ok = ok && have[st]
			}
			if ok {
				score += len(stems) // multi-word phrases weigh more
			}
		}
		switch {
		case score > bestScore:
			best, bestScore, tie = s.ID, score, false
		case score == bestScore && score > 0:
			tie = true
		}
	}
	if tie {
		return ""
	}
	return best
}

func wordStems(text string) []string {
	var out []string
	for _, w := range strings.Fields(normalize(text)) {
		r := []rune(w)
		switch {
		case len(r) < 3:
			continue
		case strings.HasPrefix(w, "бол"): // боль / болит / болят / болела
			out = append(out, "бол")
		case len(r) < 4:
			out = append(out, w)
		default:
			out = append(out, string(r[:4]))
		}
	}
	return out
}
