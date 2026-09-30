package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Hackathon: two hardcoded operators.
var operators = map[string]string{
	"operator1": "operator1",
	"operator2": "operator2",
}

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

type ctxKey struct{}

type User struct{ ID, Role string }

func (a *App) issue(sub, role string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{role, jwt.RegisteredClaims{
		Subject: sub, ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
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
	var in struct{ Username, Password string }
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		writeErr(w, 400, "bad json")
		return
	}
	if p, ok := operators[in.Username]; !ok || p != in.Password {
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
