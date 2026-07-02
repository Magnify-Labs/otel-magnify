package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestPushActivity_Unauthorized(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := httptest.NewRequest("GET", "/api/pushes/activity?window=7d", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPushActivity_EmptyReturnsSevenZeroDays(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, "GET", "/api/pushes/activity?window=7d")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var points []models.PushActivityPoint
	if err := json.NewDecoder(rec.Body).Decode(&points); err != nil {
		t.Fatal(err)
	}
	if len(points) != 7 {
		t.Fatalf("len = %d, want 7", len(points))
	}
	for i, p := range points {
		if p.Count != 0 {
			t.Errorf("points[%d].Count = %d, want 0", i, p.Count)
		}
		if p.Day == "" {
			t.Errorf("points[%d].Day is empty", i)
		}
	}
}

func TestPushActivity_BucketsByDay(t *testing.T) {
	db, router, _ := newTestAPI(t)

	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	threeDaysAgo := today.AddDate(0, 0, -3)

	// Seed a workload + config so FK constraints pass. The config id is the
	// content hash in production; for tests any non-empty string works.
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: now, Labels: models.Labels{},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{
		ID: "cfg-1", Name: "test", Content: "receivers:", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	// Two pushes today, one three days ago.
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "w1", ConfigID: "cfg-1", AppliedAt: today.Add(1 * time.Hour), Status: "applied",
	}))
	must(db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "w1", ConfigID: "cfg-1", AppliedAt: today.Add(2 * time.Hour), Status: "applied",
	}))
	must(db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "w1", ConfigID: "cfg-1", AppliedAt: threeDaysAgo.Add(5 * time.Hour), Status: "applied",
	}))

	req := authedRequest(t, "GET", "/api/pushes/activity?window=7d")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var points []models.PushActivityPoint
	if err := json.NewDecoder(rec.Body).Decode(&points); err != nil {
		t.Fatal(err)
	}
	if len(points) != 7 {
		t.Fatalf("len = %d, want 7", len(points))
	}

	byDay := make(map[string]int, len(points))
	for _, p := range points {
		byDay[p.Day] = p.Count
	}
	todayKey := today.Format("2006-01-02")
	threeAgoKey := threeDaysAgo.Format("2006-01-02")
	if byDay[todayKey] != 2 {
		t.Errorf("byDay[%s] = %d, want 2", todayKey, byDay[todayKey])
	}
	if byDay[threeAgoKey] != 1 {
		t.Errorf("byDay[%s] = %d, want 1", threeAgoKey, byDay[threeAgoKey])
	}
}

func TestPushActivity_RejectsUnsupportedWindow(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedRequest(t, "GET", "/api/pushes/activity?window=30d")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPushGroups_ListSavedGroups(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, "GET", "/api/push-groups")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var groups []pushGroup
	if err := json.NewDecoder(rec.Body).Decode(&groups); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(groups))
	selectors := make(map[string]pushGroupSelector, len(groups))
	for _, group := range groups {
		ids = append(ids, group.ID)
		selectors[group.ID] = group.Selector
	}
	for _, want := range []string{"prod-eu", "staging", "edge", "payments"} {
		if !slices.Contains(ids, want) {
			t.Fatalf("group ids = %v, missing %q", ids, want)
		}
		if len(selectors[want].MatchLabels) == 0 {
			t.Fatalf("group %s has empty match_labels selector", want)
		}
	}
}

func TestPushPreview_BucketsSavedGroupTargets(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	seed := []models.Workload{
		{ID: "payments-capable", DisplayName: "payments-capable", Type: "collector", Status: "connected", LastSeenAt: now, Version: "0.98.0", Labels: models.Labels{"otel.magnify/selector.team": "payments", "otel.magnify/selector.env": "prod", "otel.magnify/selector.cluster": "prod-eu"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true, AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}},
		{ID: "payments-read-only", DisplayName: "payments-read-only", Type: "collector", Status: "connected", LastSeenAt: now, Version: "0.98.0", Labels: models.Labels{"otel.magnify/selector.team": "payments", "otel.magnify/selector.env": "prod", "otel.magnify/selector.cluster": "prod-eu"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: false},
		{ID: "payments-incompatible", DisplayName: "payments-incompatible", Type: "collector", Status: "connected", LastSeenAt: now, Version: "0.74.0", Labels: models.Labels{"otel.magnify/selector.team": "payments", "otel.magnify/selector.env": "prod", "otel.magnify/selector.cluster": "prod-eu"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true, AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"otlp"}}}},
		{ID: "payments-offline", DisplayName: "payments-offline", Type: "collector", Status: "disconnected", LastSeenAt: now, Version: "0.98.0", Labels: models.Labels{"otel.magnify/selector.team": "payments", "otel.magnify/selector.env": "prod", "otel.magnify/selector.cluster": "prod-eu"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true, AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}}},
		{ID: "checkout", DisplayName: "checkout", Type: "collector", Status: "connected", LastSeenAt: now, Version: "0.98.0", Labels: models.Labels{"team": "checkout", "env": "prod"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true},
	}
	for _, workload := range seed {
		if err := db.UpsertWorkload(workload); err != nil {
			t.Fatal(err)
		}
	}

	body := []byte(`{
		"group_id": "payments",
		"config_content": "receivers:\n  otlp:\nexporters:\n  logging:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [logging]\n"
	}`)
	req := authedRequest(t, "POST", "/api/pushes/preview")
	req.Body = ioNopCloser{Reader: bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got pushPreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.GroupID != "payments" {
		t.Fatalf("group_id = %q, want payments", got.GroupID)
	}
	if got.SelectorSource != "trusted_labels" {
		t.Fatalf("selector_source = %q, want trusted_labels", got.SelectorSource)
	}
	if got.TargetedCount != 4 {
		t.Fatalf("targeted_count = %d, want 4", got.TargetedCount)
	}
	if got.Breakdown.RemoteConfigCapable != 1 || got.Breakdown.ReadOnly != 1 || got.Breakdown.Incompatible != 1 || got.Breakdown.Offline != 1 {
		t.Fatalf("breakdown = %#v, want 1 in each bucket", got.Breakdown)
	}
	if len(got.Targets) != 4 {
		t.Fatalf("targets len = %d, want 4", len(got.Targets))
	}
}

