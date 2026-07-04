package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func (f *fakeOpAMPPusher) InstanceWorkload(instanceUID string) (string, bool) {
	for workloadID, instances := range f.instances {
		for _, instance := range instances {
			if instance.InstanceUID == instanceUID {
				return workloadID, true
			}
		}
	}
	return "", false
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
	router := NewRouter(db, a, hub, fake, nil, "", nil, nil, 30*24*time.Hour, testEnabledFeatures(), nil, nil)
	return db, router, fake
}

func testEnabledFeatures() map[string]bool {
	return map[string]bool{
		FeatureConfigSafetyApprovals:           true,
		FeatureConfigSafetyGuidedRollback:      true,
		FeatureConfigSafetyCanaryRollout:       true,
		FeatureConfigSafetyScopedPush:          true,
		FeatureConfigSafetyDriftDashboard:      true,
		FeatureConfigSafetyVersionIntelligence: true,
		FeatureConfigSafetyGitOpsExport:        true,
		FeatureConfigSafetyPolicyPreview:       true,
		FeatureReportsEvidencePack:             true,
		FeatureAuditViewer:                     true,
	}
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

const sensitiveOpAMPErrorFixture = "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"

func assertResponseDoesNotLeakSensitiveOpAMPError(t *testing.T, body string) {
	t.Helper()
	for _, leaked := range []string{"SECRET_TOKEN", "tenant-a.internal", "authorization=Bearer"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("response leaked %q: %s", leaked, body)
		}
	}
	if !strings.Contains(body, models.SanitizeRemoteConfigErrorMessage(sensitiveOpAMPErrorFixture)) {
		t.Fatalf("response did not contain sanitized error message: %s", body)
	}
}

func assertLatestWorkloadConfigErrorIsSanitized(t *testing.T, db ext.Store, workloadID string) {
	t.Helper()
	history, err := db.GetWorkloadConfigHistory(workloadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 {
		t.Fatalf("expected workload config history for %s", workloadID)
	}
	if history[0].Status != "failed" {
		t.Fatalf("latest workload config status = %q, want failed", history[0].Status)
	}
	if history[0].ErrorMessage != models.SanitizeRemoteConfigErrorMessage(sensitiveOpAMPErrorFixture) {
		t.Fatalf("latest workload config error = %q", history[0].ErrorMessage)
	}
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

func TestGetWorkload_RedactsLegacyRemoteConfigStatusError(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedLegacyRemoteConfigStatus(t, db, "w-legacy")

	req := authedRequest(t, "GET", "/api/workloads/w-legacy")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertNoSensitiveRemoteConfigStatusLeak(t, rec.Body.String())
	var w models.Workload
	if err := json.NewDecoder(rec.Body).Decode(&w); err != nil {
		t.Fatalf("decode workload: %v", err)
	}
	assertRemoteConfigStatusSanitized(t, w.RemoteConfigStatus)
}

func TestListWorkloads_RedactsLegacyRemoteConfigStatusError(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedLegacyRemoteConfigStatus(t, db, "w-legacy")

	req := authedRequest(t, "GET", "/api/workloads")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	assertNoSensitiveRemoteConfigStatusLeak(t, rec.Body.String())
	var items []models.Workload
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode workloads: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	assertRemoteConfigStatusSanitized(t, items[0].RemoteConfigStatus)
}

func seedLegacyRemoteConfigStatus(t *testing.T, db ext.Store, workloadID string) {
	t.Helper()
	if err := db.UpsertWorkload(models.Workload{ID: workloadID, DisplayName: "legacy", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}}); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}
	sqlDB, ok := db.(*store.DB)
	if !ok {
		t.Fatalf("test store is %T, want *store.DB", db)
	}
	raw := `{"status":"failed","config_hash":"hash-a","error_message":"collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces","updated_at":"1970-01-01T00:00:00Z"}`
	if _, err := sqlDB.Exec(`UPDATE workloads SET remote_config_status = ? WHERE id = ?`, raw, workloadID); err != nil {
		t.Fatalf("seed legacy remote_config_status: %v", err)
	}
}

func assertNoSensitiveRemoteConfigStatusLeak(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"SECRET_TOKEN", "abc123", "authorization=Bearer", "super-secret", "tenant-a.internal", "4318", "/v1/traces"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked forbidden marker %q", forbidden)
		}
	}
	if !strings.Contains(body, "redacted") {
		t.Fatalf("response should explain redacted remote status details")
	}
}

