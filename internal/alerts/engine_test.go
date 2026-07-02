package alerts

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEvaluate_WorkloadDown(t *testing.T) {
	db := newTestDB(t)

	// Workload last seen 10 minutes ago
	db.UpsertWorkload(models.Workload{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})

	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Fatalf("len = %d, want 1", len(alerts))
	}
	if alerts[0].Rule != "workload_down" {
		t.Errorf("Rule = %q, want workload_down", alerts[0].Rule)
	}
	if alerts[0].WorkloadID != "a1" {
		t.Errorf("WorkloadID = %q, want a1", alerts[0].WorkloadID)
	}
}

func TestEvaluate_WorkloadDown_NoDouble(t *testing.T) {
	db := newTestDB(t)

	db.UpsertWorkload(models.Workload{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})

	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 {
		t.Errorf("len = %d, want 1 (no duplicates)", len(alerts))
	}
}

func TestEvaluate_WorkloadRecovers(t *testing.T) {
	db := newTestDB(t)

	// Workload was down
	db.UpsertWorkload(models.Workload{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})
	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()

	// Workload comes back
	db.UpsertWorkload(models.Workload{
		ID: "a1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Errorf("unresolved alerts = %d, want 0", len(alerts))
	}
}

func TestEvaluate_ArchivedWorkloadSkipped(t *testing.T) {
	db := newTestDB(t)

	// Archived workload last seen 10 minutes ago — should not fire an alert.
	archivedAt := time.Now().UTC().Add(-time.Hour)
	db.UpsertWorkload(models.Workload{
		ID: "archived-1", Type: "collector", Status: "disconnected",
		LastSeenAt: time.Now().UTC().Add(-10 * time.Minute),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
		ArchivedAt: &archivedAt,
	})

	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 {
		t.Errorf("unresolved alerts = %d, want 0 (archived workload ignored)", len(alerts))
	}
}

func TestEvaluate_ConfigDrift(t *testing.T) {
	db := newTestDB(t)

	// Workload is healthy (seen recently) but has a pending config older than 5 min.
	db.UpsertWorkload(models.Workload{
		ID: "a2", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})

	// Insert a config so we have a valid config_id to reference.
	configID := "cccccccccccccccccccccccccccccccc"
	db.CreateConfig(models.Config{
		ID: configID, Name: "test-cfg", Content: "receivers: []",
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute), CreatedBy: "test",
	})

	// Manually insert a workload_configs row with a timestamp older than 5 min.
	_, err := db.Exec(
		`INSERT INTO workload_configs (workload_id, config_id, applied_at, status) VALUES (?, ?, ?, ?)`,
		"a2", configID, time.Now().UTC().Add(-10*time.Minute), "pending",
	)
	if err != nil {
		t.Fatalf("insert workload_config: %v", err)
	}

	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, a := range alerts {
		if a.WorkloadID == "a2" && a.Rule == "config_drift" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected config_drift alert for workload a2, got %+v", alerts)
	}
}

func TestEvaluate_ConfigDrift_Resolves(t *testing.T) {
	db := newTestDB(t)

	db.UpsertWorkload(models.Workload{
		ID: "a3", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})

	configID := "dddddddddddddddddddddddddddddddd"
	db.CreateConfig(models.Config{
		ID: configID, Name: "test-cfg2", Content: "receivers: []",
		CreatedAt: time.Now().UTC().Add(-10 * time.Minute), CreatedBy: "test",
	})

	// Start with a stale pending config to fire the alert.
	_, err := db.Exec(
		`INSERT INTO workload_configs (workload_id, config_id, applied_at, status) VALUES (?, ?, ?, ?)`,
		"a3", configID, time.Now().UTC().Add(-10*time.Minute), "pending",
	)
	if err != nil {
		t.Fatalf("insert workload_config: %v", err)
	}

	engine := New(db, nil, 5*time.Minute, "")
	engine.Evaluate()

	// Simulate workload applying the config: update status to "applied".
	_, err = db.Exec(`UPDATE workload_configs SET status = 'applied' WHERE workload_id = ? AND config_id = ?`, "a3", configID)
	if err != nil {
		t.Fatalf("update workload_config: %v", err)
	}

	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alerts {
		if a.WorkloadID == "a3" && a.Rule == "config_drift" {
			t.Errorf("config_drift alert should be resolved but is still unresolved: %+v", a)
		}
	}
}

func TestEvaluate_VersionOutdated(t *testing.T) {
	db := newTestDB(t)

	db.UpsertWorkload(models.Workload{
		ID: "a4", Type: "collector", Version: "0.8.0", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})

	engine := New(db, nil, 5*time.Minute, "0.9.0")
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, a := range alerts {
		if a.WorkloadID == "a4" && a.Rule == "version_outdated" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected version_outdated alert for workload a4, got %+v", alerts)
	}
}

func TestEvaluate_VersionOutdated_UsesSemanticVersionOrdering(t *testing.T) {
	db := newTestDB(t)

	db.UpsertWorkload(models.Workload{
		ID: "semver-low", Type: "collector", Version: "0.9.0", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})
	db.UpsertWorkload(models.Workload{
		ID: "semver-equal", Type: "collector", Version: "v0.100.0", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})
	db.UpsertWorkload(models.Workload{
		ID: "semver-invalid", Type: "collector", Version: "not-a-version", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})

	engine := New(db, nil, 5*time.Minute, "0.10.0")
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAlert(alerts, "semver-low", "version_outdated") {
		t.Fatalf("expected semver-low to be outdated, got %+v", alerts)
	}
	if hasAlert(alerts, "semver-equal", "version_outdated") {
		t.Fatalf("v0.100.0 should equal 0.100.0 and not be outdated: %+v", alerts)
	}
	if hasAlert(alerts, "semver-invalid", "version_outdated") {
		t.Fatalf("invalid versions should be unknown/non-crashing, not outdated: %+v", alerts)
	}
}

func hasAlert(alerts []models.Alert, workloadID, rule string) bool {
	for _, a := range alerts {
		if a.WorkloadID == workloadID && a.Rule == rule {
			return true
		}
	}
	return false
}

func TestEvaluate_VersionOutdated_Resolves(t *testing.T) {
	db := newTestDB(t)

	db.UpsertWorkload(models.Workload{
		ID: "a5", Type: "collector", Version: "0.8.0", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})

	engine := New(db, nil, 5*time.Minute, "0.9.0")
	engine.Evaluate()

	// Workload upgrades its version.
	db.UpsertWorkload(models.Workload{
		ID: "a5", Type: "collector", Version: "0.9.0", Status: "connected",
		LastSeenAt: time.Now().UTC(),
		Labels:     models.Labels{}, FingerprintKeys: models.FingerprintKeys{},
	})
	engine.Evaluate()

	alerts, err := db.ListAlerts(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alerts {
		if a.WorkloadID == "a5" && a.Rule == "version_outdated" {
			t.Errorf("version_outdated alert should be resolved but is still unresolved: %+v", a)
		}
	}
}
