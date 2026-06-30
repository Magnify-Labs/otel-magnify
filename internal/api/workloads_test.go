package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// fakeOpAMPPusher implements OpAMPPusher for the REST handler tests. Captures
// what got pushed so tests can assert on the payload, and lets callers seed a
// map of workload -> live instances to exercise the /instances endpoint.
type pushCall struct {
	WorkloadID string
	Target     string
	Body       []byte
}

type fakeOpAMPPusher struct {
	pushed    []pushCall
	err       error
	instances map[string][]opamp.Instance
}

func (f *fakeOpAMPPusher) PushConfig(_ context.Context, workloadID string, body []byte, target string) error {
	f.pushed = append(f.pushed, pushCall{WorkloadID: workloadID, Target: target, Body: body})
	return f.err
}

func (f *fakeOpAMPPusher) Instances(workloadID string) []opamp.Instance {
	return f.instances[workloadID]
}

// newTestAPI is shared by workloads_test.go and configs_test.go. Returns the
// store (seed test data), the wired HTTP router, and the fake OpAMP pusher so
// tests can inspect what got pushed / stub instances.
func newTestAPI(t *testing.T) (ext.Store, http.Handler, *fakeOpAMPPusher) {
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

	fake := &fakeOpAMPPusher{instances: make(map[string][]opamp.Instance)}
	router := NewRouter(db, a, hub, fake, nil, "", nil, nil, 30*24*time.Hour, nil, nil)
	return db, router, fake
}

func authedRequest(t *testing.T, method, url string) *http.Request {
	t.Helper()
	return authedRequestForGroups(t, method, url, "", []string{"administrator"})
}

func authedRequestForGroups(t *testing.T, method, url, body string, groups []string) *http.Request {
	t.Helper()
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", groups)
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func authedPost(t *testing.T, url, body string) *http.Request {
	t.Helper()
	req := authedRequestForGroups(t, "POST", url, body, []string{"administrator"})
	req.Header.Set("Content-Type", "text/yaml")
	return req
}

// --- List / Get ---

func TestListWorkloads_Empty(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/workloads")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestListWorkloads_WithData(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w1", DisplayName: "svc", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	req := authedRequest(t, "GET", "/api/workloads")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var items []models.Workload
	_ = json.NewDecoder(rec.Body).Decode(&items)
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
}

func TestGetWorkload_OK(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w1", DisplayName: "svc", Type: "collector",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})

	req := authedRequest(t, "GET", "/api/workloads/w1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var w models.Workload
	_ = json.NewDecoder(rec.Body).Decode(&w)
	if w.ID != "w1" {
		t.Fatalf("ID = %q", w.ID)
	}
}

func TestGetWorkload_NotFound(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/workloads/does-not-exist")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// --- Instances ---

func TestListWorkloadInstances_FromRegistry(t *testing.T) {
	_, router, fake := newTestAPI(t)
	fake.instances["w1"] = []opamp.Instance{
		{InstanceUID: "uid-a", PodName: "pod-a", Version: "0.98.0", Healthy: true},
		{InstanceUID: "uid-b", PodName: "pod-b", Version: "0.98.0", Healthy: false},
	}

	req := authedRequest(t, "GET", "/api/workloads/w1/instances")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var out []opamp.Instance
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2, body=%s", len(out), rec.Body.String())
	}
}

func TestListWorkloadInstances_EmptyArrayNotNull(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/workloads/w-unknown/instances")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("body = %q, want \"[]\"", rec.Body.String())
	}
}

// --- Events ---

func TestListWorkloadEvents_NewestFirst(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	base := time.Now().UTC()
	for i, evType := range []string{"connected", "version_changed", "disconnected"} {
		_, _ = db.InsertWorkloadEvent(models.WorkloadEvent{
			WorkloadID:  "w1",
			InstanceUID: "uid-1",
			EventType:   evType,
			OccurredAt:  base.Add(time.Duration(i) * time.Second),
		})
	}

	req := authedRequest(t, "GET", "/api/workloads/w1/events")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var out []models.WorkloadEvent
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3", len(out))
	}
	if out[0].EventType != "disconnected" {
		t.Errorf("first event = %q, want \"disconnected\" (newest first)", out[0].EventType)
	}
}