func assertRemoteConfigStatusSanitized(t *testing.T, status *models.RemoteConfigStatus) {
	t.Helper()
	if status == nil {
		t.Fatal("remote_config_status is nil")
	}
	if status.Status != "failed" {
		t.Fatalf("remote_config_status.status = %q, want failed", status.Status)
	}
	if status.ConfigHash != "hash-a" {
		t.Fatalf("remote_config_status.config_hash = %q, want hash-a", status.ConfigHash)
	}
	const want = "Remote config error details redacted"
	if status.ErrorMessage != want {
		t.Fatalf("remote_config_status.error_message did not match sanitized summary")
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

func TestListWorkloadInstances_IncludesPerInstanceConfigStatus(t *testing.T) {
	_, router, fake := newTestAPI(t)
	updatedAt := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	fake.instances["w1"] = []opamp.Instance{
		{
			InstanceUID: "uid-a", PodName: "pod-a", Version: "0.98.0", Healthy: true,
			RemoteConfigStatus: &models.RemoteConfigStatus{
				Status: "failed", ConfigHash: "hash-a", ErrorMessage: "bad exporter", UpdatedAt: updatedAt,
			},
		},
	}

	req := authedRequest(t, "GET", "/api/workloads/w1/instances")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []opamp.Instance
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 || out[0].RemoteConfigStatus == nil {
		t.Fatalf("remote_config_status missing: %+v", out)
	}
	got := out[0].RemoteConfigStatus
	if got.Status != "failed" || got.ConfigHash != "hash-a" || got.ErrorMessage != models.SanitizeRemoteConfigErrorMessage("bad exporter") || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("remote_config_status = %+v", got)
	}
}

func TestGetWorkloadTopology_SummarizesMixedInstanceState(t *testing.T) {
	_, router, fake := newTestAPI(t)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	fake.instances["w1"] = []opamp.Instance{
		{
			InstanceUID: "uid-a", PodName: "pod-a", Version: "0.98.0", Healthy: true, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-a", LastMessageAt: now,
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: "applied", ConfigHash: "hash-a", UpdatedAt: now},
		},
		{
			InstanceUID: "uid-b", PodName: "pod-b", Version: "0.99.0", Healthy: false, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-b", LastMessageAt: now,
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: "failed", ConfigHash: "hash-b", ErrorMessage: "bad exporter", UpdatedAt: now},
		},
		{
			InstanceUID: "uid-c", PodName: "pod-c", Version: "0.99.0", Healthy: true, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-b", LastMessageAt: now,
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: "applying", ConfigHash: "hash-b", UpdatedAt: now},
		},
	}

	req := authedRequest(t, "GET", "/api/workloads/w1/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		WorkloadID string           `json:"workload_id"`
		Instances  []opamp.Instance `json:"instances"`
		Summary    struct {
			ConnectedCount          int             `json:"connected_count"`
			HealthyCount            int             `json:"healthy_count"`
			UnhealthyCount          int             `json:"unhealthy_count"`
			DriftedCount            int             `json:"drifted_count"`
			VersionDiversity        []string        `json:"version_diversity"`
			ConfigHashDiversity     []string        `json:"config_hash_diversity"`
			RemoteConfigStatusCount map[string]int  `json:"remote_config_status_counts"`
			Heterogeneity           map[string]bool `json:"heterogeneity"`
			HeterogeneityReasons    []string        `json:"heterogeneity_reasons"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.WorkloadID != "w1" || len(body.Instances) != 3 {
		t.Fatalf("unexpected topology header: %+v", body)
	}
	if body.Summary.ConnectedCount != 3 || body.Summary.HealthyCount != 2 || body.Summary.UnhealthyCount != 1 || body.Summary.DriftedCount != 0 {
		t.Fatalf("bad counts: %+v", body.Summary)
	}
	if got, want := body.Summary.VersionDiversity, []string{"0.98.0", "0.99.0"}; !stringSlicesEqual(got, want) {
		t.Fatalf("version_diversity = %v, want %v", got, want)
	}
	if got, want := body.Summary.ConfigHashDiversity, []string{"hash-a", "hash-b"}; !stringSlicesEqual(got, want) {
		t.Fatalf("config_hash_diversity = %v, want %v", got, want)
	}
	if body.Summary.RemoteConfigStatusCount["applied"] != 1 || body.Summary.RemoteConfigStatusCount["failed"] != 1 || body.Summary.RemoteConfigStatusCount["applying"] != 1 {
		t.Fatalf("remote_config_status_counts = %+v", body.Summary.RemoteConfigStatusCount)
	}
	wantReasons := []string{"mixed_versions", "mixed_effective_config_hashes", "unhealthy_instances", "mixed_remote_config_statuses", "applying_remote_config", "failed_remote_config"}
	for _, reason := range wantReasons {
		if !body.Summary.Heterogeneity[reason] {
			t.Fatalf("heterogeneity[%s] = false; heterogeneity=%+v", reason, body.Summary.Heterogeneity)
		}
	}
	if !stringSlicesEqual(body.Summary.HeterogeneityReasons, wantReasons) {
		t.Fatalf("heterogeneity reasons=%v, want %v", body.Summary.HeterogeneityReasons, wantReasons)
	}
}

func TestGetWorkloadTopology_PreservesDistinctInstanceRecords(t *testing.T) {
	_, router, fake := newTestAPI(t)
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	fake.instances["w-topology-records"] = []opamp.Instance{
		{
			InstanceUID: "uid-stable", PodName: "collector-0", Version: "0.98.0", Healthy: true, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-active", LastMessageAt: now,
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: models.PushStatusApplied, ConfigHash: "hash-active", UpdatedAt: now},
		},
		{
			InstanceUID: "uid-canary", PodName: "collector-1", Version: "0.99.0", Healthy: false, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-canary", LastMessageAt: now.Add(time.Minute),
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: models.PushStatusFailed, ConfigHash: "hash-canary", ErrorMessage: "validation failed", UpdatedAt: now.Add(time.Minute)},
		},
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/w-topology-records/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Instances []opamp.Instance `json:"instances"`
		Summary   struct {
			ConnectedCount           int            `json:"connected_count"`
			HealthyCount             int            `json:"healthy_count"`
			UnhealthyCount           int            `json:"unhealthy_count"`
			VersionDiversity         []string       `json:"version_diversity"`
			ConfigHashDiversity      []string       `json:"config_hash_diversity"`
			RemoteConfigStatusCounts map[string]int `json:"remote_config_status_counts"`
		} `json:"summary"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode topology: %v", err)
	}
	byUID := map[string]opamp.Instance{}
	for _, inst := range body.Instances {
		byUID[inst.InstanceUID] = inst
	}
	if len(byUID) != 2 {
		t.Fatalf("instances len = %d, want 2 distinct records: %+v", len(byUID), byUID)
	}
	stable := byUID["uid-stable"]
	if stable.PodName != "collector-0" || stable.Version != "0.98.0" || stable.EffectiveConfigHash != "hash-active" || !stable.Healthy {
		t.Fatalf("stable instance collapsed or corrupted: %+v", stable)
	}
	if stable.RemoteConfigStatus == nil || stable.RemoteConfigStatus.Status != models.PushStatusApplied || stable.RemoteConfigStatus.ConfigHash != "hash-active" {
		t.Fatalf("stable remote_config_status = %+v", stable.RemoteConfigStatus)
	}
	canary := byUID["uid-canary"]
	if canary.PodName != "collector-1" || canary.Version != "0.99.0" || canary.EffectiveConfigHash != "hash-canary" || canary.Healthy {
		t.Fatalf("canary instance collapsed or corrupted: %+v", canary)
	}
	if canary.RemoteConfigStatus == nil || canary.RemoteConfigStatus.Status != models.PushStatusFailed || canary.RemoteConfigStatus.ConfigHash != "hash-canary" {
		t.Fatalf("canary remote_config_status = %+v", canary.RemoteConfigStatus)
	}
	if body.Summary.ConnectedCount != 2 || body.Summary.HealthyCount != 1 || body.Summary.UnhealthyCount != 1 {
		t.Fatalf("summary counts = %+v, want connected=2 healthy=1 unhealthy=1", body.Summary)
	}
	if got, want := body.Summary.VersionDiversity, []string{"0.98.0", "0.99.0"}; !stringSlicesEqual(got, want) {
		t.Fatalf("version_diversity = %v, want %v", got, want)
	}
	if got, want := body.Summary.ConfigHashDiversity, []string{"hash-active", "hash-canary"}; !stringSlicesEqual(got, want) {
		t.Fatalf("config_hash_diversity = %v, want %v", got, want)
	}
	if body.Summary.RemoteConfigStatusCounts[models.PushStatusApplied] != 1 || body.Summary.RemoteConfigStatusCounts[models.PushStatusFailed] != 1 {
		t.Fatalf("remote_config_status_counts = %+v", body.Summary.RemoteConfigStatusCounts)
	}
}

