package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const planBaseConfig = `receivers:
  otlp: {}
processors:
  memory_limiter: {}
  batch: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [logging]
`

const planTargetConfig = `receivers:
  otlp: {}
processors:
  batch: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [logging]
`

const planHighRiskTargetConfig = `receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`

type configPlanResponse struct {
	SchemaVersion string `json:"schema_version"`
	WorkloadID    string `json:"workload_id"`
	ConfigHash    string `json:"config_hash"`
	CanPush       bool   `json:"can_push"`
	ApplyAllowed  bool   `json:"apply_allowed"`
	Summary       struct {
		TargetCount              int `json:"target_count"`
		CollectorTargetCount     int `json:"collector_target_count"`
		RemoteConfigCapableCount int `json:"remote_config_capable_count"`
		ReadOnlyCount            int `json:"read_only_count"`
		ValidationOKCount        int `json:"validation_ok_count"`
		ValidationFailedCount    int `json:"validation_failed_count"`
		ComponentsMissingCount   int `json:"components_missing_count"`
		HighRiskChangeCount      int `json:"high_risk_change_count"`
		ExcludedCount            int `json:"excluded_count"`
	} `json:"summary"`
	RiskScore struct {
		Severity       string   `json:"severity"`
		Reasons        []string `json:"reasons"`
		AppliesToCount int      `json:"applies_to_count"`
	} `json:"risk_score"`
	Targets []struct {
		WorkloadID              string   `json:"workload_id"`
		DisplayName             string   `json:"display_name"`
		Type                    string   `json:"type"`
		AcceptsRemoteConfig     bool     `json:"accepts_remote_config"`
		ReadOnly                bool     `json:"read_only"`
		ValidationStatus        string   `json:"validation_status"`
		ComponentsMissingCount  int      `json:"components_missing_count"`
		HighRiskChangeCount     int      `json:"high_risk_change_count"`
		Excluded                bool     `json:"excluded"`
		ExclusionReasons        []string `json:"exclusion_reasons"`
		HardFailures            []string `json:"hard_failures"`
		ActiveConfigHash        string   `json:"active_config_hash"`
		ActiveConfigUnavailable bool     `json:"active_config_unavailable"`
	} `json:"targets"`
	HardFailures []string `json:"hard_failures"`
	Export       struct {
		Supported        bool     `json:"supported"`
		Formats          []string `json:"formats"`
		JSONEndpoint     string   `json:"json_endpoint"`
		MarkdownEndpoint string   `json:"markdown_endpoint"`
		PersistedRollout string   `json:"persisted_rollout"`
	} `json:"export"`
}

func seedPlanWorkload(t *testing.T, db ext.Store, wl models.Workload) {
	t.Helper()
	if wl.ID == "" {
		wl.ID = "w-plan"
	}
	if wl.Type == "" {
		wl.Type = "collector"
	}
	if wl.Status == "" {
		wl.Status = "connected"
	}
	if wl.LastSeenAt.IsZero() {
		wl.LastSeenAt = time.Now().UTC()
	}
	if wl.Labels == nil {
		wl.Labels = models.Labels{}
	}
	if wl.AvailableComponents == nil {
		wl.AvailableComponents = &models.AvailableComponents{Hash: "components-v1", Components: map[string][]string{
			"receivers":  {"otlp"},
			"processors": {"batch", "memory_limiter"},
			"exporters":  {"logging"},
		}}
	}
	if err := db.UpsertWorkload(wl); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}
}

func seedAppliedPlanConfig(t *testing.T, db ext.Store, workloadID, content string, appliedAt time.Time) string {
	t.Helper()
	hash := configHash(content)
	if err := db.CreateConfig(models.Config{ID: hash, Name: "cfg-" + hash[:8], Content: content, CreatedAt: appliedAt}); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: workloadID, ConfigID: hash, AppliedAt: appliedAt, Status: models.PushStatusApplied}); err != nil {
		t.Fatalf("RecordWorkloadConfig: %v", err)
	}
	return hash
}

func postPlan(t *testing.T, router http.Handler, workloadID, body string) (configPlanResponse, int, string) {
	t.Helper()
	req := authedPost(t, "/api/workloads/"+workloadID+"/config/plan", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var plan configPlanResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &plan)
	return plan, rec.Code, rec.Body.String()
}