func TestPushPreview_SavedGroupUsesTrustedSelectorLabels(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	for _, workload := range []models.Workload{
		{
			ID: "spoofed-payments", DisplayName: "spoofed-payments", Type: "collector", Status: "connected", LastSeenAt: now,
			Labels: models.Labels{"team": "payments", "env": "prod"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true,
		},
		{
			ID: "trusted-payments", DisplayName: "trusted-payments", Type: "collector", Status: "connected", LastSeenAt: now,
			Labels: models.Labels{"otel.magnify/selector.team": "payments", "otel.magnify/selector.env": "prod"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true,
		},
	} {
		if err := db.UpsertWorkload(workload); err != nil {
			t.Fatal(err)
		}
	}

	body := []byte(`{"group_id":"payments","config_content":""}`)
	req := authedRequest(t, "POST", "/api/pushes/preview")
	req.Body = ioNopCloser{Reader: bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got pushPreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TargetedCount != 1 {
		t.Fatalf("targeted_count = %d, want 1: %#v", got.TargetedCount, got.Targets)
	}
	if got.SelectorSource != "trusted_labels" {
		t.Fatalf("selector_source = %q, want trusted_labels", got.SelectorSource)
	}
	if got.Targets[0].WorkloadID != "trusted-payments" {
		t.Fatalf("target = %q, want trusted-payments", got.Targets[0].WorkloadID)
	}
}

func TestPushPreview_BucketsDynamicSelectorTargets(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	for _, workload := range []models.Workload{
		{ID: "dyn-1", DisplayName: "dyn-1", Type: "collector", Status: "connected", LastSeenAt: now, Version: "0.98.0", Labels: models.Labels{"cluster": "prod-eu", "team": "platform", "env": "prod"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true, AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"debug"}}}},
		{ID: "dyn-2", DisplayName: "dyn-2", Type: "collector", Status: "connected", LastSeenAt: now, Version: "0.97.0", Labels: models.Labels{"cluster": "prod-eu", "team": "platform", "env": "prod"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true, AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"debug"}}}},
		{ID: "other", DisplayName: "other", Type: "collector", Status: "connected", LastSeenAt: now, Version: "0.98.0", Labels: models.Labels{"cluster": "prod-us", "team": "platform", "env": "prod"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true, AvailableComponents: &models.AvailableComponents{Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"debug"}}}},
	} {
		if err := db.UpsertWorkload(workload); err != nil {
			t.Fatal(err)
		}
	}

	body := []byte(`{"selector":{"match_labels":{"cluster":"prod-eu","team":"platform"},"types":["collector"],"versions":["0.98.0"],"capabilities":["debug"]},"config_content":""}`)
	req := authedRequest(t, "POST", "/api/pushes/preview")
	req.Body = ioNopCloser{Reader: bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got pushPreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.TargetedCount != 1 || got.Breakdown.RemoteConfigCapable != 1 {
		t.Fatalf("preview = %#v, want one capable dynamic target", got)
	}
	if got.SelectorSource != "collector_labels" {
		t.Fatalf("selector_source = %q, want collector_labels", got.SelectorSource)
	}
}

func TestPushPreview_DynamicSelectorDoesNotUseTrustedLabels(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{
		ID: "trusted-prod", DisplayName: "trusted-prod", Type: "collector", Status: "connected", LastSeenAt: now,
		Labels: models.Labels{"otel.magnify/selector.env": "prod", "env": "staging"}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"selector":{"match_labels":{"otel.magnify/selector.env":"prod"}},"config_content":""}`)
	req := authedRequest(t, "POST", "/api/pushes/preview")
	req.Body = ioNopCloser{Reader: bytes.NewReader(body)}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got pushPreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.SelectorSource != "collector_labels" {
		t.Fatalf("selector_source = %q, want collector_labels", got.SelectorSource)
	}
	if got.TargetedCount != 0 {
		t.Fatalf("targeted_count = %d, want 0: %#v", got.TargetedCount, got.Targets)
	}
}

type ioNopCloser struct {
	*bytes.Reader
}

func (ioNopCloser) Close() error { return nil }
