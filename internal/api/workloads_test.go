package api

import (
	"context"
	"encoding/json"
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
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func authedPost(t *testing.T, url, body string) *http.Request {
	t.Helper()
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	req := httptest.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
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

func TestValidateWorkloadConfig_IncludesRuntimeCheckWhenEnabled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fake uses POSIX sh")
	}
	bin := writeAPIShim(t, `
case "$1" in
  --version) echo "otelcol version 0.150.1"; exit 0 ;;
  validate) exit 0 ;;
esac
exit 9
`)
	t.Setenv("OTELCOL_RUNTIME_VALIDATION_ENABLED", "true")
	t.Setenv("OTELCOL_BINARY_PATH", bin)

	db, router, _ := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Version: "0.150.1", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}})

	req := authedPost(t, "/api/workloads/w1/config/validate?target_collector_version=0.150.1", validWorkloadConfig)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var result struct {
		Valid  bool             `json:"valid"`
		Checks []apiResultCheck `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	check := findAPIResultCheck(t, result.Checks, "otelcol_runtime")
	if !result.Valid || check.Status != "passed" || check.Metadata["binary_version"] != "0.150.1" || check.Metadata["target_version"] != "0.150.1" {
		t.Fatalf("runtime check not integrated: result=%+v check=%+v", result, check)
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

type apiResultCheck struct {
	ID       string         `json:"id"`
	Status   string         `json:"status"`
	Metadata map[string]any `json:"metadata"`
}

func writeAPIShim(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "otelcol")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake otelcol: %v", err)
	}
	return path
}

func findAPIResultCheck(t *testing.T, checks []apiResultCheck, id string) apiResultCheck {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %q not found in %+v", id, checks)
	return apiResultCheck{}
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