func TestPlanWorkloadConfig_HappyPathAllowsSingleCollectorTarget(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	seedPlanWorkload(t, db, models.Workload{ID: "w-plan", DisplayName: "collector-prod", Type: "collector", AcceptsRemoteConfig: true})
	currentHash := seedAppliedPlanConfig(t, db, "w-plan", planBaseConfig, now.Add(-time.Hour))
	seedPlanWorkload(t, db, models.Workload{ID: "w-plan", DisplayName: "collector-prod", Type: "collector", ActiveConfigHash: currentHash, AcceptsRemoteConfig: true})

	plan, status, body := postPlan(t, router, "w-plan", planTargetConfig)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", status, body)
	}
	if plan.SchemaVersion != "config_application_plan.v1" || plan.WorkloadID != "w-plan" || len(plan.ConfigHash) != 64 {
		t.Fatalf("identity fields not populated: %+v", plan)
	}
	if !plan.CanPush || !plan.ApplyAllowed || len(plan.HardFailures) != 0 {
		t.Fatalf("plan should allow push without hard failures: %+v", plan)
	}
	if plan.Summary.TargetCount != 1 || plan.Summary.CollectorTargetCount != 1 || plan.Summary.RemoteConfigCapableCount != 1 || plan.Summary.ValidationOKCount != 1 || plan.Summary.ExcludedCount != 0 {
		t.Fatalf("summary = %+v", plan.Summary)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].WorkloadID != "w-plan" || plan.Targets[0].ValidationStatus != "ok" || plan.Targets[0].Excluded {
		t.Fatalf("target = %+v", plan.Targets)
	}
	if !plan.Export.Supported || plan.Export.JSONEndpoint == "" || plan.Export.MarkdownEndpoint == "" || plan.Export.PersistedRollout != "not_persisted" {
		t.Fatalf("export metadata = %+v", plan.Export)
	}
}

func TestPlanWorkloadConfig_ExcludesReadOnlyTargetAndBlocksPush(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedPlanWorkload(t, db, models.Workload{ID: "w-readonly", DisplayName: "collector-ro", Type: "collector", AcceptsRemoteConfig: false})

	plan, status, body := postPlan(t, router, "w-readonly", planTargetConfig)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", status, body)
	}
	if plan.CanPush || plan.ApplyAllowed || plan.Summary.ReadOnlyCount != 1 || plan.Summary.ExcludedCount != 1 {
		t.Fatalf("read-only summary should block push: %+v", plan)
	}
	if len(plan.Targets) != 1 || !plan.Targets[0].ReadOnly || !plan.Targets[0].Excluded || !containsString(plan.Targets[0].ExclusionReasons, "read_only") {
		t.Fatalf("read-only target not excluded: %+v", plan.Targets)
	}
	if !containsString(plan.HardFailures, "all_targets_excluded") {
		t.Fatalf("hard_failures = %+v", plan.HardFailures)
	}
}

func TestPlanWorkloadConfig_ExcludesOfflineTargetAndBlocksPush(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedPlanWorkload(t, db, models.Workload{ID: "w-offline", DisplayName: "collector-offline", Type: "collector", Status: "disconnected", AcceptsRemoteConfig: true})

	plan, status, body := postPlan(t, router, "w-offline", planTargetConfig)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", status, body)
	}
	if plan.CanPush || plan.ApplyAllowed || plan.Summary.ExcludedCount != 1 {
		t.Fatalf("offline summary should block push: %+v", plan)
	}
	if len(plan.Targets) != 1 || !plan.Targets[0].Excluded || !containsString(plan.Targets[0].ExclusionReasons, "workload_offline") || !containsString(plan.Targets[0].HardFailures, "workload_offline") {
		t.Fatalf("offline target not excluded: %+v", plan.Targets)
	}
	if !containsString(plan.HardFailures, "all_targets_excluded") {
		t.Fatalf("hard_failures = %+v", plan.HardFailures)
	}
}