func TestGetWorkloadTopology_ContractIncludesSchemaCapabilityCountsAndHeterogeneityFlags(t *testing.T) {
	_, router, fake := newTestAPI(t)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	fake.instances["w-topology"] = []opamp.Instance{
		{
			InstanceUID: "uid-a", PodName: "pod-a", Version: "0.98.0", Healthy: true, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-a", ConnectedAt: now.Add(-3 * time.Minute), LastMessageAt: now,
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: "applied", ConfigHash: "hash-a", UpdatedAt: now},
		},
		{
			InstanceUID: "uid-b", PodName: "pod-b", Version: "0.99.0", Healthy: false, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-b", ConnectedAt: now.Add(-2 * time.Minute), LastMessageAt: now,
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: "failed", ConfigHash: "hash-b", ErrorMessage: sensitiveOpAMPErrorFixture, UpdatedAt: now},
		},
		{
			InstanceUID: "uid-c", PodName: "pod-c", Version: "0.99.0", Healthy: true, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-b", ConnectedAt: now.Add(-time.Minute), LastMessageAt: now,
			RemoteConfigStatus: &models.RemoteConfigStatus{Status: "applying", ConfigHash: "hash-b", UpdatedAt: now},
		},
		{
			InstanceUID: "uid-d", PodName: "pod-d", Version: "0.99.0", Healthy: true, AcceptsRemoteConfig: true,
			EffectiveConfigHash: "hash-b", ConnectedAt: now.Add(-30 * time.Second), LastMessageAt: now,
		},
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/w-topology/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	assertNoSensitiveRemoteConfigStatusLeak(t, rec.Body.String())

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode topology: %v", err)
	}
	if body["schema_version"] != "workload-topology.v1" {
		t.Fatalf("schema_version = %v, want workload-topology.v1; body=%s", body["schema_version"], rec.Body.String())
	}
	instances, ok := body["instances"].([]any)
	if !ok || len(instances) != 4 {
		t.Fatalf("instances = %#v", body["instances"])
	}
	first := instances[0].(map[string]any)
	for _, key := range []string{"instance_uid", "pod_name", "version", "connected_at", "last_message_at", "effective_config_hash", "healthy", "accepts_remote_config"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("instance missing %q: %+v", key, first)
		}
	}
	summary := body["summary"].(map[string]any)
	if summary["heterogeneous"] != true {
		t.Fatalf("summary.heterogeneous = %v, want true; summary=%+v", summary["heterogeneous"], summary)
	}
	statusCounts := summary["remote_config_status_counts"].(map[string]any)
	wantStatusCounts := map[string]float64{
		"capable":   4,
		"no_status": 1,
		"sent":      0,
		"applying":  1,
		"applied":   1,
		"failed":    1,
	}
	for key, want := range wantStatusCounts {
		if got := statusCounts[key]; got != want {
			t.Fatalf("remote_config_status_counts[%s] = %v, want %v; counts=%+v", key, got, want, statusCounts)
		}
	}
	heterogeneity := summary["heterogeneity"].(map[string]any)
	for _, key := range []string{"mixed_versions", "mixed_effective_config_hashes", "unhealthy_instances", "mixed_remote_config_statuses", "applying_remote_config", "failed_remote_config"} {
		if heterogeneity[key] != true {
			t.Fatalf("heterogeneity[%s] = %v, want true; heterogeneity=%+v", key, heterogeneity[key], heterogeneity)
		}
	}
	wantReasons := []string{"mixed_versions", "mixed_effective_config_hashes", "unhealthy_instances", "mixed_remote_config_statuses", "applying_remote_config", "failed_remote_config"}
	if got := stringSliceFromAny(summary["heterogeneity_reasons"]); !stringSlicesEqual(got, wantReasons) {
		t.Fatalf("heterogeneity_reasons = %v, want %v", got, wantReasons)
	}
}