func TestListWorkloadEvents_SinceFilter(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	old := time.Now().UTC().Add(-2 * time.Hour)
	fresh := time.Now().UTC()
	_, _ = db.InsertWorkloadEvent(models.WorkloadEvent{WorkloadID: "w1", InstanceUID: "uid-1", EventType: "connected", OccurredAt: old})
	_, _ = db.InsertWorkloadEvent(models.WorkloadEvent{WorkloadID: "w1", InstanceUID: "uid-1", EventType: "disconnected", OccurredAt: fresh})

	cutoff := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	req := authedRequest(t, "GET", "/api/workloads/w1/events?since="+cutoff)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var out []models.WorkloadEvent
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 1 || out[0].EventType != "disconnected" {
		t.Fatalf("since filter failed: %+v", out)
	}
}

func TestWorkloadEventsStats(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	now := time.Now().UTC()
	for _, ev := range []models.WorkloadEvent{
		{WorkloadID: "w1", InstanceUID: "uid-1", EventType: "connected", OccurredAt: now.Add(-30 * time.Minute)},
		{WorkloadID: "w1", InstanceUID: "uid-1", EventType: "version_changed", OccurredAt: now.Add(-20 * time.Minute)},
		{WorkloadID: "w1", InstanceUID: "uid-1", EventType: "disconnected", OccurredAt: now.Add(-10 * time.Minute)},
		{WorkloadID: "w1", InstanceUID: "uid-2", EventType: "disconnected", OccurredAt: now.Add(-5 * time.Minute)},
	} {
		_, _ = db.InsertWorkloadEvent(ev)
	}

	req := authedRequest(t, "GET", "/api/workloads/w1/events/stats?window=1h")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var stats map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&stats)
	if stats["connected"].(float64) != 1 {
		t.Errorf("connected = %v, want 1", stats["connected"])
	}
	if stats["disconnected"].(float64) != 2 {
		t.Errorf("disconnected = %v, want 2", stats["disconnected"])
	}
	if stats["version_changed"].(float64) != 1 {
		t.Errorf("version_changed = %v, want 1", stats["version_changed"])
	}
	// churn_rate_per_hour = disconnected / window_hours = 2 / 1 = 2
	if stats["churn_rate_per_hour"].(float64) != 2 {
		t.Errorf("churn_rate = %v, want 2", stats["churn_rate_per_hour"])
	}
}

// --- Push / Validate ---

func TestPushWorkloadConfig_HappyPath(t *testing.T) {
	db, router, fake := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	})

	validYAML := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	req := authedPost(t, "/api/workloads/w1/config", validYAML)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 202 {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if hash, _ := body["config_hash"].(string); len(hash) != 64 {
		t.Fatalf("bad hash: %q", body["config_hash"])
	}
	hist, _ := db.GetWorkloadConfigHistory("w1")
	if len(hist) != 1 || hist[0].Status != models.PushStatusSent || hist[0].PushedBy != "admin@test.com" {
		t.Fatalf("history not recorded: %+v", hist)
	}
	if len(fake.pushed) != 1 || fake.pushed[0].WorkloadID != "w1" || fake.pushed[0].Target != "" {
		t.Fatalf("push not recorded correctly: %+v", fake.pushed)
	}
}

func TestPushWorkloadConfig_RejectsWhenRemoteConfigNotAccepted(t *testing.T) {
	db, router, fake := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w-ro", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: false,
	})

	validYAML := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	req := authedPost(t, "/api/workloads/w-ro/config", validYAML)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "remote_config_unsupported" {
		t.Fatalf("code = %q", body["code"])
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("expected 0 pushes, got %d", len(fake.pushed))
	}
}

