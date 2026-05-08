package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// newMeTestAPI opens an in-memory SQLite database, runs migrations, and
// returns a store and auth instance for /api/me handler tests.
func newMeTestAPI(t *testing.T) (*store.DB, *auth.Auth) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	a := auth.New("0123456789abcdef0123456789abcdef")
	return db, a
}

// buildMeTestRouter wires a full router with no OpAMP pusher or WebSocket hub.
func buildMeTestRouter(db *store.DB, a *auth.Auth) http.Handler {
	return NewRouter(db, a, nil, nil, nil, "", nil, nil, 30*24*time.Hour, nil, nil)
}

func TestHandleMe_ReturnsUserGroupsAndPreferences(t *testing.T) {
	db, authSvc := newMeTestAPI(t)
	if err := db.CreateUser(models.User{ID: "u1", Email: "u1@x.com", PasswordHash: "x"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "editor"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := db.UpsertUserPreferences(models.UserPreferences{
		UserID: "u1", Theme: "dark", Language: "fr",
	}); err != nil {
		t.Fatalf("upsert prefs: %v", err)
	}

	tok, _ := authSvc.GenerateToken("u1", "u1@x.com", []string{"editor"})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()

	router := buildMeTestRouter(db, authSvc)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		ID          string                 `json:"id"`
		Email       string                 `json:"email"`
		Groups      []models.Group         `json:"groups"`
		Preferences models.UserPreferences `json:"preferences"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ID != "u1" || body.Email != "u1@x.com" {
		t.Errorf("identity mismatch: %+v", body)
	}
	if len(body.Groups) != 1 || body.Groups[0].Name != "editor" {
		t.Errorf("groups mismatch: %+v", body.Groups)
	}
	if body.Preferences.Theme != "dark" || body.Preferences.Language != "fr" {
		t.Errorf("prefs mismatch: %+v", body.Preferences)
	}
}

func TestHandleMe_ReturnsDefaultsWhenNoPreferencesRow(t *testing.T) {
	db, authSvc := newMeTestAPI(t)
	if err := db.CreateUser(models.User{ID: "u1", Email: "u1@x.com", PasswordHash: "x"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.AttachUserToGroupByName("u1", "viewer"); err != nil {
		t.Fatalf("attach: %v", err)
	}

	tok, _ := authSvc.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, authSvc).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body struct {
		Preferences models.UserPreferences `json:"preferences"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Preferences.Theme != "system" || body.Preferences.Language != "en" {
		t.Errorf("expected defaults, got %+v", body.Preferences)
	}
}

func TestHandleMe_Unauthorized(t *testing.T) {
	db, authSvc := newMeTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, authSvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}
