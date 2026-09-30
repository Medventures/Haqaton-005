package main

// MIS (clinic information system) integration: machine-to-machine API authenticated with X-API-Key
// (env INTEGRATION_API_KEY; empty = integration disabled, 503).

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
)

// integrationAuth checks X-API-Key against INTEGRATION_API_KEY in constant time.
func integrationAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := os.Getenv("INTEGRATION_API_KEY")
		if key == "" {
			writeErr(w, 503, "integration disabled")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-API-Key")), []byte(key)) != 1 {
			writeErr(w, 401, "invalid api key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// normalizeSlots validates "YYYY-MM-DDTHH:MM" slots, removes duplicates and sorts them.
func normalizeSlots(slots []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range slots {
		if _, err := time.Parse(slotLayout, s); err != nil {
			return nil, fmt.Errorf("slot %q: want YYYY-MM-DDTHH:MM", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

// validateCatalog checks unique non-empty ids, specialty references and slot format; slots are normalized.
func validateCatalog(c *Catalog) error {
	if len(c.Specialties) == 0 {
		return fmt.Errorf("specialties: empty")
	}
	specs := map[string]bool{}
	for _, s := range c.Specialties {
		if s.ID == "" || s.Name == "" {
			return fmt.Errorf("specialty: id and name are required")
		}
		if specs[s.ID] {
			return fmt.Errorf("specialty %q: duplicate id", s.ID)
		}
		specs[s.ID] = true
	}
	svcs := map[string]bool{}
	for _, s := range c.Services {
		if s.ID == "" || s.Name == "" {
			return fmt.Errorf("service: id and name are required")
		}
		if svcs[s.ID] {
			return fmt.Errorf("service %q: duplicate id", s.ID)
		}
		svcs[s.ID] = true
		if !specs[s.SpecialtyID] {
			return fmt.Errorf("service %q: unknown specialty %q", s.ID, s.SpecialtyID)
		}
	}
	docs := map[string]bool{}
	for i, d := range c.Doctors {
		if d.ID == "" || d.Name == "" {
			return fmt.Errorf("doctor: id and name are required")
		}
		if docs[d.ID] {
			return fmt.Errorf("doctor %q: duplicate id", d.ID)
		}
		docs[d.ID] = true
		if !specs[d.SpecialtyID] {
			return fmt.Errorf("doctor %q: unknown specialty %q", d.ID, d.SpecialtyID)
		}
		slots, err := normalizeSlots(d.Slots)
		if err != nil {
			return fmt.Errorf("doctor %q: %v", d.ID, err)
		}
		c.Doctors[i].Slots = slots
	}
	return nil
}

// PUT /api/integration/catalog — atomically replace the whole catalog (booked slots are hidden).
func (a *App) integrationCatalog(w http.ResponseWriter, r *http.Request) {
	var in Catalog
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 5<<20)).Decode(&in); err != nil {
		writeErr(w, 400, "bad json: "+err.Error())
		return
	}
	if err := validateCatalog(&in); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	booked, err := a.bookedSlots(r.Context(), "")
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	for i := range in.Doctors {
		in.Doctors[i].Slots = withoutSlots(in.Doctors[i].Slots, booked[in.Doctors[i].ID])
	}
	a.cat.mu.Lock()
	a.cat.Specialties, a.cat.Services, a.cat.Doctors = in.Specialties, in.Services, in.Doctors
	a.cat.mu.Unlock()
	writeJSON(w, 200, map[string]int{"specialties": len(in.Specialties), "services": len(in.Services), "doctors": len(in.Doctors)})
}

// PUT /api/integration/doctors/{id}/slots — replace one doctor's free slots (booked slots are hidden).
func (a *App) integrationDoctorSlots(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var in struct {
		Slots []string `json:"slots"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil || in.Slots == nil {
		writeErr(w, 400, `body must be {"slots": [...]}`)
		return
	}
	slots, err := normalizeSlots(in.Slots)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if _, ok := a.cat.Doctor(id); !ok {
		writeErr(w, 404, "doctor not found")
		return
	}
	booked, err := a.bookedSlots(r.Context(), id)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	slots = withoutSlots(slots, booked[id])
	if !a.cat.updateSlots(id, func([]string) []string { return slots }) {
		writeErr(w, 404, "doctor not found")
		return
	}
	writeJSON(w, 200, map[string]any{"doctor_id": id, "slots": slots})
}

// GET /api/integration/appointments?since=RFC3339 — appointments created or changed (e.g. cancelled)
// at or after since, oldest change first. The MIS de-duplicates by id.
func (a *App) integrationAppointments(w http.ResponseWriter, r *http.Request) {
	since := time.Time{}
	if s := r.URL.Query().Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeErr(w, 400, "since must be RFC3339, e.g. 2026-10-01T00:00:00Z")
			return
		}
		since = t
	}
	rows, err := a.db.Query(r.Context(), `select `+apptCols+` from appointments where updated_at >= $1 order by updated_at, id limit 5000`, since)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	out, err := scanAppointments(rows)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	writeJSON(w, 200, out)
}
