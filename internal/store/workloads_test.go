package store

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestUpsertAndGetWorkload(t *testing.T) {
	db := newTestDB(t)
	w := models.Workload{
		ID:                "wl1",
		FingerprintSource: "k8s",
		FingerprintKeys:   models.FingerprintKeys{"cluster": "prod", "namespace": "obs", "kind": "deployment", "name": "otel"},
		DisplayName:       "otel-collector",
		Type:              "collector",
		Version:           "0.100.0",
		Status:            "connected",
		LastSeenAt:        time.Now().UTC(),
		Labels:            models.Labels{"k8s.pod.name": "otel-abc"},
	}
	if err := db.UpsertWorkload(w); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}

	got, err := db.GetWorkload("wl1")
	if err != nil {
		t.Fatalf("GetWorkload: %v", err)
	}
	if got.FingerprintSource != "k8s" || got.FingerprintKeys["namespace"] != "obs" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestUpsertWorkloadSanitizesRemoteConfigStatusBeforeStorage(t *testing.T) {
	db := newTestDB(t)
	tests := []struct {
		name string
		raw  string
	}{
		{name: "secret token", raw: "collector failed: SECRET_TOKEN=abc123"},
		{name: "internal endpoint", raw: "endpoint=https://tenant-a.internal:4318/v1/traces"},
		{name: "authorization bearer", raw: "authorization=Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.super-secret"},
		{name: "lowercase bearer token", raw: "bearer super-secret-token"},
		{name: "tenant identifier", raw: "remote config failed for tenant tenant-a"},
		{name: "credentials", raw: "credentials username=tenant-a password=hunter2"},
		{name: "config snippet", raw: "config snippet: exporters:\n  otlp:\n    endpoint: https://tenant-a.internal:4317\n    headers:\n      authorization: Bearer SECRET_TOKEN"},
		{name: "collector validation summary", raw: "invalid component in config snippet: exporters.otlp.headers.authorization=Bearer SECRET_TOKEN endpoint=https://tenant-a.internal:4317"},
		{name: "policy summary", raw: "policy refused tenant tenant-a credentials username=tenant-a password=hunter2 authorization=Bearer SECRET_TOKEN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := "wl-sensitive-" + strings.NewReplacer(" ", "-", "_", "-").Replace(tt.name)
			want := models.SanitizeRemoteConfigErrorMessage(tt.raw)

			if err := db.UpsertWorkload(models.Workload{
				ID:         id,
				Type:       "collector",
				Status:     "connected",
				LastSeenAt: time.Now().UTC(),
				RemoteConfigStatus: &models.RemoteConfigStatus{
					Status:       "failed",
					ConfigHash:   "hash-a",
					ErrorMessage: tt.raw,
					UpdatedAt:    time.Unix(0, 0).UTC(),
				},
			}); err != nil {
				t.Fatalf("UpsertWorkload: %v", err)
			}

			var stored string
			if err := db.QueryRow(`SELECT remote_config_status FROM workloads WHERE id = ?`, id).Scan(&stored); err != nil {
				t.Fatalf("query stored remote_config_status: %v", err)
			}
			assertNoSensitiveWorkloadStatusText(t, stored)
			assertStoredRemoteConfigStatusErrorMessage(t, stored, want)

			got, err := db.GetWorkload(id)
			if err != nil {
				t.Fatalf("GetWorkload: %v", err)
			}
			if got.RemoteConfigStatus == nil {
				t.Fatalf("expected remote config status")
			}
			if got.RemoteConfigStatus.ErrorMessage != want {
				t.Fatalf("error_message = %q, want %q", got.RemoteConfigStatus.ErrorMessage, want)
			}
			assertNoSensitiveWorkloadStatusText(t, got.RemoteConfigStatus.ErrorMessage)
		})
	}
}

