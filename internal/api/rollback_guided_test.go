package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestRollbackGuided_RejectsCurrentCapabilityMismatchWithoutSideEffects(t *testing.T) {
	db, router, fake, _ := newAuditTestAPI(t)
	rollbackConfig := `receivers:
  otlp: {}
exporters:
  datadog: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [datadog]
`
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(),
		Labels: models.Labels{}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true,
		AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{ID: "hash-bad", Name: "bad", Content: rollbackConfig, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "hash-bad", Status: "applied"}); err != nil {
		t.Fatal(err)
	}

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-bad/rollback", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "component_not_installed") {
		t.Fatalf("body does not expose validation failure code: %s", rec.Body.String())
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("expected no push when rollback validation fails, got %+v", fake.pushed)
	}
	history, err := db.GetWorkloadConfigHistory("w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Status != "applied" {
		t.Fatalf("rollback validation failure should not append history rows, got %+v", history)
	}
}

func TestRollbackGuided_OpAMPFailureMarksRollbackRowFailed(t *testing.T) {
	db, router, fake, _ := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", validRollbackYAML)
	fake.err = errors.New("agent disconnected")

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/rollback", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.pushed) != 1 {
		t.Fatalf("push attempts = %d, want 1", len(fake.pushed))
	}
	history, err := db.GetWorkloadConfigHistory("w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history len = %d, want original applied + failed rollback", len(history))
	}
	if history[0].Status != "failed" || history[0].PushedBy != "admin@test.com" {
		t.Fatalf("latest rollback row = %+v, want failed with operator", history[0])
	}
	if strings.Contains(history[0].ErrorMessage, "agent disconnected") || !strings.Contains(history[0].ErrorMessage, "redacted") {
		t.Fatalf("latest rollback error = %q, want redacted OpAMP error", history[0].ErrorMessage)
	}
}
