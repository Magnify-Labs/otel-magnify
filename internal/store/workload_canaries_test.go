package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestCanaryStatusPersistsSanitizedPollingContract(t *testing.T) {
	db := newTestDB(t)
	seedCanaryPersistence(t, db, "cfg-canary")

	created := time.Now().UTC()
	status := models.CanaryStatus{
		ID: "canary-test", WorkloadID: "wl-canary", ConfigHash: "cfg-canary", Status: models.CanaryStatusRunning,
		Selection: models.CanarySelection{Strategy: "one", InstanceUID: "inst-a"}, Actor: "operator@example.com",
		CreatedAt: created, UpdatedAt: created,
		Targets: []models.CanaryTarget{{InstanceUID: "inst-a", PodName: "pod-a", Status: models.InstanceStatusSent, UpdatedAt: created}},
	}
	if err := db.CreateCanaryStatus(status); err != nil {
		t.Fatal(err)
	}

	status.Targets[0].Status = models.PushStatusFailed
	status.Targets[0].StopReason = models.CanaryStopRemoteConfigFailed
	status.Status = models.CanaryStatusStopped
	if err := db.UpdateCanaryStatus(status); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetCanaryStatus("wl-canary", "canary-test")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected canary")
	}
	if got.ConfigHash != "cfg-canary" || got.Counts.Failed != 1 || got.Targets[0].StopReason != models.CanaryStopRemoteConfigFailed {
		t.Fatalf("unexpected canary: %+v", got)
	}
}

func TestCanaryStatusFollowsRemoteConfigInstanceStatus(t *testing.T) {
	db := newTestDB(t)
	seedCanaryPersistence(t, db, "cfg-canary")
	created := time.Now().UTC().Add(-time.Minute)
	status := models.CanaryStatus{
		ID: "canary-test", WorkloadID: "wl-canary", ConfigHash: "cfg-canary", Status: models.CanaryStatusRunning,
		Selection: models.CanarySelection{Strategy: "count", Count: 2}, Actor: "operator@example.com",
		CreatedAt: created, UpdatedAt: created,
		Targets: []models.CanaryTarget{
			{InstanceUID: "inst-a", PodName: "pod-a", Status: models.InstanceStatusSent, UpdatedAt: created},
			{InstanceUID: "inst-b", PodName: "pod-b", Status: models.InstanceStatusSent, UpdatedAt: created},
		},
	}
	if err := db.CreateCanaryStatus(status); err != nil {
		t.Fatal(err)
	}

	if err := db.UpdateWorkloadConfigInstanceStatus("wl-canary", "cfg-canary", "inst-a", models.PushStatusApplied, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetCanaryStatus("wl-canary", "canary-test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Counts.Applied != 1 || got.Counts.Pending != 1 || got.Status != models.CanaryStatusRunning {
		t.Fatalf("after first applied: %+v", got)
	}

	if err := db.UpdateWorkloadConfigInstanceStatus("wl-canary", "cfg-canary", "inst-b", models.PushStatusFailed, "SECRET_TOKEN=abc123", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetCanaryStatus("wl-canary", "canary-test")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.CanaryStatusStopped || got.Counts.Failed != 1 || got.Targets[1].StopReason != models.CanaryStopRemoteConfigFailed {
		t.Fatalf("after failed: %+v", got)
	}
}

func seedCanaryPersistence(t *testing.T, db interface {
	UpsertWorkload(models.Workload) error
	CreateConfig(models.Config) error
}, configID string) {
	t.Helper()
	if err := db.UpsertWorkload(models.Workload{
		ID: "wl-canary", DisplayName: "wl-canary", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{ID: configID, Name: "candidate", Content: "receivers: {}", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}