func TestPlanWorkloadConfig_ValidationFailureBlocksPush(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedPlanWorkload(t, db, models.Workload{ID: "w-invalid", DisplayName: "collector-invalid", Type: "collector", AcceptsRemoteConfig: true})

	plan, status, body := postPlan(t, router, "w-invalid", "receivers: {}")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", status, body)
	}
	if plan.CanPush || plan.ApplyAllowed || plan.Summary.ValidationFailedCount != 1 || plan.Summary.ExcludedCount != 1 {
		t.Fatalf("invalid plan should block push: %+v", plan)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].ValidationStatus != "failed" || !containsString(plan.Targets[0].ExclusionReasons, "validation_failed") {
		t.Fatalf("validation target = %+v", plan.Targets)
	}
}

func TestPlanWorkloadConfig_CountsMissingComponents(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedPlanWorkload(t, db, models.Workload{
		ID: "w-missing", DisplayName: "collector-missing", Type: "collector", AcceptsRemoteConfig: true,
		AvailableComponents: &models.AvailableComponents{Hash: "components-v2", Components: map[string][]string{
			"receivers": {"otlp"}, "processors": {"batch"}, "exporters": {"otlp"},
		}},
	})

	plan, status, body := postPlan(t, router, "w-missing", planTargetConfig)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", status, body)
	}
	if plan.Summary.ComponentsMissingCount != 1 || plan.Summary.ValidationFailedCount != 1 || plan.Summary.ExcludedCount != 1 {
		t.Fatalf("summary = %+v", plan.Summary)
	}
	if len(plan.Targets) != 1 || plan.Targets[0].ComponentsMissingCount != 1 || !containsString(plan.Targets[0].ExclusionReasons, "validation_failed") {
		t.Fatalf("target = %+v", plan.Targets)
	}
}

func TestPlanWorkloadConfig_CountsHighRiskChangesAgainstActiveConfig(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	seedPlanWorkload(t, db, models.Workload{ID: "w-risk", DisplayName: "collector-risk", Type: "collector", AcceptsRemoteConfig: true})
	currentHash := seedAppliedPlanConfig(t, db, "w-risk", planBaseConfig, now.Add(-time.Hour))
	seedPlanWorkload(t, db, models.Workload{ID: "w-risk", DisplayName: "collector-risk", Type: "collector", ActiveConfigHash: currentHash, AcceptsRemoteConfig: true})

	plan, status, body := postPlan(t, router, "w-risk", planHighRiskTargetConfig)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", status, body)
	}
	if !plan.CanPush || !plan.ApplyAllowed {
		t.Fatalf("high risk should be visible but not a hard failure by itself: %+v", plan)
	}
	if plan.Summary.HighRiskChangeCount == 0 || len(plan.Targets) != 1 || plan.Targets[0].HighRiskChangeCount == 0 || plan.Targets[0].ActiveConfigHash != currentHash {
		t.Fatalf("risk counts not populated: %+v", plan)
	}
	if plan.RiskScore.Severity != "high" || plan.RiskScore.AppliesToCount != 1 {
		t.Fatalf("risk score = %+v, want high applying to one collector", plan.RiskScore)
	}
	if !containsString(plan.RiskScore.Reasons, "Memory limiter removed from pipeline") {
		t.Fatalf("risk score missing memory limiter reason: %+v", plan.RiskScore)
	}
}

func TestPlanWorkloadConfig_EmptyBodyHardFailureBlocksPush(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedPlanWorkload(t, db, models.Workload{ID: "w-empty", DisplayName: "collector-empty", Type: "collector", AcceptsRemoteConfig: true})

	plan, status, body := postPlan(t, router, "w-empty", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, body=%s", status, body)
	}
	if plan.CanPush || plan.ApplyAllowed || !containsString(plan.HardFailures, "empty_config") {
		t.Fatalf("empty plan should be a hard failure: %+v", plan)
	}
}

func TestExportWorkloadConfigPlanMarkdown(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedPlanWorkload(t, db, models.Workload{ID: "w-export", DisplayName: "collector-export", Type: "collector", AcceptsRemoteConfig: true})

	req := authedPost(t, "/api/workloads/w-export/config/plan/export?format=markdown", planTargetConfig)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/markdown") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"# Config Safety Plan", "collector-export", "Can push: true", "Persisted rollout: not implemented"} {
		if !strings.Contains(body, want) {
			t.Fatalf("markdown export missing %q: %s", want, body)
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