func TestGetWorkloadSanitizesLegacyRemoteConfigStatusOnRead(t *testing.T) {
	db := newTestDB(t)
	raw := strings.Join([]string{
		"collector failed while applying remote config for tenant tenant-a",
		"SECRET_TOKEN=abc123",
		"authorization=Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.super-secret",
		"bearer super-secret-token",
		"endpoint=https://tenant-a.internal:4318/v1/traces",
		"credentials username=tenant-a password=hunter2",
		"config snippet: exporters:\n  otlp:\n    endpoint: https://tenant-a.internal:4317\n    headers:\n      authorization: Bearer SECRET_TOKEN",
	}, " ")
	updatedAt := time.Unix(42, 0).UTC()
	legacyJSON, err := json.Marshal(map[string]any{
		"status":        "failed",
		"config_hash":   "hash-a",
		"error_message": raw,
		"updated_at":    updatedAt.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal legacy status: %v", err)
	}

	if err := db.UpsertWorkload(models.Workload{
		ID:         "wl-legacy-read",
		Type:       "collector",
		Status:     "connected",
		LastSeenAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}
	if _, err := db.Exec(`UPDATE workloads SET remote_config_status = ? WHERE id = ?`, string(legacyJSON), "wl-legacy-read"); err != nil {
		t.Fatalf("seed legacy remote_config_status: %v", err)
	}

	got, err := db.GetWorkload("wl-legacy-read")
	if err != nil {
		t.Fatalf("GetWorkload: %v", err)
	}
	if got.RemoteConfigStatus == nil {
		t.Fatalf("expected remote config status")
	}
	want := models.SanitizeRemoteConfigErrorMessage(raw)
	if got.RemoteConfigStatus.ErrorMessage != want {
		t.Fatalf("error_message = %q, want %q", got.RemoteConfigStatus.ErrorMessage, want)
	}
	assertNoSensitiveWorkloadStatusText(t, got.RemoteConfigStatus.ErrorMessage)
	if got.RemoteConfigStatus.Status != "failed" || got.RemoteConfigStatus.ConfigHash != "hash-a" || !got.RemoteConfigStatus.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("status metadata was corrupted: %+v", got.RemoteConfigStatus)
	}
}

func TestUpsertWorkloadSanitizesNestedRemoteConfigPushStatusBeforeStorage(t *testing.T) {
	db := newTestDB(t)
	raw := "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"
	now := time.Now().UTC()

	if err := db.UpsertWorkload(models.Workload{
		ID:         "wl-nested-sensitive",
		Type:       "collector",
		Status:     "connected",
		LastSeenAt: now,
		RemoteConfigStatus: &models.RemoteConfigStatus{
			Status:       "failed",
			ConfigHash:   "hash-a",
			ErrorMessage: raw,
			UpdatedAt:    now,
			PushStatus: &models.WorkloadConfig{
				WorkloadID:   "wl-nested-sensitive",
				ConfigID:     "hash-a",
				Status:       "failed",
				ErrorMessage: raw,
				AppliedAt:    now,
				SubmittedAt:  now,
				ErrorGroups:  []models.WorkloadConfigErrorGroup{{Cause: "unknown", SampleMessage: raw}},
				Timeline:     []models.WorkloadConfigTimelineEntry{{State: "failed", At: now, Message: raw, Terminal: true}},
				InstanceStatuses: []models.WorkloadConfigInstanceStatus{
					{InstanceUID: "instance-1", Status: "failed", ErrorMessage: raw, UpdatedAt: now},
				},
			},
		},
	}); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}

	var stored string
	if err := db.QueryRow(`SELECT remote_config_status FROM workloads WHERE id = ?`, "wl-nested-sensitive").Scan(&stored); err != nil {
		t.Fatalf("query stored remote_config_status: %v", err)
	}
	assertNoSensitiveWorkloadStatusText(t, stored)

	got, err := db.GetWorkload("wl-nested-sensitive")
	if err != nil {
		t.Fatalf("GetWorkload: %v", err)
	}
	if got.RemoteConfigStatus == nil || got.RemoteConfigStatus.PushStatus == nil {
		t.Fatalf("expected nested push status")
	}
	encoded, err := json.Marshal(got.RemoteConfigStatus)
	if err != nil {
		t.Fatalf("marshal remote config status: %v", err)
	}
	assertNoSensitiveWorkloadStatusText(t, string(encoded))
}

