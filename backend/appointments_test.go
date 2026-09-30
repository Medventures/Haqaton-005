package main

import (
	"context"
	"slices"
	"testing"
)

type bookResp struct {
	Appointment Appointment `json:"appointment"`
	Reply       *Message    `json:"reply"`
	Error       string      `json:"error"`
}

func booking(doctor, service, slot string) map[string]any {
	return map[string]any{"doctor_id": doctor, "service_id": service, "slot": slot,
		"patient_name": "Айгерим Садыкова", "phone": "+7 701 123-45-67"}
}

func (e *testEnv) doctorSlots(id string) []string {
	e.t.Helper()
	var c struct {
		Doctors []Doctor `json:"doctors"`
	}
	if code := e.do("GET", "/api/catalog", "", nil, &c); code != 200 {
		e.t.Fatalf("catalog: %d", code)
	}
	for _, d := range c.Doctors {
		if d.ID == id {
			return d.Slots
		}
	}
	e.t.Fatalf("doctor %s not in catalog", id)
	return nil
}

func (e *testEnv) newDialog(patientID, lang string) string {
	e.t.Helper()
	var id string
	if err := e.a.db.QueryRow(context.Background(), `insert into dialogs(patient_id, language) values($1,$2) returning id`, patientID, lang).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func TestBookingHappyPath(t *testing.T) {
	e := setup(t)
	pid := "p-book-" + t.Name()
	tok := e.a.issue(pid, "patient")
	dlg := e.newDialog(pid, "ru")
	if !slices.Contains(e.doctorSlots("d1"), "2026-10-01T09:00") {
		t.Fatal("fixture: d1 must have 2026-10-01T09:00")
	}
	req := booking("d1", "ther-consult", "2026-10-01T09:00")
	req["dialog_id"] = dlg
	var r bookResp
	if code := e.do("POST", "/api/appointments", tok, req, &r); code != 201 {
		t.Fatalf("book: %d %+v", code, r)
	}
	if r.Appointment.Status != "booked" || r.Appointment.PatientID != pid || r.Appointment.DialogID == nil || *r.Appointment.DialogID != dlg {
		t.Fatalf("appointment: %+v", r.Appointment)
	}
	if slices.Contains(e.doctorSlots("d1"), "2026-10-01T09:00") {
		t.Fatal("booked slot must disappear from /api/catalog")
	}
	for _, d := range e.a.cat.DoctorsFor("therapist") {
		if d.ID == "d1" && slices.Contains(d.Slots, "2026-10-01T09:00") {
			t.Fatal("booked slot must disappear from DoctorsFor")
		}
	}
	want := "Вы записаны: Консультация терапевта, Ахметова Айгуль Серикқызы, 01.10.2026 09:00."
	if r.Reply == nil || r.Reply.Content != want {
		t.Fatalf("bot message: %+v, want %q", r.Reply, want)
	}
	msgs, _ := e.a.messages(context.Background(), dlg)
	if len(msgs) != 1 || msgs[0].Role != "bot" || msgs[0].Content != want {
		t.Fatalf("dialog messages: %+v", msgs)
	}
}

func TestBookingMessageInDialogLanguage(t *testing.T) {
	e := setup(t)
	pid := "p-kk-" + t.Name()
	req := booking("d2", "ther-consult", "2026-10-01T14:00")
	req["dialog_id"] = e.newDialog(pid, "kk")
	var r bookResp
	if code := e.do("POST", "/api/appointments", e.a.issue(pid, "patient"), req, &r); code != 201 {
		t.Fatalf("book: %d %+v", code, r)
	}
	svc, _ := e.a.cat.Service("ther-consult").Localized("kk")
	want := "Сіз жазылдыңыз: " + svc + ", Иванов Сергей Петрович, 01.10.2026 14:00."
	if r.Reply == nil || r.Reply.Content != want {
		t.Fatalf("kk message: %+v, want %q", r.Reply, want)
	}
}

func TestDoubleBooking409(t *testing.T) {
	e := setup(t)
	req := booking("d1", "ther-consult", "2026-10-01T11:30")
	if code := e.do("POST", "/api/appointments", e.patient(), req, nil); code != 201 {
		t.Fatalf("first: %d", code)
	}
	if code := e.do("POST", "/api/appointments", e.patient(), req, nil); code != 409 {
		t.Fatalf("second booking of the same slot: %d, want 409", code)
	}
	// even if the slot reappears in memory (e.g. stale MIS data), the DB index rejects it
	e.a.cat.returnSlot("d1", "2026-10-01T11:30")
	if code := e.do("POST", "/api/appointments", e.patient(), req, nil); code != 409 {
		t.Fatalf("DB guard: %d, want 409", code)
	}
	var n int
	e.a.db.QueryRow(context.Background(), `select count(*) from appointments where doctor_id='d1' and slot='2026-10-01T11:30'`).Scan(&n)
	if n != 1 {
		t.Fatalf("appointments for the slot: %d", n)
	}
}

func TestBookingForeignDialog404(t *testing.T) {
	e := setup(t)
	req := booking("d1", "ther-consult", "2026-10-01T09:00")
	req["dialog_id"] = e.newDialog("p-someone-else", "ru")
	if code := e.do("POST", "/api/appointments", e.patient(), req, nil); code != 404 {
		t.Fatalf("foreign dialog: %d, want 404", code)
	}
	req["dialog_id"] = "not-a-uuid"
	if code := e.do("POST", "/api/appointments", e.patient(), req, nil); code != 404 {
		t.Fatalf("bad dialog id: %d, want 404", code)
	}
	if !slices.Contains(e.doctorSlots("d1"), "2026-10-01T09:00") {
		t.Fatal("failed booking must not take the slot")
	}
}

func TestBookingValidation400(t *testing.T) {
	e := setup(t)
	tok := e.patient()
	cases := map[string]func(m map[string]any){
		"invalid phone":   func(m map[string]any) { m["phone"] = "12345" },
		"foreign phone":   func(m map[string]any) { m["phone"] = "+1 202 555 0100" },
		"wrong specialty": func(m map[string]any) { m["service_id"] = "card-consult" },
		"short name":      func(m map[string]any) { m["patient_name"] = " A " },
		"unknown doctor":  func(m map[string]any) { m["doctor_id"] = "nope" },
		"slot not free":   func(m map[string]any) { m["slot"] = "2026-10-01T10:00" },
		"slot format":     func(m map[string]any) { m["slot"] = "tomorrow" },
	}
	for name, mut := range cases {
		req := booking("d1", "ther-consult", "2026-10-01T09:00")
		mut(req)
		var r bookResp
		if code := e.do("POST", "/api/appointments", tok, req, &r); code != 400 {
			t.Errorf("%s: %d %q, want 400", name, code, r.Error)
		}
	}
	for _, p := range []string{"87011234567", "+7 (701) 123-45-67", "8-701-123-45-67"} {
		if !validPhone(p) {
			t.Errorf("valid KZ phone rejected: %q", p)
		}
	}
	if code := e.do("POST", "/api/appointments", e.operator(), booking("d1", "ther-consult", "2026-10-01T09:00"), nil); code != 403 {
		t.Fatalf("operator booking: %d, want 403", code)
	}
}

func TestCancelReturnsSlot(t *testing.T) {
	e := setup(t)
	tok := e.patient()
	var r bookResp
	if code := e.do("POST", "/api/appointments", tok, booking("d3", "card-consult", "2026-10-02T10:00"), &r); code != 201 {
		t.Fatalf("book: %d", code)
	}
	if slices.Contains(e.doctorSlots("d3"), "2026-10-02T10:00") {
		t.Fatal("slot must be taken")
	}
	if code := e.do("POST", "/api/appointments/"+r.Appointment.ID+"/cancel", e.patient(), nil, nil); code != 404 {
		t.Fatalf("other patient cancel: %d, want 404", code)
	}
	var c Appointment
	if code := e.do("POST", "/api/appointments/"+r.Appointment.ID+"/cancel", tok, nil, &c); code != 200 || c.Status != "cancelled" {
		t.Fatalf("cancel: %d %+v", code, c)
	}
	if !slices.Contains(e.doctorSlots("d3"), "2026-10-02T10:00") {
		t.Fatal("cancelled slot must return to the catalog")
	}
	if code := e.do("POST", "/api/appointments/"+r.Appointment.ID+"/cancel", e.operator(), nil, nil); code != 409 {
		t.Fatalf("second cancel: %d, want 409", code)
	}
	// the slot can be booked again; the operator can cancel any appointment
	if code := e.do("POST", "/api/appointments", e.patient(), booking("d3", "card-consult", "2026-10-02T10:00"), &r); code != 201 {
		t.Fatalf("rebook: %d", code)
	}
	if code := e.do("POST", "/api/appointments/"+r.Appointment.ID+"/cancel", e.operator(), nil, nil); code != 200 {
		t.Fatalf("operator cancel: %d", code)
	}
}

func TestListAppointmentsOperatorAllPatientOwn(t *testing.T) {
	e := setup(t)
	p1, p2 := e.a.issue("p-list-1", "patient"), e.a.issue("p-list-2", "patient")
	if code := e.do("POST", "/api/appointments", p1, booking("d1", "ther-consult", "2026-10-01T09:00"), nil); code != 201 {
		t.Fatalf("book1: %d", code)
	}
	if code := e.do("POST", "/api/appointments", p2, booking("d2", "ther-consult", "2026-10-03T10:00"), nil); code != 201 {
		t.Fatalf("book2: %d", code)
	}
	var own, all []Appointment
	e.do("GET", "/api/appointments", p1, nil, &own)
	if len(own) != 1 || own[0].PatientID != "p-list-1" {
		t.Fatalf("patient sees only own: %+v", own)
	}
	e.do("GET", "/api/appointments", e.operator(), nil, &all)
	if len(all) != 2 || all[0].PatientID != "p-list-2" {
		t.Fatalf("operator sees all, newest first: %+v", all)
	}
	if code := e.do("GET", "/api/appointments", "", nil, nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}
}
