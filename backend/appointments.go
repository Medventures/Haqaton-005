package main

// Booking: patients book a free doctor slot from the catalog; the slot disappears from the in-memory
// catalog and comes back on cancel. The DB unique index (doctor_id, slot) where status='booked' is the
// final guard against double booking.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const slotLayout = "2006-01-02T15:04"

type Appointment struct {
	ID          string    `json:"id"`
	DialogID    *string   `json:"dialog_id"`
	PatientID   string    `json:"patient_id"`
	DoctorID    string    `json:"doctor_id"`
	ServiceID   string    `json:"service_id"`
	Slot        string    `json:"slot"`
	PatientName string    `json:"patient_name"`
	Phone       string    `json:"phone"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const apptCols = `id, dialog_id, patient_id, doctor_id, service_id, slot, patient_name, phone, status, created_at, updated_at`

func scanAppointments(rows pgx.Rows) ([]Appointment, error) {
	defer rows.Close()
	out := []Appointment{}
	for rows.Next() {
		var ap Appointment
		if err := rows.Scan(&ap.ID, &ap.DialogID, &ap.PatientID, &ap.DoctorID, &ap.ServiceID, &ap.Slot,
			&ap.PatientName, &ap.Phone, &ap.Status, &ap.CreatedAt, &ap.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ap)
	}
	return out, rows.Err()
}

// Doctor returns a copy of the doctor (its Slots slice is never modified in place).
func (c *Catalog) Doctor(id string) (Doctor, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, d := range c.Doctors {
		if d.ID == id {
			return d, true
		}
	}
	return Doctor{}, false
}

// updateSlots replaces one doctor's slots via fn (copy-on-write); false if the doctor is unknown.
func (c *Catalog) updateSlots(doctorID string, fn func([]string) []string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Doctors {
		if c.Doctors[i].ID == doctorID {
			docs := append([]Doctor(nil), c.Doctors...)
			docs[i].Slots = fn(docs[i].Slots)
			c.Doctors = docs
			return true
		}
	}
	return false
}

func (c *Catalog) removeSlot(doctorID, slot string) {
	c.updateSlots(doctorID, func(old []string) []string {
		out := []string{}
		for _, s := range old {
			if s != slot {
				out = append(out, s)
			}
		}
		return out
	})
}

func (c *Catalog) returnSlot(doctorID, slot string) {
	c.updateSlots(doctorID, func(old []string) []string {
		if slices.Contains(old, slot) {
			return old
		}
		out := append(append([]string{}, old...), slot)
		sort.Strings(out)
		return out
	})
}

// bookedSlots: doctor_id -> set of booked slots (all doctors when doctorID is "").
func (a *App) bookedSlots(ctx context.Context, doctorID string) (map[string]map[string]bool, error) {
	rows, err := a.db.Query(ctx, `select doctor_id, slot from appointments where status='booked' and ($1='' or doctor_id=$1)`, doctorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]bool{}
	for rows.Next() {
		var d, s string
		if err := rows.Scan(&d, &s); err != nil {
			return nil, err
		}
		if out[d] == nil {
			out[d] = map[string]bool{}
		}
		out[d][s] = true
	}
	return out, rows.Err()
}

// hideBookedSlots removes already booked slots from the catalog (at startup: catalog.json does not know them).
func (a *App) hideBookedSlots(ctx context.Context) error {
	booked, err := a.bookedSlots(ctx, "")
	if err != nil {
		return err
	}
	for doc, slots := range booked {
		a.cat.updateSlots(doc, func(old []string) []string { return withoutSlots(old, slots) })
	}
	return nil
}

func withoutSlots(slots []string, booked map[string]bool) []string {
	out := []string{}
	for _, s := range slots {
		if !booked[s] {
			out = append(out, s)
		}
	}
	return out
}

var phoneRe = regexp.MustCompile(`^(\+7|8)\d{10}$`)

// validPhone: Kazakhstan number +7XXXXXXXXXX or 8XXXXXXXXXX; spaces, dashes and brackets allowed.
func validPhone(p string) bool {
	p = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(strings.TrimSpace(p))
	return phoneRe.MatchString(p)
}

var bookedTexts = map[string]string{
	"ru": "Вы записаны: %s, %s, %s.",
	"kk": "Сіз жазылдыңыз: %s, %s, %s.",
	"en": "You are booked: %s, %s, %s.",
}

func bookedText(lang, service, doctor, slot string) string {
	f, ok := bookedTexts[lang]
	if !ok {
		f = bookedTexts["ru"]
	}
	when := slot
	if t, err := time.Parse(slotLayout, slot); err == nil {
		when = t.Format("02.01.2006 15:04")
	}
	return fmt.Sprintf(f, service, doctor, when)
}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// POST /api/appointments (patient)
func (a *App) createAppointment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	var in struct {
		DoctorID    string  `json:"doctor_id"`
		ServiceID   string  `json:"service_id"`
		Slot        string  `json:"slot"`
		DialogID    *string `json:"dialog_id"`
		PatientName string  `json:"patient_name"`
		Phone       string  `json:"phone"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in) != nil {
		writeErr(w, 400, "bad json")
		return
	}
	in.PatientName = strings.TrimSpace(in.PatientName)
	in.Phone = strings.TrimSpace(in.Phone)
	doc, ok := a.cat.Doctor(in.DoctorID)
	if !ok {
		writeErr(w, 400, "unknown doctor")
		return
	}
	svc := a.cat.Service(in.ServiceID)
	if svc == nil {
		writeErr(w, 400, "unknown service")
		return
	}
	if svc.SpecialtyID != doc.SpecialtyID {
		writeErr(w, 400, "service does not match the doctor's specialty")
		return
	}
	if n := utf8.RuneCountInString(in.PatientName); n < 2 || n > 100 {
		writeErr(w, 400, "patient_name must be 2-100 characters")
		return
	}
	if !validPhone(in.Phone) {
		writeErr(w, 400, "phone must be a Kazakhstan number: +7XXXXXXXXXX or 8XXXXXXXXXX")
		return
	}
	var dlg *Dialog
	if in.DialogID != nil && *in.DialogID != "" {
		if _, err := uuid.Parse(*in.DialogID); err != nil {
			writeErr(w, 404, "dialog not found")
			return
		}
		d, err := a.getDialog(ctx, *in.DialogID)
		if err != nil || d.PatientID != u.ID {
			writeErr(w, 404, "dialog not found")
			return
		}
		dlg = d
	} else {
		in.DialogID = nil
	}
	if _, err := time.Parse(slotLayout, in.Slot); err != nil {
		writeErr(w, 400, "slot must be YYYY-MM-DDTHH:MM")
		return
	}
	if !slices.Contains(doc.Slots, in.Slot) {
		var taken bool
		a.db.QueryRow(ctx, `select exists(select 1 from appointments where doctor_id=$1 and slot=$2 and status='booked')`, doc.ID, in.Slot).Scan(&taken)
		if taken {
			writeErr(w, 409, "slot is already booked")
		} else {
			writeErr(w, 400, "slot is not in the doctor's free slots")
		}
		return
	}

	var ap Appointment
	err := a.db.QueryRow(ctx, `insert into appointments(dialog_id, patient_id, doctor_id, service_id, slot, patient_name, phone)
		values($1,$2,$3,$4,$5,$6,$7) returning `+apptCols, in.DialogID, u.ID, doc.ID, svc.ID, in.Slot, in.PatientName, in.Phone).
		Scan(&ap.ID, &ap.DialogID, &ap.PatientID, &ap.DoctorID, &ap.ServiceID, &ap.Slot, &ap.PatientName, &ap.Phone, &ap.Status, &ap.CreatedAt, &ap.UpdatedAt)
	if isUniqueViolation(err) {
		a.cat.removeSlot(doc.ID, in.Slot)
		writeErr(w, 409, "slot is already booked")
		return
	}
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	a.cat.removeSlot(doc.ID, in.Slot)

	resp := map[string]any{"appointment": ap}
	if dlg != nil {
		name, _ := svc.Localized(dlg.Language)
		if m, err := a.addMessage(ctx, dlg.ID, "bot", "bot", bookedText(dlg.Language, name, doc.Name, in.Slot),
			map[string]any{"appointment_id": ap.ID}); err == nil {
			resp["reply"] = m
		}
	}
	writeJSON(w, 201, resp)
}

