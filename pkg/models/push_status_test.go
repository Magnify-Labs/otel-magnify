package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWorkloadConfigHydratePushStatus_PartialRolloutTimesOutSilentRequiredTargets(t *testing.T) {
	submitted := time.Date(2026, 6, 30, 10, 12, 0, 0, time.UTC)
	wc := WorkloadConfig{
		WorkloadID:           "w1",
		ConfigID:             "feedface",
		Status:               "sent",
		SubmittedAt:          submitted,
		SentAt:               ptrTime(submitted.Add(time.Second)),
		OpAMPStatusTimeoutAt: ptrTime(submitted.Add(30 * time.Second)),
		InstanceStatuses: []WorkloadConfigInstanceStatus{
			{InstanceUID: "applied", Required: true, Status: "applied", UpdatedAt: submitted.Add(5 * time.Second)},
			{InstanceUID: "silent", Required: true, Status: "sent", UpdatedAt: submitted.Add(time.Second)},
		},
	}

	wc.HydratePushStatus(submitted.Add(31 * time.Second))

	if !wc.TimedOutWaitingForOpAMPStatus || wc.TimeoutMessage != OpAMPStatusTimeoutMessage {
		t.Fatalf("expected aggregate timeout flag/message, got timeout=%v message=%q", wc.TimedOutWaitingForOpAMPStatus, wc.TimeoutMessage)
	}
	if wc.Status != PushStatusApplying {
		t.Fatalf("status = %q, want applying while one target applied and one required target is timed out", wc.Status)
	}
	if wc.TargetCount != 2 || wc.AppliedCount != 1 || wc.PendingCount != 1 {
		t.Fatalf("counts = target:%d applied:%d pending:%d", wc.TargetCount, wc.AppliedCount, wc.PendingCount)
	}
	if wc.InstanceStatuses[1].Status != InstanceStatusNoStatus {
		t.Fatalf("silent required instance status = %q, want no_status", wc.InstanceStatuses[1].Status)
	}
	b, err := json.Marshal(wc)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["timed_out_count"] != float64(1) || payload["no_status_count"] != float64(1) {
		t.Fatalf("payload counts = timed_out:%v no_status:%v, want 1/1; json=%s", payload["timed_out_count"], payload["no_status_count"], string(b))
	}
	instances := payload["instance_statuses"].([]any)
	silent := instances[1].(map[string]any)
	if silent["timed_out"] != true {
		t.Fatalf("silent instance timed_out = %v, want true; json=%s", silent["timed_out"], string(b))
	}
	if !timelineContainsTimedOutNoStatus(wc.Timeline) {
		t.Fatalf("timeline missing timed-out no_status entry: %+v", wc.Timeline)
	}
}

func TestWorkloadConfigHydratePushStatus_RedactsRemoteErrorSamplesEverywhere(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 12, 0, 0, time.UTC)
	raw := "collector failed: exporters.otlp.headers.authorization=Bearer SECRET_TOKEN endpoint=https://tenant-a.internal:4317"
	wc := WorkloadConfig{
		WorkloadID:   "w1",
		ConfigID:     "deadbeef",
		Status:       "failed",
		ErrorMessage: raw,
		SubmittedAt:  now,
		AppliedAt:    now,
		InstanceStatuses: []WorkloadConfigInstanceStatus{
			{InstanceUID: "i1", Required: true, Status: "failed", ErrorMessage: raw, UpdatedAt: now.Add(time.Second)},
		},
	}

	wc.HydratePushStatus(now.Add(2 * time.Second))
	b, err := json.Marshal(wc)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(b)
	for _, forbidden := range []string{"SECRET_TOKEN", "tenant-a.internal", "authorization=Bearer"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("redacted payload still contains forbidden marker %q", forbidden)
		}
	}
	if wc.ErrorMessage == "" || wc.InstanceStatuses[0].ErrorMessage == "" || wc.ErrorGroups[0].SampleMessage == "" {
		t.Fatalf("expected stable redacted samples to remain available")
	}
}

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

func timelineContainsTimedOutNoStatus(entries []WorkloadConfigTimelineEntry) bool {
	for _, entry := range entries {
		if entry.State == InstanceStatusNoStatus && entry.TimedOut {
			return true
		}
	}
	return false
}

func ptrTime(t time.Time) *time.Time { return &t }
