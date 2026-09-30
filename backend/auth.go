package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const defaultJWTSecret = "change-me-hackathon"

const (
	patientTTL  = 7 * 24 * time.Hour
	operatorTTL = 12 * time.Hour
)

// demoOperators are used only when OPERATORS is not set (frontend demo and tests rely on operator1/operator1).
var demoOperators = map[string]string{
	"operator1": "operator1",
	"operator2": "operator2",
}

// operators: login -> password. Set at startup from OPERATORS (see loadOperators).
var operators = demoOperators

// parseOperators parses "login1:password1,login2:password2". Malformed entries are skipped.
func parseOperators(s string) map[string]string {
	m := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		login, pass, ok := strings.Cut(strings.TrimSpace(part), ":")
		login = strings.TrimSpace(login)
		if !ok || login == "" || pass == "" {
			continue
		}
		m[login] = pass
	}
	return m
}

// loadOperators reads OPERATORS from env; falls back to the demo operators with a warning.
func loadOperators() map[string]string {
	if m := parseOperators(os.Getenv("OPERATORS")); len(m) > 0 {
		log.Printf("auth: %d operator(s) loaded from OPERATORS", len(m))
		return m
	}
	log.Println("WARNING: OPERATORS not set — using demo operators operator1/operator2")
	return demoOperators
}

// checkSecret validates the JWT secret. It returns an error (refuse to start) in production when the
// secret is empty/default or shorter than 32 chars; otherwise weak reports whether to log a warning.
func checkSecret(secret, appEnv string) (weak bool, err error) {
	weak = secret == "" || secret == defaultJWTSecret
	if appEnv == "production" && (weak || len(secret) < 32) {
		return weak, errors.New("JWT_SECRET must be set to a non-default value of at least 32 chars when APP_ENV=production")
	}
	return weak, nil
}

// secretFromEnv reads JWT_SECRET/APP_ENV, warns about a weak secret and exits in production.
func secretFromEnv() []byte {
	s := os.Getenv("JWT_SECRET")
	weak, err := checkSecret(s, os.Getenv("APP_ENV"))
	if err != nil {
		log.Fatalf("auth: %v", err)
	}
	if weak {
		log.Println("WARNING: JWT_SECRET is empty or default — set a strong secret outside of the demo")
		s = defaultJWTSecret
	}
	return []byte(s)
}

// rateLimiter: fixed-window attempt counter per key (in-memory).
type rateLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*rlEntry
}

type rlEntry struct {
	start time.Time
	n     int
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{max: max, window: window, hits: map[string]*rlEntry{}}
}

// allow records an attempt for key and reports whether it is within the limit.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.hits) > 10000 { // drop expired entries so the map cannot grow unbounded
		for k, e := range l.hits {
			if now.Sub(e.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	e := l.hits[key]
	if e == nil || now.Sub(e.start) >= l.window {
		e = &rlEntry{start: now}
		l.hits[key] = e
	}
	e.n++
	return e.n <= l.max
}

// loginLimiter: max 10 login attempts per IP per 5 minutes.
var loginLimiter = newRateLimiter(10, 5*time.Minute)

// clientIP: RemoteAddr host; first X-Forwarded-For value only when TRUST_PROXY=true.
func clientIP(r *http.Request) string {
	if os.Getenv("TRUST_PROXY") == "true" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

type ctxKey struct{}

type User struct{ ID, Role string }

func (a *App) issue(sub, role string) string {
	ttl := patientTTL
	if role == "operator" {
		ttl = operatorTTL
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{role, jwt.RegisteredClaims{
		Subject: sub, ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
	}})
	s, _ := t.SignedString(a.secret)
	return s
}

// POST /api/auth/patient — anonymous patient session.
func (a *App) patientToken(w http.ResponseWriter, r *http.Request) {
	id := "p-" + uuid.NewString()
	writeJSON(w, 200, map[string]string{"token": a.issue(id, "patient"), "user_id": id, "role": "patient"})
}

// POST /api/auth/login — operator login.
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !loginLimiter.allow(clientIP(r)) {
		writeErr(w, 429, "too many attempts")
		return
	}
	var in struct{ Username, Password string }
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeErr(w, 400, "bad json")
		return
	}
	p, ok := operators[in.Username]
	if !ok {
		p = "\x00no-such-operator" // still do a comparison so timing does not reveal unknown logins
	}
	if subtle.ConstantTimeCompare([]byte(p), []byte(in.Password)) != 1 || !ok {
		writeErr(w, 401, "invalid credentials")
		return
	}
	writeJSON(w, 200, map[string]string{"token": a.issue(in.Username, "operator"), "user_id": in.Username, "role": "operator"})
}

// auth requires a valid token; if roles are given, the token role must be one of them.
func (a *App) auth(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			var c claims
			_, err := jwt.ParseWithClaims(raw, &c, func(*jwt.Token) (any, error) { return a.secret, nil },
				jwt.WithValidMethods([]string{"HS256"}))
			if err != nil {
				writeErr(w, 401, "unauthorized")
				return
			}
			ok := len(roles) == 0
			for _, role := range roles {
				ok = ok || role == c.Role
			}
			if !ok {
				writeErr(w, 403, "forbidden")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, User{c.Subject, c.Role})))
		})
	}
}

func userFrom(r *http.Request) User { return r.Context().Value(ctxKey{}).(User) }