func TestMigrateSanitizesLegacyRemoteConfigStatusesAtRest(t *testing.T) {
	db := newTestDB(t)
	raw := "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"
	updatedAt := time.Unix(42, 0).UTC()
	legacyPayload := map[string]any{
		"status":        "failed",
		"config_hash":   "hash-a",
		"error_message": raw,
		"updated_at":    updatedAt.Format(time.RFC3339),
	}
	legacyJSON, err := json.Marshal(legacyPayload)
	if err != nil {
		t.Fatalf("marshal legacy status: %v", err)
	}

	if err := db.UpsertWorkload(models.Workload{
		ID:         "wl-legacy",
		Type:       "collector",
		Status:     "connected",
		LastSeenAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}
	if _, err := db.Exec(`UPDATE workloads SET remote_config_status = ? WHERE id = ?`, string(legacyJSON), "wl-legacy"); err != nil {
		t.Fatalf("seed legacy remote_config_status: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var stored string
	if err := db.QueryRow(`SELECT remote_config_status FROM workloads WHERE id = ?`, "wl-legacy").Scan(&stored); err != nil {
		t.Fatalf("query stored remote_config_status: %v", err)
	}
	assertNoSensitiveWorkloadStatusText(t, stored)

	var got models.RemoteConfigStatus
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatalf("unmarshal stored remote_config_status: %v", err)
	}
	if got.Status != "failed" || got.ConfigHash != "hash-a" || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("status metadata was corrupted: %+v", got)
	}
	if got.ErrorMessage != "Remote config error details redacted" {
		t.Fatalf("error_message = %q, want redacted summary", got.ErrorMessage)
	}
}

func assertNoSensitiveWorkloadStatusText(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{"SECRET_TOKEN", "abc123", "authorization=Bearer", "Bearer SECRET_TOKEN", "eyJhbGci", "super-secret", "super-secret-token", "tenant-a", "tenant-a.internal", "4318", "4317", "/v1/traces", "hunter2", "credentials", "password=", "username=", "endpoint:", "config snippet", "exporters:", "headers:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("remote config status leaked forbidden marker %q", forbidden)
		}
	}
}

func assertStoredRemoteConfigStatusErrorMessage(t *testing.T, stored, want string) {
	t.Helper()
	var status map[string]any
	if err := json.Unmarshal([]byte(stored), &status); err != nil {
		t.Fatalf("unmarshal stored remote_config_status: %v", err)
	}
	if got := status["error_message"]; got != want {
		t.Fatalf("stored error_message = %q, want %q", got, want)
	}
}

func TestListWorkloadsExcludesArchived(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{ID: "live", Type: "sdk", Status: "connected", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkload(models.Workload{ID: "gone", Type: "sdk", Status: "disconnected", LastSeenAt: now, ArchivedAt: &now}); err != nil {
		t.Fatal(err)
	}

	list, err := db.ListWorkloads(false)
	if err != nil {
		t.Fatalf("ListWorkloads: %v", err)
	}
	if len(list) != 1 || list[0].ID != "live" {
		t.Fatalf("expected only live, got %+v", list)
	}
	allIncl, _ := db.ListWorkloads(true)
	if len(allIncl) != 2 {
		t.Fatalf("expected 2 with includeArchived=true, got %d", len(allIncl))
	}
}

func TestUpsertWorkloadProjectsQueryableAttributes(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	w := models.Workload{
		ID:                "wl1",
		FingerprintSource: "k8s",
		FingerprintKeys: models.FingerprintKeys{
			"cluster":   "prod-eu",
			"namespace": "observability",
			"kind":      "deployment",
			"name":      "otel-collector",
		},
		DisplayName: "otel-collector",
		Type:        "collector",
		Status:      "connected",
		LastSeenAt:  now,
		Labels: models.Labels{
			"cloud.region": "eu-west-3",
			"env":          "prod",
			"team":         "platform",
		},
	}
	if err := db.UpsertWorkload(w); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}

	got := queryWorkloadAttributes(t, db, "wl1")
	want := []string{
		"fingerprint:cluster=prod-eu",
		"fingerprint:kind=deployment",
		"fingerprint:name=otel-collector",
		"fingerprint:namespace=observability",
		"label:cloud.region=eu-west-3",
		"label:env=prod",
		"label:team=platform",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queryable attributes = %#v, want %#v", got, want)
	}

	w.Labels = models.Labels{"env": "staging"}
	if err := db.UpsertWorkload(w); err != nil {
		t.Fatalf("UpsertWorkload update: %v", err)
	}
	got = queryWorkloadAttributes(t, db, "wl1")
	want = []string{
		"fingerprint:cluster=prod-eu",
		"fingerprint:kind=deployment",
		"fingerprint:name=otel-collector",
		"fingerprint:namespace=observability",
		"label:env=staging",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queryable attributes after update = %#v, want %#v", got, want)
	}
}