func TestGetWorkloadTopology_DriftedCountUsesPersistedActiveConfigHash(t *testing.T) {
	db, router, fake := newTestAPI(t)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-active", DisplayName: "collector active", Type: "collector", Status: "connected", LastSeenAt: now,
		Labels: models.Labels{}, ActiveConfigHash: "hash-a",
	}); err != nil {
		t.Fatal(err)
	}
	fake.instances["w-active"] = []opamp.Instance{
		{InstanceUID: "uid-a", PodName: "pod-a", Healthy: true, EffectiveConfigHash: "hash-a"},
		{InstanceUID: "uid-b", PodName: "pod-b", Healthy: true, EffectiveConfigHash: "hash-b"},
		{InstanceUID: "uid-c", PodName: "pod-c", Healthy: true, EffectiveConfigHash: "hash-b"},
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/w-active/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	summary := decodeTopologySummary(t, rec.Body.Bytes())
	if got := summary["drifted_count"]; got != float64(2) {
		t.Fatalf("drifted_count = %v, want 2 using persisted active_config_hash; summary=%+v", got, summary)
	}
	if got := stringSliceFromAny(summary["config_hash_diversity"]); !stringSlicesEqual(got, []string{"hash-a", "hash-b"}) {
		t.Fatalf("config_hash_diversity = %v, want [hash-a hash-b]", got)
	}
}

func TestGetWorkloadTopology_DriftedCountStaysZeroWithoutActiveConfigHash(t *testing.T) {
	db, router, fake := newTestAPI(t)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-rollout", DisplayName: "collector rollout", Type: "collector", Status: "connected", LastSeenAt: now,
		Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}
	fake.instances["w-rollout"] = []opamp.Instance{
		{InstanceUID: "uid-a", PodName: "pod-a", Healthy: true, EffectiveConfigHash: "hash-a"},
		{InstanceUID: "uid-b", PodName: "pod-b", Healthy: true, EffectiveConfigHash: "hash-b"},
		{InstanceUID: "uid-c", PodName: "pod-c", Healthy: true, EffectiveConfigHash: "hash-b"},
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/w-rollout/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	summary := decodeTopologySummary(t, rec.Body.Bytes())
	if got := summary["drifted_count"]; got != float64(0) {
		t.Fatalf("drifted_count = %v, want 0 without persisted active_config_hash; summary=%+v", got, summary)
	}
	if summary["heterogeneous"] != true {
		t.Fatalf("summary.heterogeneous = %v, want true for mixed effective hashes; summary=%+v", summary["heterogeneous"], summary)
	}
}

func TestGetWorkloadTopology_NoStatusCountsOnlyRemoteConfigCapableInstances(t *testing.T) {
	_, router, fake := newTestAPI(t)
	fake.instances["w-mixed-capability"] = []opamp.Instance{
		{InstanceUID: "uid-capable-no-status", PodName: "pod-a", Healthy: true, AcceptsRemoteConfig: true},
		{InstanceUID: "uid-read-only", PodName: "pod-b", Healthy: true, AcceptsRemoteConfig: false},
		{InstanceUID: "uid-capable-applied", PodName: "pod-c", Healthy: true, AcceptsRemoteConfig: true, RemoteConfigStatus: &models.RemoteConfigStatus{Status: "applied"}},
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/w-mixed-capability/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	summary := decodeTopologySummary(t, rec.Body.Bytes())
	statusCounts := summary["remote_config_status_counts"].(map[string]any)
	if got := statusCounts["capable"]; got != float64(2) {
		t.Fatalf("remote_config_status_counts.capable = %v, want 2; counts=%+v", got, statusCounts)
	}
	if got := statusCounts["no_status"]; got != float64(1) {
		t.Fatalf("remote_config_status_counts.no_status = %v, want 1 capable instance only; counts=%+v", got, statusCounts)
	}
}

func TestGetWorkloadTopology_EmptyShapeHasArraysAndObjects(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/workloads/w-empty/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := strings.TrimSpace(rec.Body.String())
	for _, want := range []string{`"instances":[]`, `"version_diversity":[]`, `"config_hash_diversity":[]`, `"remote_config_status_counts":{`, `"heterogeneity_reasons":[]`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s: %s", want, body)
		}
	}
}

func TestGetWorkloadTopology_EmptyContractShapeHasNoNullArraysMapsOrHeterogeneity(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, http.MethodGet, "/api/workloads/w-empty/topology")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode topology: %v", err)
	}
	if body["schema_version"] != "workload-topology.v1" {
		t.Fatalf("schema_version = %v, want workload-topology.v1; body=%s", body["schema_version"], rec.Body.String())
	}
	if instances, ok := body["instances"].([]any); !ok || len(instances) != 0 {
		t.Fatalf("instances = %#v, want empty array", body["instances"])
	}
	summary := body["summary"].(map[string]any)
	if summary["heterogeneous"] != false {
		t.Fatalf("summary.heterogeneous = %v, want false; summary=%+v", summary["heterogeneous"], summary)
	}
	for _, key := range []string{"version_diversity", "config_hash_diversity", "heterogeneity_reasons"} {
		items, ok := summary[key].([]any)
		if !ok || len(items) != 0 {
			t.Fatalf("summary[%s] = %#v, want empty array", key, summary[key])
		}
	}
	for _, key := range []string{"remote_config_status_counts", "heterogeneity"} {
		value, ok := summary[key].(map[string]any)
		if !ok {
			t.Fatalf("summary[%s] = %#v, want object", key, summary[key])
		}
		if key == "heterogeneity" {
			for _, flag := range []string{"mixed_versions", "mixed_effective_config_hashes", "unhealthy_instances", "mixed_remote_config_statuses", "applying_remote_config", "failed_remote_config"} {
				if value[flag] != false {
					t.Fatalf("heterogeneity[%s] = %v, want false; heterogeneity=%+v", flag, value[flag], value)
				}
			}
		}
	}
}

