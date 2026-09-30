package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newAuthTestApp(t *testing.T) *App {
	t.Helper()
	old, oldLim := operators, loginLimiter
	operators = map[string]string{"operator1": "operator1", "alice": "s3cret"}
	loginLimiter = newRateLimiter(10, 5*time.Minute)
	t.Cleanup(func() { operators, loginLimiter = old, oldLim })
	return &App{secret: []byte("test-secret-test-secret-test-secret")}
}

func doLogin(a *App, body, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.RemoteAddr = ip + ":12345"
	rec := httptest.NewRecorder()
	a.login(rec, req)
	return rec
}

func TestParseOperatorsEnv(t *testing.T) {
	m := parseOperators(" op1:pw1 , op2:p:w2,bad,:x,y:")
	if len(m) != 2 || m["op1"] != "pw1" || m["op2"] != "p:w2" {
		t.Fatalf("unexpected: %v", m)
	}
	t.Setenv("OPERATORS", "")
	if got := loadOperators(); got["operator1"] != "operator1" || got["operator2"] != "operator2" {
		t.Fatalf("demo fallback: %v", got)
	}
	t.Setenv("OPERATORS", "boss:pa55")
	if got := loadOperators(); len(got) != 1 || got["boss"] != "pa55" {
		t.Fatalf("env operators: %v", got)
	}
}

func TestLoginWrongPasswordAndUnknownUserSame401(t *testing.T) {
	a := newAuthTestApp(t)
	wrong := doLogin(a, `{"username":"alice","password":"nope"}`, "10.0.0.1")
	unknown := doLogin(a, `{"username":"mallory","password":"nope"}`, "10.0.0.1")
	if wrong.Code != 401 || unknown.Code != 401 {
		t.Fatalf("codes: %d %d", wrong.Code, unknown.Code)
	}
	if wrong.Body.String() != unknown.Body.String() {
		t.Fatalf("bodies differ: %q vs %q", wrong.Body.String(), unknown.Body.String())
	}
}

func TestLoginOperatorTokenRoleAndTTL(t *testing.T) {
	a := newAuthTestApp(t)
	rec := doLogin(a, `{"username":"alice","password":"s3cret"}`, "10.0.0.2")
	if rec.Code != 200 {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}
	var out struct{ Token, Role string }
	json.NewDecoder(rec.Body).Decode(&out)
	var c claims
	if _, err := jwt.ParseWithClaims(out.Token, &c, func(*jwt.Token) (any, error) { return a.secret, nil }); err != nil {
		t.Fatal(err)
	}
	if c.Role != "operator" || out.Role != "operator" || c.Subject != "alice" {
		t.Fatalf("claims: %+v", c)
	}
	if d := time.Until(c.ExpiresAt.Time); d < 11*time.Hour+50*time.Minute || d > 12*time.Hour+time.Minute {
		t.Fatalf("operator ttl %v, want ~12h", d)
	}
	var pc claims
	jwt.ParseWithClaims(a.issue("p-1", "patient"), &pc, func(*jwt.Token) (any, error) { return a.secret, nil })
	if d := time.Until(pc.ExpiresAt.Time); d < 7*24*time.Hour-time.Minute {
		t.Fatalf("patient ttl %v, want 7d", d)
	}
}

func TestPatientTokenCannotPassOperatorAuth(t *testing.T) {
	a := newAuthTestApp(t)
	h := a.auth("operator")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for tok, want := range map[string]int{
		a.issue("p-1", "patient"):    403,
		a.issue("alice", "operator"): 200,
		"garbage":                    401,
	} {
		req := httptest.NewRequest("GET", "/api/operator/queue", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("token %.10s...: code %d want %d", tok, rec.Code, want)
		}
	}
}

func TestLoginRateLimit(t *testing.T) {
	a := newAuthTestApp(t)
	for i := 1; i <= 10; i++ {
		if rec := doLogin(a, `{"username":"alice","password":"nope"}`, "10.0.0.3"); rec.Code != 401 {
			t.Fatalf("attempt %d: code %d", i, rec.Code)
		}
	}
	rec := doLogin(a, `{"username":"alice","password":"s3cret"}`, "10.0.0.3")
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), "too many attempts") {
		t.Fatalf("11th attempt: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doLogin(a, `{"username":"alice","password":"s3cret"}`, "10.0.0.4"); rec.Code != 200 {
		t.Fatalf("other IP should not be limited: %d", rec.Code)
	}
}

func TestRateLimitForwardedForOnlyWithTrustProxy(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "192.168.1.5:999"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	t.Setenv("TRUST_PROXY", "")
	if ip := clientIP(req); ip != "192.168.1.5" {
		t.Fatalf("got %s", ip)
	}
	t.Setenv("TRUST_PROXY", "true")
	if ip := clientIP(req); ip != "1.2.3.4" {
		t.Fatalf("got %s", ip)
	}
}

func TestCheckSecretProduction(t *testing.T) {
	strong := strings.Repeat("x", 32)
	cases := []struct {
		secret, env string
		weak, fail  bool
	}{
		{"", "", true, false},
		{defaultJWTSecret, "", true, false},
		{"short", "", false, false},
		{"", "production", true, true},
		{defaultJWTSecret, "production", true, true},
		{"short-but-custom", "production", false, true},
		{strong, "production", false, false},
	}
	for _, c := range cases {
		weak, err := checkSecret(c.secret, c.env)
		if weak != c.weak || (err != nil) != c.fail {
			t.Fatalf("checkSecret(%q,%q) = %v,%v", c.secret, c.env, weak, err)
		}
	}
}
