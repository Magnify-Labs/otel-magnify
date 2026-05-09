package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// seedAuditUser creates a user with a bcrypt-hashed password and attaches them
// to the named system group so login/password tests can authenticate.
func seedAuditUser(t *testing.T, db ext.Store, id, email, password, group string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 4) // cheap cost for tests
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	if err := db.CreateUser(models.User{ID: id, Email: email, PasswordHash: string(hash)}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.AttachUserToGroupByName(id, group); err != nil {
		t.Fatalf("attach group: %v", err)
	}
}

func TestAudit_LoginSuccess_EmitsAuthLoginSuccess(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedAuditUser(t, db, "u-login-ok", "alice@example.com", "correct-horse-battery", "viewer")

	body := `{"email":"alice@example.com","password":"correct-horse-battery"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	got := findEvent(audit.snapshot(), "auth.login.success")
	if got == nil {
		t.Fatalf("missing auth.login.success: %+v", audit.snapshot())
	}
	if got.UserID != "u-login-ok" || got.Email != "alice@example.com" {
		t.Errorf("identity = (%q, %q)", got.UserID, got.Email)
	}
	if got.Resource != "user" || got.ResourceID != "u-login-ok" {
		t.Errorf("Resource/ResourceID = (%q, %q)", got.Resource, got.ResourceID)
	}
}

// Login failure on unknown email — no UserInfo in ctx, so UserID/Email stay
// empty and the attempted email lands in Detail.
func TestAudit_LoginFailure_UnknownEmail_EmitsAttemptedEmailInDetail(t *testing.T) {
	_, router, _, audit := newAuditTestAPI(t)

	body := `{"email":"ghost@example.com","password":"whatever"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	got := findEvent(audit.snapshot(), "auth.login.failure")
	if got == nil {
		t.Fatalf("missing auth.login.failure")
	}
	if got.UserID != "" || got.Email != "" {
		t.Errorf("identity = (%q, %q), want empty", got.UserID, got.Email)
	}
	if got.Detail != "ghost@example.com" {
		t.Errorf("Detail = %q, want ghost@example.com", got.Detail)
	}
}

func TestAudit_LoginFailure_BadPassword_EmitsAttemptedEmailInDetail(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedAuditUser(t, db, "u-login-bad", "bob@example.com", "right-password", "viewer")

	body := `{"email":"bob@example.com","password":"wrong-password"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}

	got := findEvent(audit.snapshot(), "auth.login.failure")
	if got == nil {
		t.Fatalf("missing auth.login.failure")
	}
	if got.Detail != "bob@example.com" {
		t.Errorf("Detail = %q", got.Detail)
	}
}

// Validation rejections (missing fields, bad JSON) are not auth failures and
// should not emit an audit event — they're 400s, not 401s.
func TestAudit_LoginInvalidJSON_DoesNotEmit(t *testing.T) {
	_, router, _, audit := newAuditTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if findEvent(audit.snapshot(), "auth.login.failure") != nil {
		t.Errorf("unexpected audit event for malformed JSON")
	}
}

func TestAudit_PasswordChange_Emits(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedAuditUser(t, db, "u-pw", "carol@example.com", "old-password-xx", "viewer")

	a := auth.New("test-secret-key-at-least-32-bytes!")
	tok, _ := a.GenerateToken("u-pw", "carol@example.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{
		"current_password": "old-password-xx",
		"new_password":     "new-password-xxxx",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	got := findEvent(audit.snapshot(), "auth.password.change")
	if got == nil {
		t.Fatalf("missing auth.password.change")
	}
	if got.UserID != "u-pw" || got.Email != "carol@example.com" {
		t.Errorf("identity = (%q, %q)", got.UserID, got.Email)
	}
	if got.Resource != "user" || got.ResourceID != "u-pw" {
		t.Errorf("Resource/ResourceID = (%q, %q)", got.Resource, got.ResourceID)
	}
	// Detail must stay empty — passwords (old or new) never enter the audit log.
	if got.Detail != "" {
		t.Errorf("Detail = %q, expected empty (must not leak password material)", got.Detail)
	}
}

func TestAudit_LoginSuccess_503WhenAuditFails(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedAuditUser(t, db, "u-loud", "loud@example.com", "correct-horse-battery", "viewer")
	audit.failWith(errors.New("audit DB down"))

	body := `{"email":"loud@example.com","password":"correct-horse-battery"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error"] != "audit unavailable" {
		t.Errorf("error = %q", resp["error"])
	}
	if resp["side_effect_status"] != "none" {
		t.Errorf("side_effect_status = %q, want none (token minted but never returned)", resp["side_effect_status"])
	}
}

func TestAudit_LoginFailure_503WhenAuditFails(t *testing.T) {
	_, router, _, audit := newAuditTestAPI(t)
	audit.failWith(errors.New("audit DB down"))

	body := `{"email":"ghost@example.com","password":"whatever"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["side_effect_status"] != "none" {
		t.Errorf("side_effect_status = %q, want none", resp["side_effect_status"])
	}
}

func TestAudit_PasswordChange_503AppliedWhenAuditFails(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedAuditUser(t, db, "u-pw-fail", "dave@example.com", "old-password-xx", "viewer")

	a := auth.New("test-secret-key-at-least-32-bytes!")
	tok, _ := a.GenerateToken("u-pw-fail", "dave@example.com", []string{"viewer"})
	body, _ := json.Marshal(map[string]string{
		"current_password": "old-password-xx",
		"new_password":     "new-password-xxxx",
	})

	audit.failWith(errors.New("audit DB down"))

	req := httptest.NewRequest(http.MethodPut, "/api/me/password", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["side_effect_status"] != "applied" {
		t.Errorf("side_effect_status = %q, want applied (password row already changed)", resp["side_effect_status"])
	}
	// Password DID change — audit failed AFTER the UPDATE.
	u, _ := db.GetUserByEmail("dave@example.com")
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("new-password-xxxx")) != nil {
		t.Error("password should have changed despite the 503")
	}
}
