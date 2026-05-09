package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
