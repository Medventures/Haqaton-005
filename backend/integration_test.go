package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"
)

const testAPIKey = "mis-test-key"

// doKey calls an integration endpoint with X-API-Key (empty key = no header).
func (e *testEnv) doKey(method, path, key string, body any, out any) int {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func testCatalogJSON(t *testing.T) map[string]any {
	b, err := os.ReadFile("../catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

func TestIntegrationAuth(t *testing.T) {
	e := setup(t)
	t.Setenv("INTEGRATION_API_KEY", "")
	if code := e.doKey("GET", "/api/integration/appointments", testAPIKey, nil, nil); code != 503 {
		t.Fatalf("disabled: %d, want 503", code)
	}
	t.Setenv("INTEGRATION_API_KEY", testAPIKey)
	if code := e.doKey("GET", "/api/integration/appointments", "", nil, nil); code != 401 {
		t.Fatalf("no key: %d, want 401", code)
	}
	if code := e.doKey("PUT", "/api/integration/catalog", "wrong", testCatalogJSON(t), nil); code != 401 {
		t.Fatalf("wrong key: %d, want 401", code)
	}
	if code := e.do("GET", "/api/integration/appointments", e.operator(), nil, nil); code != 401 {
		t.Fatalf("operator JWT is not an API key: %d, want 401", code)
	}
	if code := e.doKey("GET", "/api/integration/appointments", testAPIKey, nil, nil); code != 200 {
		t.Fatalf("valid key: %d", code)
	}
}

func TestIntegrationCatalogReplace(t *testing.T) {
	e := setup(t)
	t.Setenv("INTEGRATION_API_KEY", testAPIKey)
	c := testCatalogJSON(t)
	docs := c["doctors"].([]any)
	d1 := docs[0].(map[string]any)
	d1["name"] = "Новый Врач"
	d1["slots"] = []string{"2026-11-01T10:00", "2026-11-01T09:00", "2026-11-01T09:00"}
	var out map[string]int
	if code := e.doKey("PUT", "/api/integration/catalog", testAPIKey, c, &out); code != 200 || out["doctors"] != len(docs) {
		t.Fatalf("valid replace: %d %+v", code, out)
	}
	if got := e.doctorSlots("d1"); !slices.Equal(got, []string{"2026-11-01T09:00", "2026-11-01T10:00"}) {
		t.Fatalf("slots are replaced, deduplicated and sorted: %v", got)
	}
	if d, _ := e.a.cat.Doctor("d1"); d.Name != "Новый Врач" {
		t.Fatalf("doctor not replaced: %+v", d)
	}

	bad := []struct {
		name string
		mut  func(c map[string]any)
	}{
		{"duplicate service id", func(c map[string]any) {
			s := c["services"].([]any)
			c["services"] = append(s, s[0])
		}},
		{"unknown specialty", func(c map[string]any) {
			c["doctors"].([]any)[0].(map[string]any)["specialty_id"] = "astrologer"
		}},
		{"bad slot", func(c map[string]any) {
			c["doctors"].([]any)[0].(map[string]any)["slots"] = []string{"01.10.2026 09:00"}
		}},
		{"empty", func(c map[string]any) { c["specialties"] = []any{} }},
	}
	for _, b := range bad {
		c := testCatalogJSON(t)
		b.mut(c)
		var r map[string]string
		if code := e.doKey("PUT", "/api/integration/catalog", testAPIKey, c, &r); code != 400 || r["error"] == "" {
			t.Errorf("%s: %d %+v, want 400 with a reason", b.name, code, r)
		}
	}
	if d, _ := e.a.cat.Doctor("d1"); d.Name != "Новый Врач" {
		t.Fatal("invalid input must not change the catalog")
	}
}

func TestIntegrationCatalogReplaceHidesBooked(t *testing.T) {
	e := setup(t)
	t.Setenv("INTEGRATION_API_KEY", testAPIKey)
	if code := e.do("POST", "/api/appointments", e.patient(), booking("d1", "ther-consult", "2026-10-01T09:00"), nil); code != 201 {
		t.Fatalf("book: %d", code)
	}
	if code := e.doKey("PUT", "/api/integration/catalog", testAPIKey, testCatalogJSON(t), nil); code != 200 {
		t.Fatalf("replace: %d", code)
	}
	if slices.Contains(e.doctorSlots("d1"), "2026-10-01T09:00") {
		t.Fatal("booked slot must not reappear after a catalog replace")
	}
}

func TestIntegrationSlotsExcludeBooked(t *testing.T) {
	e := setup(t)
	t.Setenv("INTEGRATION_API_KEY", testAPIKey)
	if code := e.do("POST", "/api/appointments", e.patient(), booking("d2", "ther-consult", "2026-10-01T14:00"), nil); code != 201 {
		t.Fatalf("book: %d", code)
	}
	body := map[string][]string{"slots": {"2026-10-01T14:00", "2026-10-05T09:00", "2026-10-04T16:30"}}
	var out struct {
		Slots []string `json:"slots"`
	}
	if code := e.doKey("PUT", "/api/integration/doctors/d2/slots", testAPIKey, body, &out); code != 200 {
		t.Fatalf("slots: %d", code)
	}
	want := []string{"2026-10-04T16:30", "2026-10-05T09:00"}
	if !slices.Equal(out.Slots, want) || !slices.Equal(e.doctorSlots("d2"), want) {
		t.Fatalf("booked slot must be filtered out: resp %v, catalog %v", out.Slots, e.doctorSlots("d2"))
	}
	if code := e.doKey("PUT", "/api/integration/doctors/nope/slots", testAPIKey, body, nil); code != 404 {
		t.Fatalf("unknown doctor: %d", code)
	}
	if code := e.doKey("PUT", "/api/integration/doctors/d2/slots", testAPIKey, map[string][]string{"slots": {"soon"}}, nil); code != 400 {
		t.Fatalf("bad slot: %d", code)
	}
	if code := e.doKey("PUT", "/api/integration/doctors/d2/slots", testAPIKey, map[string]any{}, nil); code != 400 {
		t.Fatalf("missing slots: %d", code)
	}
}

func TestIntegrationAppointmentsSince(t *testing.T) {
	e := setup(t)
	t.Setenv("INTEGRATION_API_KEY", testAPIKey)
	var first bookResp
	if code := e.do("POST", "/api/appointments", e.patient(), booking("d1", "ther-consult", "2026-10-01T09:00"), &first); code != 201 {
		t.Fatalf("book1: %d", code)
	}
	var all []Appointment
	if code := e.doKey("GET", "/api/integration/appointments", testAPIKey, nil, &all); code != 200 || len(all) != 1 {
		t.Fatalf("all: %d %+v", code, all)
	}
	time.Sleep(20 * time.Millisecond)
	mark := time.Now().UTC().Format(time.RFC3339Nano)
	if code := e.do("POST", "/api/appointments", e.patient(), booking("d2", "ther-consult", "2026-10-03T10:00"), nil); code != 201 {
		t.Fatalf("book2: %d", code)
	}
	var since []Appointment
	e.doKey("GET", "/api/integration/appointments?since="+url.QueryEscape(mark), testAPIKey, nil, &since)
	if len(since) != 1 || since[0].DoctorID != "d2" {
		t.Fatalf("since: %+v", since)
	}
	// a cancellation is a change the MIS must see
	if code := e.do("POST", "/api/appointments/"+first.Appointment.ID+"/cancel", e.operator(), nil, nil); code != 200 {
		t.Fatalf("cancel: %d", code)
	}
	since = nil
	e.doKey("GET", "/api/integration/appointments?since="+url.QueryEscape(mark), testAPIKey, nil, &since)
	if len(since) != 2 || since[1].ID != first.Appointment.ID || since[1].Status != "cancelled" {
		t.Fatalf("since after cancel: %+v", since)
	}
	if code := e.doKey("GET", "/api/integration/appointments?since=yesterday", testAPIKey, nil, nil); code != 400 {
		t.Fatalf("bad since: %d", code)
	}
}
