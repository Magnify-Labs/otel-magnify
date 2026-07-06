package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/opamp"
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

func TestAudit_WorkloadConfigPush_LegacyEndpointDoesNotEmitDirectPushAudit(t *testing.T) {
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

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := findEvent(audit.snapshot(), "config.push"); got != nil {
		t.Fatalf("legacy direct push emitted config.push audit event: %+v", got)
	}
}

func TestAudit_WorkloadConfigPushTarget_EmitsTargetWithoutConfigBody(t *testing.T) {
	db, router, fake, audit := newAuditTestAPI(t)
	seedCanaryWorkload(t, db, "w-push", models.Labels{})
	fake.instances["w-push"] = []opamp.Instance{
		{InstanceUID: "uid-canary", Healthy: true, LastMessageAt: time.Now().UTC()},
		{InstanceUID: "uid-other", Healthy: true, LastMessageAt: time.Now().UTC()},
	}

	rec := postCanary(t, router, "w-push", `{"config":"`+jsonEsc(validCanaryConfig)+`","selection":{"strategy":"one","instance_uid":"uid-canary"}}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := findEvent(audit.snapshot(), "config.canary.start")
	if got == nil {
		t.Fatalf("missing config.canary.start")
	}
	if !strings.Contains(got.Detail, "uid-canary") {
		t.Fatalf("audit detail %q does not include target uid", got.Detail)
	}
	for _, forbidden := range []string{"receivers:", "exporters:", "service:"} {
		if strings.Contains(got.Detail, forbidden) {
			t.Fatalf("audit detail leaked config body fragment %q: %s", forbidden, got.Detail)
		}
	}
}

func TestAudit_WorkloadArchive_Emits(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-archive", Type: "collector", Status: "disconnected",
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

func TestAudit_WorkloadPush_LegacyEndpointDoesNotDependOnAudit(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-push-fail", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	audit.failWith(errors.New("audit DB down"))

	req := authedPost(t, "/api/workloads/w-push-fail/config", validRollbackYAML)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["code"] != "config_approval_required" {
		t.Errorf("code = %q, want config_approval_required", resp["code"])
	}
}

func TestAudit_WorkloadArchive_503AppliedWhenAuditFails(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-arch-fail", Type: "collector", Status: "disconnected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}
	audit.failWith(errors.New("audit DB down"))

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w-arch-fail/archive", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAudit_WorkloadDelete_503AppliedWhenAuditFails(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-del-fail", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}
	audit.failWith(errors.New("audit DB down"))

	req := authedJSONRequest(t, http.MethodDelete, "/api/workloads/w-del-fail", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
