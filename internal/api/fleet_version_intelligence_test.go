package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestFleetVersionIntelligence_MissingRecommendedVersionReturnsUnknownBaseline(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, http.MethodGet, "/api/workloads/version-intelligence")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["schema_version"] != "fleet-version-intelligence.v1" || body["recommended_version"] != "" {
		t.Fatalf("unexpected response header: %+v", body)
	}
}

func TestFleetVersionIntelligence_InvalidRecommendedVersionDoesNotProduceFalseFindings(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, http.MethodGet, "/api/workloads/version-intelligence?recommended_version=nightly")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["recommended_version"] != "nightly" {
		t.Fatalf("recommended_version = %v", body["recommended_version"])
	}
	if below := body["collectors_below_recommended"].([]any); len(below) != 0 {
		t.Fatalf("collectors_below_recommended = %+v, want empty for invalid recommendation baseline", below)
	}
}

func TestFleetVersionIntelligence_UsesLatestAppliedConfigSnapshot(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-snapshot", DisplayName: "collector snapshot", Type: "collector", Version: "0.99.0", Status: "connected", LastSeenAt: now,
		Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{},
		AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}},
	}); err != nil {
		t.Fatal(err)
	}

	oldUnsupported := `receivers:
  otlp: {}
exporters:
  datadog: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [datadog]
`
	newSupported := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	oldHash := configHash(oldUnsupported)
	newHash := configHash(newSupported)
	if err := db.CreateConfig(models.Config{ID: oldHash, Name: "old", Content: oldUnsupported, CreatedAt: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w-snapshot", ConfigID: oldHash, AppliedAt: now.Add(-2 * time.Hour), Status: "applied"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{ID: newHash, Name: "new", Content: newSupported, CreatedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w-snapshot", ConfigID: newHash, AppliedAt: now.Add(-time.Hour), Status: "applied"}); err != nil {
		t.Fatal(err)
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/version-intelligence?recommended_version=0.100.0")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	unsupported := body["unsupported_config_components"].([]any)
	if len(unsupported) != 0 {
		t.Fatalf("unsupported_config_components = %+v, want latest applied supported config only", unsupported)
	}
}