func decodeTopologySummary(t *testing.T, bodyBytes []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		t.Fatalf("decode topology: %v", err)
	}
	summary, ok := body["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary = %#v, want object; body=%s", body["summary"], string(bodyBytes))
	}
	return summary
}

func stringSliceFromAny(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil
		}
		out = append(out, text)
	}
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- Fleet version intelligence ---

func TestFleetVersionIntelligence_EmptyFleet(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/workloads/version-intelligence?recommended_version=0.100.0")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["schema_version"] != "fleet-version-intelligence.v1" || body["recommended_version"] != "0.100.0" {
		t.Fatalf("unexpected body header: %+v", body)
	}
	for _, key := range []string{"version_matrix", "collectors_below_recommended", "unsupported_config_components", "invalid_versions", "recommendations"} {
		if items, ok := body[key].([]any); !ok || len(items) != 0 {
			t.Fatalf("%s = %#v, want empty array", key, body[key])
		}
	}
}

func TestFleetVersionIntelligence_RequiresAuth(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := httptest.NewRequest(http.MethodGet, "/api/workloads/version-intelligence?recommended_version=0.100.0", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestFleetVersionIntelligence_MixedFleetRecommendations(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	workloads := []models.Workload{
		{ID: "w-old", DisplayName: "collector old", Type: "collector", Version: "0.9.0", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}, AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}},
		{ID: "w-equal", DisplayName: "collector equal", Type: "collector", Version: "v0.100.0", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}},
		{ID: "w-new", DisplayName: "collector new", Type: "collector", Version: "0.101.0", Status: "disconnected", LastSeenAt: now, Labels: models.Labels{"group": "dev"}, FingerprintKeys: models.FingerprintKeys{}},
		{ID: "w-sdk", DisplayName: "sdk agent", Type: "sdk", Version: "0.1.0", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}},
		{ID: "w-invalid", DisplayName: "collector invalid", Type: "collector", Version: "nightly", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}},
		{ID: "w-empty", DisplayName: "collector empty", Type: "collector", Version: "", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}},
	}
	for _, wl := range workloads {
		if err := db.UpsertWorkload(wl); err != nil {
			t.Fatalf("UpsertWorkload(%s): %v", wl.ID, err)
		}
	}
	unsupportedConfig := `receivers:
  otlp: {}
exporters:
  datadog: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [datadog]
`
	unsupportedHash := configHash(unsupportedConfig)
	if err := db.CreateConfig(models.Config{ID: unsupportedHash, Name: "unsupported", Content: unsupportedConfig, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w-old", ConfigID: unsupportedHash, AppliedAt: now, Status: "applied"}); err != nil {
		t.Fatal(err)
	}

	req := authedRequest(t, "GET", "/api/workloads/version-intelligence?recommended_version=0.100.0")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["recommended_version"] != "0.100.0" {
		t.Fatalf("recommended_version = %v", body["recommended_version"])
	}
	matrix := body["version_matrix"].([]any)
	if len(matrix) != 6 {
		t.Fatalf("version_matrix len = %d, matrix=%+v", len(matrix), matrix)
	}
	assertVersionMatrixEntry(t, matrix, "dev", "collector", "disconnected", "0.101.0", 1, "above_recommended")
	assertVersionMatrixEntry(t, matrix, "prod", "collector", "connected", "0.9.0", 1, "below_recommended")
	assertVersionMatrixEntry(t, matrix, "prod", "collector", "connected", "v0.100.0", 1, "at_recommended")
	assertVersionMatrixEntry(t, matrix, "prod", "collector", "connected", "nightly", 1, "unknown")
	assertVersionMatrixEntry(t, matrix, "prod", "sdk", "connected", "0.1.0", 1, "not_applicable")
	below := body["collectors_below_recommended"].([]any)
	if len(below) != 1 || below[0].(map[string]any)["workload_id"] != "w-old" {
		t.Fatalf("collectors_below_recommended = %+v", below)
	}
	invalid := body["invalid_versions"].([]any)
	if len(invalid) != 2 || invalid[0].(map[string]any)["workload_id"] != "w-empty" || invalid[1].(map[string]any)["workload_id"] != "w-invalid" {
		t.Fatalf("invalid_versions = %+v", invalid)
	}
	unsupported := body["unsupported_config_components"].([]any)
	if len(unsupported) != 1 {
		t.Fatalf("unsupported_config_components = %+v", unsupported)
	}
	if got := unsupported[0].(map[string]any)["component_type"]; got != "datadog" {
		t.Fatalf("unsupported component_type = %v", got)
	}
	if got := unsupported[0].(map[string]any)["config_hash"]; got != unsupportedHash {
		t.Fatalf("unsupported config_hash = %v", got)
	}
	assertRecommendationAction(t, body["recommendations"].([]any), "upgrade_collector")
	assertRecommendationAction(t, body["recommendations"].([]any), "choose_older_config")
	assertRecommendationAction(t, body["recommendations"].([]any), "remove_component")
	if strings.Contains(rec.Body.String(), "receivers:") || strings.Contains(rec.Body.String(), "exporters:") {
		t.Fatalf("response leaked raw config content: %s", rec.Body.String())
	}
}

