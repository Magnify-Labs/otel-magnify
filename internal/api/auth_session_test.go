package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func newAuthSessionTestRouter(t *testing.T) (*store.DB, *auth.Auth, http.Handler) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	authSvc := auth.New("0123456789abcdef0123456789abcdef")
	return db, authSvc, NewRouter(db, authSvc, nil, nil, nil, "", nil, nil, 30*24*time.Hour, nil, nil, nil)
}

func TestLoginSetsHttpOnlySessionCookie(t *testing.T) {
	db, _, router := newAuthSessionTestRouter(t)
	passwordHash, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.CreateUser(models.User{ID: "u1", Email: "u1@example.com", PasswordHash: passwordHash}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("attach group: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"u1@example.com","password":"correct-horse-battery"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	cookie := findCookie(rec.Result().Cookies(), "om_session")
	if cookie == nil {
		t.Fatalf("login response did not set om_session cookie; cookies=%v", rec.Result().Cookies())
	}
	if !cookie.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	if cookie.Path != "/" {
		t.Fatalf("session cookie path=%q, want /", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie SameSite=%v, want Lax", cookie.SameSite)
	}
	if cookie.MaxAge <= 0 {
		t.Fatalf("session cookie MaxAge=%d, want positive", cookie.MaxAge)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	if body["token"] == "" {
		t.Fatal("login body should keep returning token for backward compatibility")
	}
}

func TestProtectedAPIAcceptsHttpOnlySessionCookie(t *testing.T) {
	db, authSvc, router := newAuthSessionTestRouter(t)
	if err := db.CreateUser(models.User{ID: "u1", Email: "u1@example.com", PasswordHash: "x"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("attach group: %v", err)
	}
	token, err := authSvc.GenerateToken("u1", "u1@example.com", []string{"viewer"})
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(&http.Cookie{Name: "om_session", Value: token})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLogoutClearsSessionCookie(t *testing.T) {
	_, _, router := newAuthSessionTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	cookie := findCookie(rec.Result().Cookies(), "om_session")
	if cookie == nil {
		t.Fatalf("logout response did not clear om_session cookie; cookies=%v", rec.Result().Cookies())
	}
	if cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("logout cookie value=%q MaxAge=%d, want cleared with negative MaxAge", cookie.Value, cookie.MaxAge)
	}
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
