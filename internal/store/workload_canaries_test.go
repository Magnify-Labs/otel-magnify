package store

import (
	"fmt"
	"sync"
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

func TestUpdateCanaryTargetStatusConcurrentUpdatesDoNotLoseTargets(t *testing.T) {
	const targetCount = 16

	poolConfig := testPoolConfig()
	poolConfig.MaxOpenConns = 20
	db := newTestDBWithPoolConfig(t, poolConfig)
	seedCanaryPersistence(t, db, "cfg-canary-concurrent")

	now := time.Now().UTC()
	targets := make([]models.CanaryTarget, 0, targetCount)
	instanceUIDs := make([]string, 0, targetCount)
	for i := 0; i < targetCount; i++ {
		instanceUID := fmt.Sprintf("instance-%02d", i)
		instanceUIDs = append(instanceUIDs, instanceUID)
		targets = append(targets, models.CanaryTarget{
			InstanceUID: instanceUID,
			Status:      models.PushStatusSent,
			UpdatedAt:   now,
		})
	}
	if err := db.CreateCanaryStatus(models.CanaryStatus{
		ID:         "canary-concurrent",
		WorkloadID: "wl-canary",
		ConfigHash: "cfg-canary-concurrent",
		Status:     models.CanaryStatusRunning,
		Selection:  models.CanarySelection{Strategy: "count", Count: targetCount},
		Targets:    targets,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("CreateCanaryStatus: %v", err)
	}

	controlTx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin control transaction: %v", err)
	}
	t.Cleanup(func() { _ = controlTx.Rollback() })
	var canaryID string
	if err := controlTx.QueryRow(`
		SELECT id
		FROM workload_config_canaries
		WHERE workload_id = ? AND config_id = ? AND status IN (?, ?)
		ORDER BY id
		FOR UPDATE`,
		"wl-canary", "cfg-canary-concurrent", models.CanaryStatusRunning, models.CanaryStatusSucceeded,
	).Scan(&canaryID); err != nil {
		t.Fatalf("lock workload config canary: %v", err)
	}

	start := make(chan struct{})
	updateErrs := make(chan error, targetCount)
	var ready sync.WaitGroup
	var updates sync.WaitGroup
	ready.Add(targetCount)
	for _, instanceUID := range instanceUIDs {
		updates.Add(1)
		go func() {
			defer updates.Done()
			ready.Done()
			<-start
			if err := db.UpdateCanaryTargetStatus(
				"wl-canary",
				"cfg-canary-concurrent",
				instanceUID,
				models.PushStatusApplied,
				now.Add(time.Second),
			); err != nil {
				updateErrs <- fmt.Errorf("update %s: %w", instanceUID, err)
			}
		}()
	}
	ready.Wait()
	close(start)

	waitForPostgresTableLockWaiters(t, db, "workload_config_canaries")

	if err := controlTx.Commit(); err != nil {
		t.Fatalf("release control transaction: %v", err)
	}
	updates.Wait()
	close(updateErrs)
	for err := range updateErrs {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}

	got, err := db.GetCanaryStatus("wl-canary", "canary-concurrent")
	if err != nil {
		t.Fatalf("GetCanaryStatus: %v", err)
	}
	if got == nil {
		t.Fatal("GetCanaryStatus returned nil")
	}
	if got.Counts.Applied != 16 || got.Status != models.CanaryStatusSucceeded {
		t.Fatalf("canary result = %+v", got)
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
