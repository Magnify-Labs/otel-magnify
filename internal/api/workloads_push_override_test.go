package api

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// invalidYAML deliberately references a processor that is not declared in
// the top-level processors section. The light validator (validator.Validate)
// flags this as an undefined_component, which the push handler enforces as
// a safety net unless the caller passes ?override=true.
const invalidPushYAML = `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
`

func TestPushWorkloadConfig_OverrideBypassesSafetyNet(t *testing.T) {
	db, router, fake, audit := newVersioningTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	tok, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	req := httptest.NewRequest("POST", "/api/workloads/w1/config?override=true", strings.NewReader(invalidPushYAML))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "text/yaml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(fake.pushed) != 1 {
		t.Fatalf("expected push to reach OpAMP, got %d", len(fake.pushed))
	}

	events := audit.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected audit event, got %d", len(events))
	}
	if events[0].Action != "config.push" {
		t.Errorf("Action = %q, want config.push", events[0].Action)
	}
	if events[0].Detail != "override=true" {
		t.Errorf("Detail = %q, want override=true", events[0].Detail)
	}
	if events[0].Email != "admin@test.com" {
		t.Errorf("Email = %q", events[0].Email)
	}
}

func TestPushWorkloadConfig_NoOverrideStillRejectsInvalid(t *testing.T) {
	db, router, fake := newTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}

	req := authedPost(t, "/api/workloads/w1/config", invalidPushYAML)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("expected no push without override, got %d", len(fake.pushed))
	}
}

func TestPushWorkloadConfig_ValidYAML_NoAuditWithoutOverride(t *testing.T) {
	// Audit emission for push is reserved for override-flagged pushes; a
	// regular successful push should not log to audit (push history table
	// is the canonical record for ordinary pushes).
	db, router, _, audit := newVersioningTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}

	a := auth.New("test-secret-key-at-least-32-bytes!")
	tok, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	req := httptest.NewRequest("POST", "/api/workloads/w1/config", strings.NewReader(validRollbackYAML))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "text/yaml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if events := audit.snapshot(); len(events) != 0 {
		t.Errorf("expected no audit events for non-override push, got %+v", events)
	}
}
