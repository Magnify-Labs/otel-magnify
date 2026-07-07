package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

func TestRespondAuditUnavailable_AppliedSideEffect(t *testing.T) {
	rec := httptest.NewRecorder()
	respondAuditUnavailable(rec, sideEffectApplied)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "audit unavailable" {
		t.Errorf("error = %q", body["error"])
	}
	if body["side_effect_status"] != "applied" {
		t.Errorf("side_effect_status = %q", body["side_effect_status"])
	}
}

func TestRespondAuditUnavailable_NoSideEffect(t *testing.T) {
	rec := httptest.NewRecorder()
	respondAuditUnavailable(rec, sideEffectNone)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["side_effect_status"] != "none" {
		t.Errorf("side_effect_status = %q, want none", body["side_effect_status"])
	}
}

func TestEmitAudit_Responds503AndReportsNotEmittedOnFailure(t *testing.T) {
	audit := &recordingAuditLogger{}
	audit.failWith(errors.New("audit DB down"))
	api := &API{audit: audit}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)

	emitted := api.emitAudit(rec, req, sideEffectApplied, "config.push", "workload", "w1", "hash-a")

	if emitted {
		t.Fatal("emitAudit returned true after audit logger failure")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["side_effect_status"] != string(sideEffectApplied) {
		t.Fatalf("side_effect_status = %q, want %q", body["side_effect_status"], sideEffectApplied)
	}
	if len(audit.snapshot()) != 0 {
		t.Fatalf("failed audit event should not be recorded: %+v", audit.snapshot())
	}
}

func TestEmitAudit_ReturnsTrueOnSuccess(t *testing.T) {
	audit := &recordingAuditLogger{}
	api := &API{audit: audit}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
	req = req.WithContext(ext.ContextWithUserInfo(req.Context(), &ext.UserInfo{UserID: "u1", Email: "user@example.com"}))

	emitted := api.emitAudit(rec, req, sideEffectNone, "auth.login.success", "user", "u1", "")

	if !emitted {
		t.Fatal("emitAudit returned false after successful audit log")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected response status = %d", rec.Code)
	}
	got := findEvent(audit.snapshot(), "auth.login.success")
	if got == nil {
		t.Fatalf("missing audit event")
	}
	if got.UserID != "u1" || got.Email != "user@example.com" {
		t.Fatalf("identity = (%q, %q)", got.UserID, got.Email)
	}
}