func TestPushWorkloadConfig_RejectsRuntimeValidationFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake uses POSIX sh")
	}
	bin := writeAPIShim(t, `
case "$1" in
  --version) echo "otelcol version 0.150.1"; exit 0 ;;
  validate) echo "bad runtime config" >&2; exit 42 ;;
esac
exit 9
`)
	t.Setenv("OTELCOL_RUNTIME_VALIDATION_ENABLED", "true")
	t.Setenv("OTELCOL_BINARY_PATH", bin)

	db, router, fake := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Version: "0.150.1", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true})

	req := authedPost(t, "/api/workloads/w1/config", validWorkloadConfig)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("runtime-invalid config should not be pushed, got %d pushes", len(fake.pushed))
	}
	var body struct {
		ValidationErrors []struct {
			Code    string `json:"code"`
			CheckID string `json:"check_id"`
		} `json:"validation_errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.ValidationErrors) != 1 || body.ValidationErrors[0].Code != "otelcol_validation_failed" || body.ValidationErrors[0].CheckID != "otelcol_runtime" {
		t.Fatalf("runtime validation error not returned stably: %+v", body.ValidationErrors)
	}
}

func TestPushWorkloadConfig_RejectsEmptyBody(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	})

	req := authedPost(t, "/api/workloads/w1/config", "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestValidateWorkloadConfig_ReturnsErrorsForBadYAML(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	req := authedPost(t, "/api/workloads/w1/config/validate", "receivers: {}")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var result struct {
		Valid  bool `json:"valid"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result.Valid || len(result.Errors) == 0 {
		t.Fatalf("expected validation errors, got %+v", result)
	}
}

const validWorkloadConfig = `
receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`

func writeAPIShim(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "otelcol")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake otelcol: %v", err)
	}
	return path
}

// --- Config history ---

func TestGetWorkloadConfigHistory(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})
	_ = db.CreateConfig(models.Config{ID: "c1", Name: "n", Content: "my-yaml", CreatedAt: time.Now().UTC()})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "c1", Status: "failed", ErrorMessage: "oops", PushedBy: "u@x"})

	req := authedRequest(t, "GET", "/api/workloads/w1/configs")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var hist []models.WorkloadConfig
	_ = json.Unmarshal(rec.Body.Bytes(), &hist)
	if len(hist) != 1 || hist[0].ErrorMessage != "oops" || hist[0].Content != "my-yaml" || hist[0].PushedBy != "u@x" {
		t.Fatalf("history shape: %+v", hist)
	}
}

// --- Guided rollback ---

func configHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func seedRollbackConfig(t *testing.T, db ext.Store, workloadID, content, status, pushedBy string, appliedAt time.Time, label *string) string {
	t.Helper()
	_ = db.UpsertWorkload(models.Workload{ID: workloadID, Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true})
	hash := configHash(content)
	if err := db.CreateConfig(models.Config{ID: hash, Name: "cfg-" + hash[:8], Content: content, CreatedAt: appliedAt, CreatedBy: pushedBy}); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: workloadID, ConfigID: hash, AppliedAt: appliedAt, Status: status, PushedBy: pushedBy, Label: label}); err != nil {
		t.Fatalf("RecordWorkloadConfig: %v", err)
	}
	if label != nil {
		if err := db.SetWorkloadConfigLabel(workloadID, hash, *label); err != nil {
			t.Fatalf("SetWorkloadConfigLabel: %v", err)
		}
	}
	return hash
}

