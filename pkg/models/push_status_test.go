package models

import (
	"testing"
	"time"
)

func TestWorkloadConfigHydratePushStatus_TimeoutLayerClearsAfterLateStatus(t *testing.T) {
	submitted := time.Date(2026, 6, 30, 10, 12, 0, 0, time.UTC)
	wc := WorkloadConfig{
		WorkloadID:           "w1",
		ConfigID:             "feedface",
		Status:               "sent",
		AppliedAt:            submitted,
		SubmittedAt:          submitted,
		SentAt:               ptrTime(submitted.Add(time.Second)),
		OpAMPStatusTimeoutAt: ptrTime(submitted.Add(30 * time.Second)),
		InstanceStatuses: []WorkloadConfigInstanceStatus{
			{InstanceUID: "i1", Status: "sent", UpdatedAt: submitted.Add(time.Second)},
		},
	}

	wc.HydratePushStatus(submitted.Add(31 * time.Second))
	if !wc.TimedOutWaitingForOpAMPStatus {
		t.Fatal("expected timeout warning when no instance reported OpAMP status after 30s")
	}
	if wc.TimeoutMessage != "No OpAMP status after 30s" {
		t.Fatalf("timeout message = %q", wc.TimeoutMessage)
	}
	if wc.Status != "sent" {
		t.Fatalf("timeout must not hide base status, got %q", wc.Status)
	}

	wc.InstanceStatuses[0].Status = "applying"
	wc.InstanceStatuses[0].UpdatedAt = submitted.Add(35 * time.Second)
	wc.HydratePushStatus(submitted.Add(36 * time.Second))
	if wc.TimedOutWaitingForOpAMPStatus {
		t.Fatal("late OpAMP status should clear timeout warning")
	}
	if wc.Status != "applying" {
		t.Fatalf("aggregate status = %q, want applying", wc.Status)
	}
}

func TestWorkloadConfigHydratePushStatus_MultiInstanceAggregation(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 12, 0, 0, time.UTC)
	wc := WorkloadConfig{
		WorkloadID:  "w1",
		ConfigID:    "feedface",
		Status:      "sent",
		SubmittedAt: now,
		InstanceStatuses: []WorkloadConfigInstanceStatus{
			{InstanceUID: "i1", Status: "applied", UpdatedAt: now},
			{InstanceUID: "i2", Status: "applying", UpdatedAt: now},
			{InstanceUID: "i3", Status: "sent", UpdatedAt: now},
		},
	}

	wc.HydratePushStatus(now)
	if wc.Status != "applying" {
		t.Fatalf("status = %q, want applying", wc.Status)
	}
	if wc.TargetCount != 3 || wc.AppliedCount != 1 || wc.FailedCount != 0 || wc.PendingCount != 2 {
		t.Fatalf("counts = target:%d applied:%d failed:%d pending:%d", wc.TargetCount, wc.AppliedCount, wc.FailedCount, wc.PendingCount)
	}

	wc.InstanceStatuses[1].Status = "applied"
	wc.InstanceStatuses[2].Status = "applied"
	wc.HydratePushStatus(now)
	if wc.Status != "applied" || wc.AppliedCount != 3 || wc.PendingCount != 0 {
		t.Fatalf("final aggregate = status:%q applied:%d pending:%d", wc.Status, wc.AppliedCount, wc.PendingCount)
	}
}

func TestWorkloadConfigHydratePushStatus_ErrorGrouping(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 12, 0, 0, time.UTC)
	wc := WorkloadConfig{
		WorkloadID: "w1",
		ConfigID:   "deadbeef",
		Status:     "failed",
		InstanceStatuses: []WorkloadConfigInstanceStatus{
			{InstanceUID: "i1", Status: "failed", ErrorMessage: "unknown exporter 'othttp'", UpdatedAt: now},
			{InstanceUID: "i2", Status: "failed", ErrorMessage: "invalid pipeline reference", UpdatedAt: now.Add(time.Second)},
			{InstanceUID: "i3", Status: "failed", ErrorCause: "permission_or_policy", ErrorMessage: "policy denied", UpdatedAt: now.Add(2 * time.Second)},
		},
	}

	wc.HydratePushStatus(now)
	if len(wc.ErrorGroups) != 2 {
		t.Fatalf("groups len = %d, want 2: %+v", len(wc.ErrorGroups), wc.ErrorGroups)
	}
	if wc.ErrorGroups[0].Cause != "collector_validation" || wc.ErrorGroups[0].Count != 2 {
		t.Fatalf("top group = %+v, want collector_validation count 2", wc.ErrorGroups[0])
	}
	if got := wc.ErrorGroups[0].AffectedInstances; len(got) != 2 || got[0] != "i1" || got[1] != "i2" {
		t.Fatalf("affected instances = %+v", got)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