// GET /api/appointments — patient: own; operator: all. Newest first.
func (a *App) listAppointments(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	patient := ""
	if u.Role == "patient" {
		patient = u.ID
	}
	rows, err := a.db.Query(r.Context(), `select `+apptCols+` from appointments where ($1='' or patient_id=$1)
		order by created_at desc, id limit 1000`, patient)
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

// POST /api/appointments/{id}/cancel — patient: own; operator: any. The slot returns to the doctor.
func (a *App) cancelAppointment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		writeErr(w, 404, "appointment not found")
		return
	}
	patient := ""
	if u.Role == "patient" {
		patient = u.ID
	}
	var status string
	if err := a.db.QueryRow(ctx, `select status from appointments where id=$1 and ($2='' or patient_id=$2)`, id, patient).Scan(&status); err != nil {
		writeErr(w, 404, "appointment not found")
		return
	}
	rows, err := a.db.Query(ctx, `update appointments set status='cancelled', updated_at=now() where id=$1 and status='booked' returning `+apptCols, id)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	out, err := scanAppointments(rows)
	if err != nil {
		writeErr(w, 500, "db error")
		return
	}
	if len(out) == 0 {
		writeErr(w, 409, "appointment is already cancelled")
		return
	}
	a.cat.returnSlot(out[0].DoctorID, out[0].Slot)
	writeJSON(w, 200, out[0])
}