func TestRollbackPrepareByHashReturnsSnapshotValidationAndDiff(t *testing.T) {
	db, router, _ := newTestAPI(t)
	current := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	target := `receivers:
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
	now := time.Now().UTC()
	currentHash := seedRollbackConfig(t, db, "w1", current, "applied", "ops@example.com", now.Add(-2*time.Hour), nil)
	label := "stable-before-change"
	targetHash := seedRollbackConfig(t, db, "w1", target, "applied", "valentin@example.com", now.Add(-time.Hour), &label)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w1", DisplayName: "collector-prod", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true,
		ActiveConfigHash: currentHash,
		AvailableComponents: &models.AvailableComponents{Hash: "components-v1", Components: map[string][]string{
			"receivers": {"otlp"}, "processors": {"batch"}, "exporters": {"logging"},
		}},
	})

	req := authedRequest(t, "GET", "/api/workloads/w1/rollback/prepare?target_hash="+targetHash)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["schema_version"] != "guided-rollback-prepare.v1" {
		t.Fatalf("schema_version = %v", body["schema_version"])
	}
	if targetCfg := body["target_config"].(map[string]any); targetCfg["hash"] != targetHash || targetCfg["content_available"] != true {
		t.Fatalf("target_config = %+v", targetCfg)
	}
	metadata := body["target_config"].(map[string]any)["metadata"].(map[string]any)
	if metadata["label"] != label || metadata["pushed_by"] != "valentin@example.com" {
		t.Fatalf("metadata = %+v", metadata)
	}
	validation := body["validation"].(map[string]any)
	if validation["status"] != "valid" || validation["can_confirm"] != true {
		t.Fatalf("validation = %+v", validation)
	}
	diff := body["diff"].(map[string]any)
	if diff["status"] != "available" || diff["direction"] != "current_to_target" || diff["base_hash"] != currentHash || diff["target_hash"] != targetHash {
		t.Fatalf("diff = %+v", diff)
	}
	action := body["action"].(map[string]any)
	if action["can_submit"] != true || action["submit_url"] != "/api/workloads/w1/configs/"+targetHash+"/rollback" {
		t.Fatalf("action = %+v", action)
	}
}

func TestRollbackPrepareUnavailableComponentBlocksConfirm(t *testing.T) {
	db, router, _ := newTestAPI(t)
	target := `receivers:
  otlp: {}
exporters:
  datadog: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [datadog]
`
	targetHash := seedRollbackConfig(t, db, "w1", target, "applied", "", time.Now().UTC(), nil)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true,
		AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}},
	})

	req := authedRequest(t, "GET", "/api/workloads/w1/rollback/prepare?target_hash="+targetHash)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	validation := body["validation"].(map[string]any)
	if validation["status"] != "invalid" || validation["can_confirm"] != false {
		t.Fatalf("validation = %+v", validation)
	}
	unavailable := validation["unavailable_components"].([]any)
	if len(unavailable) != 1 || unavailable[0].(map[string]any)["component_type"] != "datadog" {
		t.Fatalf("unavailable_components = %+v", unavailable)
	}
}

func TestRollbackActionResponseAndStatusTransitions(t *testing.T) {
	db, router, fake := newTestAPI(t)
	target := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	targetHash := seedRollbackConfig(t, db, "w1", target, "applied", "", time.Now().UTC().Add(-time.Hour), nil)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true})

	req := authedPost(t, "/api/workloads/w1/configs/"+targetHash+"/rollback", "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var accepted map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &accepted)
	if accepted["schema_version"] != "guided-rollback-action.v1" || accepted["status"] != "accepted" || accepted["config_hash"] != targetHash {
		t.Fatalf("accepted = %+v", accepted)
	}
	requestID, ok := accepted["request_id"].(string)
	if !ok || requestID == "" || accepted["status_url"] == "" {
		t.Fatalf("missing correlation fields: %+v", accepted)
	}
	if len(fake.pushed) != 1 || string(fake.pushed[0].Body) != target {
		t.Fatalf("push = %+v", fake.pushed)
	}

	req = authedRequest(t, "GET", "/api/workloads/w1/rollback/status?request_id="+requestID)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status report code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var report map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &report)
	if report["apply_status"] != "accepted" || report["terminal"] != false || report["last_known_status"] != models.PushStatusSent {
		t.Fatalf("initial report = %+v", report)
	}

	if err := db.UpdateWorkloadConfigStatus("w1", targetHash, "applied", ""); err != nil {
		t.Fatal(err)
	}
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true, RemoteConfigStatus: &models.RemoteConfigStatus{Status: "applied", ConfigHash: targetHash, UpdatedAt: time.Now().UTC()}})
	req = authedRequest(t, "GET", "/api/workloads/w1/rollback/status?request_id="+requestID)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &report)
	if report["apply_status"] != "applied" || report["terminal_status"] != "applied" || report["terminal"] != true {
		t.Fatalf("applied report = %+v", report)
	}
}

func TestRollbackMissingMetadataDoesNotBlockPrepare(t *testing.T) {
	db, router, _ := newTestAPI(t)
	target := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	targetHash := seedRollbackConfig(t, db, "w1", target, "applied", "", time.Now().UTC(), nil)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true})

	req := authedRequest(t, "GET", "/api/workloads/w1/rollback/prepare?target_hash="+targetHash)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["action"].(map[string]any)["can_submit"] != true {
		t.Fatalf("action = %+v", body["action"])
	}
	metadata := body["target_config"].(map[string]any)["metadata"].(map[string]any)
	if _, ok := metadata["label"]; ok {
		t.Fatalf("unexpected label metadata: %+v", metadata)
	}
}

func TestRollbackApplyFailureRecordsFailedAndReturnsRetryableError(t *testing.T) {
	db, router, fake := newTestAPI(t)
	target := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	targetHash := seedRollbackConfig(t, db, "w1", target, "applied", "", time.Now().UTC(), nil)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true})
	fake.err = errors.New("collector disconnected")

	req := authedPost(t, "/api/workloads/w1/configs/"+targetHash+"/rollback", "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "push_failed" || body["retryable"] != true {
		t.Fatalf("error body = %+v", body)
	}
	history, _ := db.GetWorkloadConfigHistory("w1")
	if history[0].Status != "failed" || history[0].ErrorMessage != "collector disconnected" {
		t.Fatalf("history = %+v", history)
	}
}

func TestRollbackPrepareRejectsNonAppliedTarget(t *testing.T) {
	db, router, _ := newTestAPI(t)
	targetHash := seedRollbackConfig(t, db, "w1", validRollbackYAMLForAPI(), "failed", "", time.Now().UTC(), nil)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true})

	req := authedRequest(t, "GET", "/api/workloads/w1/rollback/prepare?target_hash="+targetHash)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "target_not_applied" {
		t.Fatalf("body = %+v", body)
	}
}

func TestRollbackActionRejectsNonAppliedTarget(t *testing.T) {
	db, router, fake := newTestAPI(t)
	targetHash := seedRollbackConfig(t, db, "w1", validRollbackYAMLForAPI(), "failed", "", time.Now().UTC(), nil)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true})

	req := authedPost(t, "/api/workloads/w1/configs/"+targetHash+"/rollback", "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "target_not_applied" {
		t.Fatalf("body = %+v", body)
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("expected no opamp push, got %+v", fake.pushed)
	}
}

func TestDefaultRollbackRejectsArchivedNonCollectorAndConcurrentChanges(t *testing.T) {
	tests := []struct {
		name     string
		workload models.Workload
		setup    func(ext.Store)
		wantCode string
	}{
		{
			name:     "archived",
			workload: models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true, ArchivedAt: ptrTime(time.Now().UTC())},
			wantCode: "workload_archived",
		},
		{
			name:     "non collector",
			workload: models.Workload{ID: "w1", Type: "sdk", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true},
			wantCode: "workload_not_collector",
		},
		{
			name:     "concurrent",
			workload: models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true},
			setup: func(db ext.Store) {
				_ = db.CreateConfig(models.Config{ID: "pending", Name: "pending", Content: validRollbackYAMLForAPI(), CreatedAt: time.Now().UTC()})
				_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "pending", Status: "pending", AppliedAt: time.Now().UTC()})
			},
			wantCode: "concurrent_config_change",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, router, fake := newTestAPI(t)
			seedRollbackConfig(t, db, "w1", validRollbackYAMLForAPI(), "applied", "", time.Now().UTC().Add(-time.Hour), nil)
			_ = db.UpsertWorkload(tc.workload)
			if tc.setup != nil {
				tc.setup(db)
			}

			req := authedPost(t, "/api/workloads/w1/rollback", "")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409, body=%s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body["code"] != tc.wantCode {
				t.Fatalf("body = %+v, want code %s", body, tc.wantCode)
			}
			if len(fake.pushed) != 0 {
				t.Fatalf("expected no opamp push, got %+v", fake.pushed)
			}
		})
	}
}

func TestDefaultRollbackPermissionsViewerForbiddenEditorAllowed(t *testing.T) {
	db, router, fake := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true, ActiveConfigHash: "current"})
	_ = db.CreateConfig(models.Config{ID: "current", Name: "current", Content: strings.ReplaceAll(validRollbackYAMLForAPI(), "logging", "debug"), CreatedAt: time.Now().UTC().Add(-2 * time.Hour)})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "current", Status: "applied", AppliedAt: time.Now().UTC().Add(-2 * time.Hour)})
	seedRollbackConfig(t, db, "w1", validRollbackYAMLForAPI(), "applied", "", time.Now().UTC().Add(-time.Hour), nil)

	viewerReq := authedRequestForGroups(t, http.MethodPost, "/api/workloads/w1/rollback", "", []string{"viewer"})
	viewerRec := httptest.NewRecorder()
	router.ServeHTTP(viewerRec, viewerReq)
	if viewerRec.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, want 403", viewerRec.Code)
	}

	editorReq := authedRequestForGroups(t, http.MethodPost, "/api/workloads/w1/rollback", "", []string{"editor"})
	editorRec := httptest.NewRecorder()
	router.ServeHTTP(editorRec, editorReq)
	if editorRec.Code != http.StatusAccepted {
		t.Fatalf("editor status = %d, body=%s", editorRec.Code, editorRec.Body.String())
	}
	if len(fake.pushed) != 1 {
		t.Fatalf("editor should push once, got %+v", fake.pushed)
	}
}

func TestRollbackStatusRedactsRawConfigAndRemoteErrors(t *testing.T) {
	db, router, _ := newTestAPI(t)
	secretYAML := strings.Replace(validRollbackYAMLForAPI(), "logging", "secret_exporter", 1)
	targetHash := seedRollbackConfig(t, db, "w1", secretYAML, "applied", "operator@example.com", time.Now().UTC().Add(-time.Hour), ptrString("safe label"))
	startedAt := time.Now().UTC()
	requestID := newRollbackRequestID("w1", targetHash, startedAt)
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: targetHash, AppliedAt: startedAt, Status: models.PushStatusSent, PushedBy: "operator@example.com", ErrorMessage: "backend secret detail", Label: ptrString("safe label")}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetWorkloadConfigLabel("w1", targetHash, "safe label"); err != nil {
		t.Fatal(err)
	}
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true, RemoteConfigStatus: &models.RemoteConfigStatus{Status: "failed", ConfigHash: targetHash, ErrorMessage: "remote secret detail", UpdatedAt: time.Now().UTC()}})

	req := authedRequest(t, "GET", "/api/workloads/w1/rollback/status?request_id="+requestID)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"history_row", "remote_config_status", "content", "secret_exporter", "backend secret detail", "remote secret detail"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("status response leaked %q in %s", forbidden, body)
		}
	}
	var report map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &report)
	if report["target_hash"] != targetHash || report["target_label"] != "safe label" || report["target_status"] == "" || report["target_pushed_by"] != "operator@example.com" {
		t.Fatalf("redacted report missing safe target fields: %+v", report)
	}
}

func TestRollbackAuditDetailIncludesContextWithoutRawConfig(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	currentHash := seedRollbackConfig(t, db, "w1", strings.Replace(validRollbackYAMLForAPI(), "logging", "debug", 1), "applied", "", time.Now().UTC().Add(-2*time.Hour), nil)
	targetHash := seedRollbackConfig(t, db, "w1", validRollbackYAMLForAPI(), "applied", "", time.Now().UTC().Add(-time.Hour), nil)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true, ActiveConfigHash: currentHash})

	req := authedPost(t, "/api/workloads/w1/configs/"+targetHash+"/rollback", "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	events := audit.snapshot()
	if len(events) != 1 || events[0].Action != "config.rollback" {
		t.Fatalf("events = %+v", events)
	}
	var detail map[string]any
	if err := json.Unmarshal([]byte(events[0].Detail), &detail); err != nil {
		t.Fatalf("audit detail is not JSON: %q", events[0].Detail)
	}
	if detail["target_kind"] != "hash" || detail["request_id"] == "" || detail["current_hash"] != currentHash || detail["target_hash"] != targetHash {
		t.Fatalf("detail = %+v", detail)
	}
	if strings.Contains(events[0].Detail, "receivers:") || strings.Contains(events[0].Detail, "exporters:") {
		t.Fatalf("audit detail leaked raw config: %s", events[0].Detail)
	}
}

func validRollbackYAMLForAPI() string {
	return `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
}

