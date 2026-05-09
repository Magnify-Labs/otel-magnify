package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// findEvent returns the first event with the given action, or nil. Shared by
// every audit emission test in this package.
func findEvent(events []ext.AuditEvent, action string) *ext.AuditEvent {
	for i := range events {
		if events[i].Action == action {
			return &events[i]
		}
	}
	return nil
}

func TestAudit_WorkloadConfigPush_Emits(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-push", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}

	req := authedPost(t, "/api/workloads/w-push/config", validRollbackYAML)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	got := findEvent(audit.snapshot(), "config.push")
	if got == nil {
		t.Fatalf("missing config.push")
	}
	if got.Resource != "workload" || got.ResourceID != "w-push" {
		t.Errorf("Resource/ResourceID = (%q, %q)", got.Resource, got.ResourceID)
	}
	if got.Detail != resp["config_hash"] {
		t.Errorf("Detail = %q, want hash %q", got.Detail, resp["config_hash"])
	}
}

func TestAudit_WorkloadArchive_Emits(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-archive", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w-archive/archive", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	got := findEvent(audit.snapshot(), "workload.archive")
	if got == nil {
		t.Fatalf("missing workload.archive")
	}
	if got.Resource != "workload" || got.ResourceID != "w-archive" {
		t.Errorf("Resource/ResourceID = (%q, %q)", got.Resource, got.ResourceID)
	}
}

func TestAudit_WorkloadDelete_Emits(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-del", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}

	req := authedJSONRequest(t, http.MethodDelete, "/api/workloads/w-del", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}

	got := findEvent(audit.snapshot(), "workload.delete")
	if got == nil {
		t.Fatalf("missing workload.delete")
	}
	if got.Resource != "workload" || got.ResourceID != "w-del" {
		t.Errorf("Resource/ResourceID = (%q, %q)", got.Resource, got.ResourceID)
	}
}
