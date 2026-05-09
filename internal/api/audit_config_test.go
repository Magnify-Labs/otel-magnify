package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestAudit_ConfigCreate_Emits(t *testing.T) {
	_, router, _, audit := newAuditTestAPI(t)

	body := `{"name":"collector-base","content":"receivers:\n  otlp:"}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var cfg models.Config
	_ = json.Unmarshal(rec.Body.Bytes(), &cfg)

	got := findEvent(audit.snapshot(), "config.create")
	if got == nil {
		t.Fatalf("missing config.create: %+v", audit.snapshot())
	}
	if got.Resource != "config" || got.ResourceID != cfg.ID {
		t.Errorf("Resource/ResourceID = (%q, %q), want (config, %q)", got.Resource, got.ResourceID, cfg.ID)
	}
	if got.Email != "admin@test.com" {
		t.Errorf("Email = %q", got.Email)
	}
}