func ptrString(v string) *string { return &v }

func ptrTime(v time.Time) *time.Time { return &v }

// --- Delete ---

func TestDeleteWorkload(t *testing.T) {
	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	req := httptest.NewRequest("DELETE", "/api/workloads/w1", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if _, err := db.GetWorkload("w1"); err == nil {
		t.Fatal("expected workload to be deleted")
	}
}

// --- Legacy redirect ---

func TestLegacyAgentsRedirect(t *testing.T) {
	_, router, _ := newTestAPI(t)
	// Note: httptest.ResponseRecorder does NOT follow redirects — we want to
	// observe the 307 + Location header directly.
	req := authedRequest(t, "GET", "/api/agents")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/workloads" {
		t.Fatalf("Location = %q, want /api/workloads", loc)
	}
}

func TestLegacyAgentsRedirect_KeepsSubpath(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/agents/abc/configs?foo=bar")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/workloads/abc/configs?foo=bar" {
		t.Fatalf("Location = %q", loc)
	}
}

// Defense against an open redirect: a crafted request whose URL.Path starts
// with "//" produces a RequestURI() like "//evil.com/api/agents". After the
// strings.Replace the target is "//evil.com/api/workloads", which browsers
// resolve as an absolute URL to evil.com — gosec G710 flags this. The handler
// must reject it with 400.
func TestLegacyAgentsRedirect_RejectsProtocolRelativePath(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.URL.Path = "//evil.com/api/agents"
	rec := httptest.NewRecorder()
	redirectAgentsToWorkloads(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no Location header on rejection, got %q", loc)
	}
}

// Chrome and Edge normalise a leading "/\" to "//", so /\evil.com is
// equivalent to //evil.com in the browser — the handler must reject it
// just like the protocol-relative case above. CodeQL's go/bad-redirect-check
// flags any redirect guard that does not cover this.
func TestLegacyAgentsRedirect_RejectsBackslashBypass(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/agents", nil)
	req.URL.Path = `/\evil.com/api/agents`
	rec := httptest.NewRecorder()
	redirectAgentsToWorkloads(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; Location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("expected no Location header on rejection, got %q", loc)
	}
}