func TestFleetVersionIntelligence_InvalidRecommendedVersionDoesNotFlagCollectors(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-valid", DisplayName: "collector valid", Type: "collector", Version: "0.100.0", Status: "connected",
		LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{},
	}); err != nil {
		t.Fatal(err)
	}

	req := authedRequest(t, "GET", "/api/workloads/version-intelligence?recommended_version=latest")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	assertVersionMatrixEntry(t, body["version_matrix"].([]any), "prod", "collector", "connected", "0.100.0", 1, "unknown")
	if invalid := body["invalid_versions"].([]any); len(invalid) != 0 {
		t.Fatalf("invalid_versions = %+v, want empty when only recommended_version is invalid", invalid)
	}
	if below := body["collectors_below_recommended"].([]any); len(below) != 0 {
		t.Fatalf("collectors_below_recommended = %+v, want empty when recommended_version is invalid", below)
	}
}

func TestFleetVersionIntelligence_CompatibilityMatrixAllCompatible(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	content := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	configID := seedFleetCompatibilityConfig(t, db, "w-compatible", content, now)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w-compatible", DisplayName: "collector compatible", Type: "collector", Version: "0.100.0", Status: "connected",
		LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}, ActiveConfigHash: configID,
		AvailableComponents: &models.AvailableComponents{Hash: "components-compatible", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}},
		AcceptsRemoteConfig: true,
		RemoteConfigStatus:  &models.RemoteConfigStatus{Status: models.PushStatusApplied, ConfigHash: configID, UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/version-intelligence?recommended_version=0.100.0")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	summary := body["compatibility_summary"].(map[string]any)
	if summary["total_collectors"] != float64(1) || summary["not_runnable_count"] != float64(0) || summary["runnable_count"] != float64(1) {
		t.Fatalf("compatibility_summary = %+v", summary)
	}
	matrix := body["compatibility_matrix"].([]any)
	if len(matrix) != 1 {
		t.Fatalf("compatibility_matrix = %+v", matrix)
	}
	entry := matrix[0].(map[string]any)
	if entry["workload_id"] != "w-compatible" || entry["runnable"] != true {
		t.Fatalf("compatibility entry = %+v", entry)
	}
	if entry["config"].(map[string]any)["hash"] != configID {
		t.Fatalf("config metadata = %+v", entry["config"])
	}
	if len(entry["required_components"].([]any)) != 2 {
		t.Fatalf("required_components = %+v", entry["required_components"])
	}
	if strings.Contains(rec.Body.String(), "receivers:") || strings.Contains(rec.Body.String(), "exporters:") {
		t.Fatalf("response leaked raw config content: %s", rec.Body.String())
	}
}

