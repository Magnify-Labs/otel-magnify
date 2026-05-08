package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/validator"
)

// stubValidator is a ConfigValidator that returns a canned Result. Tests inject
// it so they don't depend on a real otelcol binary being present.
type stubValidator struct {
	result validator.Result
	calls  int
}

func (s *stubValidator) Validate(_ context.Context, _ []byte) validator.Result {
	s.calls++
	return s.result
}

// newValidationTestAPI mirrors newTestAPI but lets the caller inject a
// ConfigValidator and recordingAuditLogger, which are the two collaborators
// the /api/configs/validate handler depends on.
func newValidationTestAPI(t *testing.T, v ConfigValidator) (http.Handler, *recordingAuditLogger) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	a := auth.New("test-secret-key-at-least-32-bytes!")
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	audit := &recordingAuditLogger{}
	router := NewRouter(db, a, hub, nil, audit, v, "", nil, nil, 30*24*time.Hour, nil, nil)
	return router, audit
}

func postValidate(t *testing.T, router http.Handler, jsonBody string) *httptest.ResponseRecorder {
	t.Helper()
	a := auth.New("test-secret-key-at-least-32-bytes!")
	tok, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	req := httptest.NewRequest("POST", "/api/configs/validate", strings.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestValidateConfig_ValidYAML(t *testing.T) {
	stub := &stubValidator{result: validator.Result{Valid: true}}
	router, audit := newValidationTestAPI(t, stub)

	rec := postValidate(t, router, `{"content":"receivers: {otlp: {}}"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got validator.Result
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Valid {
		t.Errorf("Valid = false, want true")
	}
	if stub.calls != 1 {
		t.Errorf("validator called %d times, want 1", stub.calls)
	}

	events := audit.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event on success, got %d (%+v)", len(events), events)
	}
	if events[0].Action != "config.validate" || events[0].Resource != "config" {
		t.Errorf("audit event = %+v, want action=config.validate resource=config", events[0])
	}
	if events[0].Email != "admin@test.com" {
		t.Errorf("audit Email = %q, want admin@test.com", events[0].Email)
	}
}

func TestValidateConfig_InvalidYAML_NoAudit(t *testing.T) {
	stub := &stubValidator{result: validator.Result{
		Errors: []validator.Error{{Code: "otelcol_validate", Message: "boom", Path: "receivers.otlp"}},
	}}
	router, audit := newValidationTestAPI(t, stub)

	rec := postValidate(t, router, `{"content":"x"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got validator.Result
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Valid || len(got.Errors) != 1 || got.Errors[0].Path != "receivers.otlp" {
		t.Errorf("Result = %+v", got)
	}

	if events := audit.snapshot(); len(events) != 0 {
		t.Errorf("expected no audit events on failure, got %+v", events)
	}
}

func TestValidateConfig_BadJSON(t *testing.T) {
	stub := &stubValidator{result: validator.Result{Valid: true}}
	router, _ := newValidationTestAPI(t, stub)

	rec := postValidate(t, router, `not json`)
	if rec.Code != 400 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 0 {
		t.Errorf("validator called %d times for invalid JSON, want 0", stub.calls)
	}
}

func TestValidateConfig_EmptyContent(t *testing.T) {
	stub := &stubValidator{result: validator.Result{Valid: true}}
	router, _ := newValidationTestAPI(t, stub)

	rec := postValidate(t, router, `{"content":""}`)
	if rec.Code != 400 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if stub.calls != 0 {
		t.Errorf("validator called for empty content")
	}
}

func TestValidateConfig_UnconfiguredReturns503(t *testing.T) {
	router, _ := newValidationTestAPI(t, nil) // no validator wired

	rec := postValidate(t, router, `{"content":"any"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	var got validator.Result
	json.NewDecoder(rec.Body).Decode(&got)
	if got.Valid || len(got.Errors) != 1 || got.Errors[0].Code != "validator_unavailable" {
		t.Errorf("Result = %+v", got)
	}
}

func TestValidateConfig_RequiresAuth(t *testing.T) {
	stub := &stubValidator{result: validator.Result{Valid: true}}
	router, _ := newValidationTestAPI(t, stub)

	req := httptest.NewRequest("POST", "/api/configs/validate", strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401 without bearer token", rec.Code)
	}
}
