package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// authedJSONRequest builds a request with the admin Bearer token and a JSON body.
func authedJSONRequest(t *testing.T, method, url, body string, groups []string) *http.Request {
	t.Helper()
	a := auth.New("test-secret-key-at-least-32-bytes!")
	if len(groups) == 0 {
		groups = []string{"administrator"}
	}
	tok, _ := a.GenerateToken("user-001", "admin@test.com", groups)
	req := httptest.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func seedHistory(t *testing.T, db ext.Store, workloadID, hash, content string) {
	t.Helper()
	if err := db.UpsertWorkload(models.Workload{
		ID: workloadID, Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
		AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{
		ID: hash, Name: "rev", Content: content,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: workloadID, ConfigID: hash, Status: "applied",
	}); err != nil {
		t.Fatal(err)
	}
}

// validRollbackYAML is a minimal config that passes light validation.
const validRollbackYAML = `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`

func TestSetWorkloadConfigLabel_HappyPath(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", "yaml-a")

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/label",
		`{"label":"stable-2026-05"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	wc, err := db.GetWorkloadConfigByHash("w1", "hash-a")
	if err != nil || wc == nil {
		t.Fatalf("Get: %v / %+v", err, wc)
	}
	if wc.Label == nil || *wc.Label != "stable-2026-05" {
		t.Fatalf("Label = %v, want stable-2026-05", wc.Label)
	}

	events := audit.snapshot()
	if len(events) != 1 || events[0].Action != "config.label" || events[0].Email != "admin@test.com" {
		t.Fatalf("audit = %+v", events)
	}
	if events[0].Resource != "workload" || events[0].ResourceID != "w1" {
		t.Errorf("Resource/ResourceID = (%q, %q), want (workload, w1)", events[0].Resource, events[0].ResourceID)
	}
	if events[0].Detail != "stable-2026-05" {
		t.Errorf("Detail = %q, want stable-2026-05", events[0].Detail)
	}
}

func TestSetWorkloadConfigLabel_EmptyClearsExisting(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", "yaml-a")
	_ = db.SetWorkloadConfigLabel("w1", "hash-a", "stale")

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/label", `{"label":""}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	wc, _ := db.GetWorkloadConfigByHash("w1", "hash-a")
	if wc.Label != nil {
		t.Fatalf("Label = %v, want nil", wc.Label)
	}
}

func TestSetWorkloadConfigLabel_404OnUnknownHash(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", "yaml-a")

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/ghost/label", `{"label":"x"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetWorkloadConfigLabel_403ForViewer(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", "yaml-a")

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/label",
		`{"label":"x"}`, []string{"viewer"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSetWorkloadConfigLabel_RejectsLabelOver128Chars(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", "yaml-a")

	long := strings.Repeat("x", 129)
	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/label",
		`{"label":"`+long+`"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetWorkloadConfigByHash_HappyPath(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", "yaml-content")
	_ = db.SetWorkloadConfigLabel("w1", "hash-a", "blessed")

	req := authedJSONRequest(t, http.MethodGet, "/api/workloads/w1/configs/hash-a", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var wc models.WorkloadConfig
	_ = json.Unmarshal(rec.Body.Bytes(), &wc)
	if wc.Content != "yaml-content" {
		t.Errorf("Content = %q", wc.Content)
	}
	if wc.Label == nil || *wc.Label != "blessed" {
		t.Errorf("Label = %v", wc.Label)
	}
}

func TestGetWorkloadConfigByHash_404(t *testing.T) {
	_, router, _, _ := newAuditTestAPI(t)

	req := authedJSONRequest(t, http.MethodGet, "/api/workloads/w1/configs/ghost", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRollbackWorkloadConfig_RecordsPendingPushAndPushesContent(t *testing.T) {
	db, router, fake, audit := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", validRollbackYAML)

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/rollback", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	hist, _ := db.GetWorkloadConfigHistory("w1")
	if len(hist) != 2 {
		t.Fatalf("history len = %d, want 2 (original applied + new pending)", len(hist))
	}
	// Newest first.
	if hist[0].Status != "pending" || hist[0].ConfigID != "hash-a" || hist[0].PushedBy != "admin@test.com" {
		t.Fatalf("rollback row = %+v", hist[0])
	}
	if len(fake.pushed) != 1 || string(fake.pushed[0].Body) != validRollbackYAML {
		t.Fatalf("opamp push not invoked with original content: %+v", fake.pushed)
	}

	events := audit.snapshot()
	if len(events) != 1 || events[0].Action != "config.rollback" {
		t.Fatalf("audit = %+v", events)
	}
}

func TestRollbackWorkloadConfig_404OnUnknownHash(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/ghost/rollback", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestRollbackWorkloadConfig_409WhenWorkloadRefusesRemoteConfig(t *testing.T) {
	db, router, fake, _ := newAuditTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: false,
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.CreateConfig(models.Config{ID: "h1", Name: "x", Content: validRollbackYAML, CreatedAt: time.Now().UTC()})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "h1", Status: "applied"})

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/h1/rollback", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if len(fake.pushed) != 0 {
		t.Fatalf("expected no opamp push, got %d", len(fake.pushed))
	}
}

func TestRollbackWorkloadConfig_403ForViewer(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", validRollbackYAML)

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/rollback",
		"", []string{"viewer"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestSetWorkloadConfigLabel_401WithoutToken(t *testing.T) {
	_, router, _, _ := newAuditTestAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/api/workloads/w1/configs/h1/label", strings.NewReader(`{"label":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSetWorkloadConfigLabel_503WhenAuditFails(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", "yaml-a")
	audit.failWith(errors.New("audit DB down"))

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/label",
		`{"label":"stable"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	// Label DID persist — audit failed after SetWorkloadConfigLabel succeeded.
	wc, _ := db.GetWorkloadConfigByHash("w1", "hash-a")
	if wc.Label == nil || *wc.Label != "stable" {
		t.Errorf("Label = %v, want stable", wc.Label)
	}
}

func TestRollbackWorkloadConfig_503WhenAuditFails(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedHistory(t, db, "w1", "hash-a", validRollbackYAML)
	audit.failWith(errors.New("audit DB down"))

	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w1/configs/hash-a/rollback", "", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