func TestFleetVersionIntelligence_CompatibilityMatrixAggregatesBlockers(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	compatibleConfig := `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
	unsupportedConfig := `receivers:
  otlp: {}
exporters:
  datadog: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [datadog]
`
	compatibleHash := seedFleetCompatibilityConfig(t, db, "w-known-issue", compatibleConfig, now)
	unsupportedHash := seedFleetCompatibilityConfig(t, db, "w-unsupported", unsupportedConfig, now)
	seedFleetCompatibilityConfigWithHash(t, db, "w-no-remote", compatibleHash, now.Add(time.Second))
	seedFleetCompatibilityConfigWithHash(t, db, "w-invalid", compatibleHash, now.Add(2*time.Second))
	seedFleetCompatibilityConfigWithHash(t, db, "w-multi", unsupportedHash, now.Add(3*time.Second))

	workloads := []models.Workload{
		{ID: "w-unsupported", DisplayName: "collector unsupported", Type: "collector", Version: "0.100.0", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}, ActiveConfigHash: unsupportedHash, AvailableComponents: &models.AvailableComponents{Hash: "components-no-datadog", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}, AcceptsRemoteConfig: true, RemoteConfigStatus: &models.RemoteConfigStatus{Status: models.PushStatusApplied, ConfigHash: unsupportedHash, UpdatedAt: now}},
		{ID: "w-known-issue", DisplayName: "collector known issue", Type: "collector", Version: "0.79.0", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}, ActiveConfigHash: compatibleHash, AvailableComponents: &models.AvailableComponents{Hash: "components-known", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}, AcceptsRemoteConfig: true, RemoteConfigStatus: &models.RemoteConfigStatus{Status: models.PushStatusApplied, ConfigHash: compatibleHash, UpdatedAt: now}},
		{ID: "w-no-remote", DisplayName: "collector no remote", Type: "collector", Version: "0.100.0", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}, ActiveConfigHash: compatibleHash, AvailableComponents: &models.AvailableComponents{Hash: "components-no-remote", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}, AcceptsRemoteConfig: false},
		{ID: "w-invalid", DisplayName: "collector invalid", Type: "collector", Version: "nightly", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}, ActiveConfigHash: compatibleHash, AvailableComponents: &models.AvailableComponents{Hash: "components-invalid", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}, AcceptsRemoteConfig: true},
		{ID: "w-multi", DisplayName: "collector multi", Type: "collector", Version: "dev-build", Status: "connected", LastSeenAt: now, Labels: models.Labels{"group": "prod"}, FingerprintKeys: models.FingerprintKeys{}, ActiveConfigHash: unsupportedHash, AvailableComponents: &models.AvailableComponents{Hash: "components-multi", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}, AcceptsRemoteConfig: false},
	}
	for _, wl := range workloads {
		if err := db.UpsertWorkload(wl); err != nil {
			t.Fatalf("UpsertWorkload(%s): %v", wl.ID, err)
		}
	}

	req := authedRequest(t, http.MethodGet, "/api/workloads/version-intelligence?recommended_version=0.100.0")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	summary := body["compatibility_summary"].(map[string]any)
	if summary["not_runnable_count"] != float64(5) || summary["runnable_count"] != float64(0) {
		t.Fatalf("compatibility_summary = %+v", summary)
	}
	notRunnable := summary["not_runnable_collectors"].([]any)
	if len(notRunnable) != 5 {
		t.Fatalf("not_runnable_collectors = %+v", notRunnable)
	}
	matrix := body["compatibility_matrix"].([]any)
	assertCompatibilityEntryBlockedBy(t, matrix, "w-unsupported", "unsupported_component")
	assertCompatibilityEntryBlockedBy(t, matrix, "w-known-issue", "known_issue")
	assertCompatibilityEntryBlockedBy(t, matrix, "w-no-remote", "remote_config_not_accepted")
	assertCompatibilityEntryBlockedBy(t, matrix, "w-invalid", "invalid_version")
	multi := assertCompatibilityEntryBlockedBy(t, matrix, "w-multi", "remote_config_not_accepted")
	if len(multi["blocking_reasons"].([]any)) < 3 {
		t.Fatalf("multi blocker entry = %+v", multi)
	}
	if len(multi["known_issues"].([]any)) != 0 {
		t.Fatalf("dev-build should be invalid, not a comparable known issue match: %+v", multi["known_issues"])
	}
	if strings.Contains(rec.Body.String(), "datadog: {}") {
		t.Fatalf("response leaked raw config content: %s", rec.Body.String())
	}
}

func seedFleetCompatibilityConfig(t *testing.T, db ext.Store, workloadID, content string, appliedAt time.Time) string {
	t.Helper()
	hash := configHash(content)
	if err := db.CreateConfig(models.Config{ID: hash, Name: "cfg-" + hash[:8], Content: content, CreatedAt: appliedAt}); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	seedFleetCompatibilityConfigWithHash(t, db, workloadID, hash, appliedAt)
	return hash
}

func seedFleetCompatibilityConfigWithHash(t *testing.T, db ext.Store, workloadID, hash string, appliedAt time.Time) {
	t.Helper()
	if err := db.UpsertWorkload(models.Workload{ID: workloadID, Type: "collector", Status: "connected", LastSeenAt: appliedAt, Labels: models.Labels{}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true}); err != nil {
		t.Fatalf("UpsertWorkload seed: %v", err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: workloadID, ConfigID: hash, AppliedAt: appliedAt, Status: models.PushStatusApplied}); err != nil {
		t.Fatalf("RecordWorkloadConfig: %v", err)
	}
}

func assertCompatibilityEntryBlockedBy(t *testing.T, matrix []any, workloadID, code string) map[string]any {
	t.Helper()
	for _, item := range matrix {
		entry := item.(map[string]any)
		if entry["workload_id"] != workloadID {
			continue
		}
		if entry["runnable"] != false {
			t.Fatalf("entry %s runnable = %v, want false: %+v", workloadID, entry["runnable"], entry)
		}
		for _, reason := range entry["blocking_reasons"].([]any) {
			if reason.(map[string]any)["code"] == code {
				return entry
			}
		}
		t.Fatalf("entry %s missing blocker %q: %+v", workloadID, code, entry)
	}
	t.Fatalf("missing compatibility entry for %s in %+v", workloadID, matrix)
	return nil
}

func assertVersionMatrixEntry(t *testing.T, matrix []any, group, typ, status, version string, count float64, versionStatus string) {
	t.Helper()
	for _, item := range matrix {
		entry := item.(map[string]any)
		if entry["group"] == group && entry["type"] == typ && entry["status"] == status && entry["version"] == version && entry["count"] == count && entry["version_status"] == versionStatus {
			return
		}
	}
	t.Fatalf("missing matrix entry group=%q type=%q status=%q version=%q count=%v version_status=%q in %+v", group, typ, status, version, count, versionStatus, matrix)
}

func assertRecommendationAction(t *testing.T, recommendations []any, action string) {
	t.Helper()
	for _, item := range recommendations {
		if item.(map[string]any)["action"] == action {
			return
		}
	}
	t.Fatalf("missing recommendation action %q in %+v", action, recommendations)
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

// --- Config safety drift dashboard ---

func TestListConfigDriftSummarizesCollectorRiskSignals(t *testing.T) {
	db, router, fake := newTestAPI(t)
	now := time.Now().UTC()

	if err := db.UpsertWorkload(models.Workload{
		ID: "wl-drift", DisplayName: "collector-prod", Type: "collector", Version: "0.88.0",
		Status: "connected", LastSeenAt: now, Labels: models.Labels{"env": "prod"},
		ActiveConfigHash: "expected-a", AcceptsRemoteConfig: true,
		AvailableComponents: &models.AvailableComponents{Hash: "components-a", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"otlp"}}},
		RemoteConfigStatus:  &models.RemoteConfigStatus{Status: "applied", ConfigHash: "effective-b", UpdatedAt: now.Add(-time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	fake.instances["wl-drift"] = []opamp.Instance{{InstanceUID: "uid-a", PodName: "pod-a", Version: "0.88.0", EffectiveConfigHash: "effective-b", Healthy: true}}
	if err := db.CreateAlert(models.Alert{ID: "alert-drift", WorkloadID: "wl-drift", Rule: "config_drift", Severity: "critical", Message: "drift", FiredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAlert(models.Alert{ID: "alert-version", WorkloadID: "wl-drift", Rule: "version_outdated", Severity: "warning", Message: "old", FiredAt: now}); err != nil {
		t.Fatal(err)
	}

	if err := db.UpsertWorkload(models.Workload{
		ID: "wl-pending", DisplayName: "collector-staging", Type: "collector", Version: "0.100.0",
		Status: "connected", LastSeenAt: now, Labels: models.Labels{"env": "staging"},
		ActiveConfigHash: "expected-p", AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{ID: "expected-p", Name: "pending", Content: validWorkloadConfig, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	sentAt := now.Add(-30 * time.Minute)
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "wl-pending", ConfigID: "expected-p", Status: models.PushStatusSent, AppliedAt: sentAt, SubmittedAt: sentAt, SentAt: &sentAt}); err != nil {
		t.Fatal(err)
	}
	fake.instances["wl-pending"] = []opamp.Instance{{InstanceUID: "uid-p", PodName: "pod-p", Version: "0.100.0", Healthy: true}}

	req := authedRequest(t, "GET", "/api/config-safety/drift")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body models.ConfigDriftDashboard
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode drift dashboard: %v", err)
	}
	if body.Summary.TotalCollectors != 2 || body.Summary.DriftedCollectors != 1 || body.Summary.PendingTooLong != 1 || body.Summary.MissingEffectiveConfig != 1 || body.Summary.OutdatedVersions != 1 {
		t.Fatalf("summary = %+v", body.Summary)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items len = %d", len(body.Items))
	}
	byID := map[string]models.ConfigDriftItem{}
	for _, item := range body.Items {
		byID[item.WorkloadID] = item
	}
	drift := byID["wl-drift"]
	if drift.Env != "prod" || drift.DriftStatus != "drifted" || drift.ExpectedConfigHash != "expected-a" || drift.EffectiveConfigHash != "effective-b" || !drift.HasConfigDriftAlert || !drift.HasVersionOutdatedAlert {
		t.Fatalf("drift item = %+v", drift)
	}
	if len(drift.Actions) == 0 || drift.Actions["view_diff"].Enabled || drift.Actions["view_diff"].Reason == "" || drift.Actions["mark_ignored"].Enabled || drift.Actions["mark_ignored"].Reason == "" {
		t.Fatalf("drift actions = %+v", drift.Actions)
	}
	pending := byID["wl-pending"]
	if pending.DriftStatus != "missing_effective_config" || !pending.PendingTooLong || pending.LastPush == nil || pending.LastPush.Content != "" || pending.LastPush.PushedBy != "" || len(pending.LastPush.InstanceStatuses) != 0 || pending.Actions["push_expected"].Enabled != false || pending.Actions["rollback"].Enabled != false {
		t.Fatalf("pending item = %+v", pending)
	}
}

// --- Push / Validate ---

func TestPushWorkloadConfig_LegacyEndpointRequiresApprovalFlow(t *testing.T) {
	db, router, fake := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	})

	req := authedPost(t, "/api/workloads/w1/config", validWorkloadConfig)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410, body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "config_approval_required" {
		t.Fatalf("code = %q", body["code"])
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("legacy direct push should not reach OpAMP, got %+v", fake.pushed)
	}
	history, err := db.GetWorkloadConfigHistory("w1")
	if err != nil {
		t.Fatalf("GetWorkloadConfigHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("legacy direct push should not record history, got %+v", history)
	}
}

func TestPushWorkloadConfig_RejectsViewerBeforeApprovalGate(t *testing.T) {
	db, router, fake := newTestAPI(t)
	_ = db.UpsertWorkload(models.Workload{
		ID: "w-viewer", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	})

	req := authedRequestForGroups(t, http.MethodPost, "/api/workloads/w-viewer/config", validWorkloadConfig, []string{"viewer"})
	req.Header.Set("Content-Type", "text/yaml")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("viewer push should not reach OpAMP, got %+v", fake.pushed)
	}
	history, err := db.GetWorkloadConfigHistory("w-viewer")
	if err != nil {
		t.Fatalf("GetWorkloadConfigHistory: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("viewer push should not record history, got %+v", history)
	}
}

func TestRollbackWorkloadDefault_OpAMPFailureResponseDoesNotLeakRawError(t *testing.T) {
	db, router, fake := newTestAPI(t)
	fake.err = errors.New(sensitiveOpAMPErrorFixture)

	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: now, Labels: models.Labels{}, ActiveConfigHash: "current",
		AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []models.Config{
		{ID: "previous", Name: "previous", Content: validRollbackYAML, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "current", Name: "current", Content: validRollbackYAML, CreatedAt: now.Add(-time.Hour)},
	} {
		if err := db.CreateConfig(cfg); err != nil {
			t.Fatal(err)
		}
	}
	for _, wc := range []models.WorkloadConfig{
		{WorkloadID: "w1", ConfigID: "previous", AppliedAt: now.Add(-2 * time.Hour), Status: models.PushStatusApplied},
		{WorkloadID: "w1", ConfigID: "current", AppliedAt: now.Add(-time.Hour), Status: models.PushStatusApplied},
	} {
		if err := db.RecordWorkloadConfig(wc); err != nil {
			t.Fatal(err)
		}
	}

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/rollback", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
	assertResponseDoesNotLeakSensitiveOpAMPError(t, rec.Body.String())
	assertLatestWorkloadConfigErrorIsSanitized(t, db, "w1")
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
	if len(hist) != 1 || hist[0].ErrorMessage != models.SanitizeRemoteConfigErrorMessage("oops") || hist[0].Content != "my-yaml" || hist[0].PushedBy != "u@x" {
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
	if history[0].Status != "failed" || history[0].ErrorMessage != models.SanitizeRemoteConfigErrorMessage("collector disconnected") {
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
			t.Fatalf("status response leaked forbidden marker %q", forbidden)
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