func TestMarkWorkloadDisconnectedSetsRetention(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}

	until := now.Add(24 * time.Hour)
	if err := db.MarkWorkloadDisconnected("w1", until); err != nil {
		t.Fatalf("MarkWorkloadDisconnected: %v", err)
	}
	w, err := db.GetWorkload("w1")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "disconnected" {
		t.Fatalf("status = %q, want disconnected", w.Status)
	}
	if w.RetentionUntil == nil || !w.RetentionUntil.Equal(until) {
		t.Fatalf("retention_until = %v, want %v", w.RetentionUntil, until)
	}
}

func TestClearWorkloadRetention(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	until := now.Add(time.Hour)
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "disconnected", LastSeenAt: now, RetentionUntil: &until}); err != nil {
		t.Fatal(err)
	}
	if err := db.ClearWorkloadRetention("w1"); err != nil {
		t.Fatal(err)
	}
	w, _ := db.GetWorkload("w1")
	if w.RetentionUntil != nil {
		t.Fatalf("expected retention_until nil, got %v", w.RetentionUntil)
	}
}

func TestArchiveExpiredWorkloads(t *testing.T) {
	db := newTestDB(t)
	past := time.Now().UTC().Add(-time.Hour)
	future := time.Now().UTC().Add(time.Hour)
	if err := db.UpsertWorkload(models.Workload{ID: "old", Type: "collector", Status: "disconnected", LastSeenAt: past, RetentionUntil: &past}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkload(models.Workload{ID: "young", Type: "collector", Status: "disconnected", LastSeenAt: past, RetentionUntil: &future}); err != nil {
		t.Fatal(err)
	}

	n, err := db.ArchiveExpiredWorkloads(time.Now().UTC())
	if err != nil {
		t.Fatalf("ArchiveExpiredWorkloads: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 archived, got %d", n)
	}
	old, _ := db.GetWorkload("old")
	if old.ArchivedAt == nil {
		t.Fatalf("expected ArchivedAt set")
	}
	young, _ := db.GetWorkload("young")
	if young.ArchivedAt != nil {
		t.Fatalf("expected young not archived")
	}
}

func TestDeleteWorkload(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "sdk", Status: "connected", LastSeenAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteWorkload("w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetWorkload("w1"); err == nil {
		t.Fatalf("expected not-found error after delete")
	}
}

func queryWorkloadAttributes(t *testing.T, db *DB, workloadID string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT source, key, value FROM workload_attributes WHERE workload_id = ? ORDER BY source, key`, workloadID)
	if err != nil {
		t.Fatalf("query workload_attributes: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var source, key, value string
		if err := rows.Scan(&source, &key, &value); err != nil {
			t.Fatalf("scan workload_attributes: %v", err)
		}
		out = append(out, source+":"+key+"="+value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate workload_attributes: %v", err)
	}
	return out
}
