package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestRecordWorkloadConfig(t *testing.T) {
	db := newTestDB(t)

	if err := db.CreateConfig(models.Config{
		ID: "cfg-1", Name: "test", Content: "yaml",
		CreatedAt: time.Now().UTC(), CreatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	seedWorkload(t, db, "a1")

	if err := db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "a1", ConfigID: "cfg-1", Status: "pending",
	}); err != nil {
		t.Fatalf("RecordWorkloadConfig: %v", err)
	}

	history, err := db.GetWorkloadConfigHistory("a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("len = %d, want 1", len(history))
	}
	if history[0].Status != models.PushStatusSubmitted {
		t.Errorf("Status = %q, want submitted", history[0].Status)
	}
	if history[0].WorkloadID != "a1" {
		t.Errorf("WorkloadID = %q, want a1", history[0].WorkloadID)
	}
}

func TestRecordWorkloadConfig_WithPushedByAndError(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	seedConfig(t, db, "c1", "receivers: {}")

	if err := db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: "a1",
		ConfigID:   "c1",
		Status:     "pending",
		PushedBy:   "admin@magnify.dev",
	}); err != nil {
		t.Fatal(err)
	}

	hist, err := db.GetWorkloadConfigHistory("a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 1 || hist[0].PushedBy != "admin@magnify.dev" {
		t.Fatalf("unexpected history: %+v", hist)
	}
	if hist[0].Content != "receivers: {}" {
		t.Fatalf("expected JOINed content, got %q", hist[0].Content)
	}
}

func TestUpdateWorkloadConfigStatus_SetsFailedWithError(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	seedConfig(t, db, "c1", "x")
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "a1", ConfigID: "c1", Status: "pending"})

	if err := db.UpdateWorkloadConfigStatus("a1", "c1", "failed", "unknown exporter 'xyz'"); err != nil {
		t.Fatal(err)
	}

	hist, _ := db.GetWorkloadConfigHistory("a1")
	if hist[0].Status != "failed" || hist[0].ErrorMessage != "unknown exporter 'xyz'" {
		t.Fatalf("status/error not updated: %+v", hist[0])
	}
}

func TestUpdateWorkloadConfigStatus_AcceptsApplying(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	seedConfig(t, db, "c1", "x")
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "a1", ConfigID: "c1", Status: "pending"})

	if err := db.UpdateWorkloadConfigStatus("a1", "c1", "applying", ""); err != nil {
		t.Fatalf("applying should be a valid status: %v", err)
	}

	hist, _ := db.GetWorkloadConfigHistory("a1")
	if hist[0].Status != "applying" {
		t.Fatalf("expected applying, got %q", hist[0].Status)
	}
}

func TestGetLastAppliedWorkloadConfig(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	seedConfig(t, db, "cA", "A")
	seedConfig(t, db, "cB", "B")

	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "a1", ConfigID: "cA", Status: "applied"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "a1", ConfigID: "cB", Status: "failed"})

	wc, err := db.GetLastAppliedWorkloadConfig("a1")
	if err != nil {
		t.Fatal(err)
	}
	if wc == nil || wc.ConfigID != "cA" || wc.Content != "A" {
		t.Fatalf("expected cA content A, got %+v", wc)
	}
}

func TestGetLastAppliedWorkloadConfig_None(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	wc, err := db.GetLastAppliedWorkloadConfig("a1")
	if err != nil {
		t.Fatal(err)
	}
	if wc != nil {
		t.Fatalf("expected nil, got %+v", wc)
	}
}

func TestSetWorkloadConfigLabel_SetAndClear(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	seedConfig(t, db, "h1", "yaml")
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "a1", ConfigID: "h1", Status: "applied"})

	if err := db.SetWorkloadConfigLabel("a1", "h1", "stable-2026-05"); err != nil {
		t.Fatalf("SetWorkloadConfigLabel: %v", err)
	}

	wc, err := db.GetWorkloadConfigByHash("a1", "h1")
	if err != nil || wc == nil {
		t.Fatalf("GetWorkloadConfigByHash: %v / %+v", err, wc)
	}
	if wc.Label == nil || *wc.Label != "stable-2026-05" {
		t.Fatalf("Label = %v, want stable-2026-05", wc.Label)
	}

	// Clear the label by passing the empty string.
	if err := db.SetWorkloadConfigLabel("a1", "h1", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	wc, _ = db.GetWorkloadConfigByHash("a1", "h1")
	if wc.Label != nil {
		t.Fatalf("Label after clear = %v, want nil", wc.Label)
	}
}

func TestSetWorkloadConfigLabel_AppliesToAllRowsForHash(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")
	seedConfig(t, db, "h1", "yaml")
	// Same hash pushed twice — both rows should pick up the label since it
	// describes the revision content, not a specific push instance.
	t0 := time.Now().UTC()
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "a1", ConfigID: "h1", AppliedAt: t0, Status: "applied"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "a1", ConfigID: "h1", AppliedAt: t0.Add(time.Second), Status: "applied"})

	if err := db.SetWorkloadConfigLabel("a1", "h1", "blessed"); err != nil {
		t.Fatal(err)
	}

	hist, _ := db.GetWorkloadConfigHistory("a1")
	if len(hist) != 2 {
		t.Fatalf("len = %d, want 2", len(hist))
	}
	for i, row := range hist {
		if row.Label == nil || *row.Label != "blessed" {
			t.Errorf("row %d Label = %v, want blessed", i, row.Label)
		}
	}
}

func TestSetWorkloadConfigLabel_UnknownHashReturnsErrNoRows(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")

	err := db.SetWorkloadConfigLabel("a1", "ghost", "x")
	if err == nil {
		t.Fatal("expected error for unknown hash")
	}
	// We deliberately surface sql.ErrNoRows so the API can map it to 404
	// without needing custom error sentinels in the store package.
	if err.Error() != "sql: no rows in result set" {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestGetWorkloadConfigByHash_None(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "a1")

	wc, err := db.GetWorkloadConfigByHash("a1", "ghost")
	if err != nil {
		t.Fatal(err)
	}
	if wc != nil {
		t.Fatalf("expected nil, got %+v", wc)
	}
}

func TestWorkloadConfigPushStatusPersistsTimelineAndInstances(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w1")
	seedConfig(t, db, "feedface", "x")
	now := time.Date(2026, 6, 30, 10, 12, 0, 0, time.UTC)

	if err := db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID:       "w1",
		ConfigID:         "feedface",
		Status:           "submitted",
		AppliedAt:        now,
		SubmittedAt:      now,
		PushID:           "push-1",
		InstanceStatuses: []models.WorkloadConfigInstanceStatus{{InstanceUID: "i1", PodName: "pod-a", Status: "sent", UpdatedAt: now}},
	}); err != nil {
		t.Fatal(err)
	}
	sentAt := now.Add(time.Second)
	if err := db.MarkWorkloadConfigSent("w1", "feedface", sentAt); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateWorkloadConfigInstanceStatus("w1", "feedface", "i1", "applied", "", sentAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	wc, err := db.GetLatestWorkloadConfig("w1")
	if err != nil {
		t.Fatal(err)
	}
	if wc == nil || wc.PushID != "push-1" || wc.SentAt == nil || wc.Status != "applied" {
		t.Fatalf("unexpected latest push: %+v", wc)
	}
	if wc.TargetCount != 1 || wc.AppliedCount != 1 || len(wc.Timeline) < 3 {
		t.Fatalf("hydrated status incomplete: %+v", wc)
	}
}

func TestSetWorkloadKnownGood_HappyPathReplaceAndIdempotent(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w1")
	seedConfig(t, db, "good-a", "receivers:\n  otlp: {}")
	seedConfig(t, db, "good-b", "receivers:\n  otlp: {}\nprocessors: {}")
	t0 := time.Now().UTC().Add(-2 * time.Hour)
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "good-a", AppliedAt: t0, Status: "applied"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "good-b", AppliedAt: t0.Add(time.Hour), Status: "applied"})

	kg, res, err := db.SetWorkloadKnownGood("w1", "good-a", "admin@magnify.dev", "initial baseline")
	if err != nil {
		t.Fatalf("SetWorkloadKnownGood first: %v", err)
	}
	if !res.Changed || res.ReplacedConfigID != "" || kg.ConfigID != "good-a" || kg.MarkedBy != "admin@magnify.dev" {
		t.Fatalf("first mark result = %+v / %+v", res, kg)
	}
	markedAt := kg.MarkedAt

	again, res, err := db.SetWorkloadKnownGood("w1", "good-a", "admin@magnify.dev", "retry")
	if err != nil {
		t.Fatalf("SetWorkloadKnownGood retry: %v", err)
	}
	if res.Changed || again.MarkedAt != markedAt {
		t.Fatalf("retry should be unchanged and preserve marked_at: %+v / %+v", res, again)
	}

	repl, res, err := db.SetWorkloadKnownGood("w1", "good-b", "admin@magnify.dev", "new baseline")
	if err != nil {
		t.Fatalf("SetWorkloadKnownGood replace: %v", err)
	}
	if !res.Changed || res.ReplacedConfigID != "good-a" || repl.ConfigID != "good-b" || repl.ReplaceReason != "new baseline" {
		t.Fatalf("replace result = %+v / %+v", res, repl)
	}
}

func TestSetWorkloadKnownGood_RejectsMissingNonAppliedAndEmptyContent(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w1")
	seedConfig(t, db, "pending", "receivers: {}")
	seedConfig(t, db, "empty", "")
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "pending", Status: "pending"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "empty", Status: "applied"})

	if _, _, err := db.SetWorkloadKnownGood("w1", "missing", "u", ""); err == nil {
		t.Fatal("expected error for missing history hash")
	}
	if _, _, err := db.SetWorkloadKnownGood("w1", "pending", "u", ""); err == nil {
		t.Fatal("expected error for non-applied hash")
	}
	if _, _, err := db.SetWorkloadKnownGood("w1", "empty", "u", ""); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestKnownGoodDoesNotMigrateFromLegacyLabelsAndProtectsContent(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w1")
	seedConfig(t, db, "legacy", "yaml")
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "legacy", Status: "applied"})
	_ = db.SetWorkloadConfigLabel("w1", "legacy", "known-good")

	kg, err := db.GetWorkloadKnownGood("w1")
	if err != nil {
		t.Fatal(err)
	}
	if kg != nil {
		t.Fatalf("legacy label must not become known-good: %+v", kg)
	}

	if _, _, err := db.SetWorkloadKnownGood("w1", "legacy", "u", "explicit"); err != nil {
		t.Fatal(err)
	}
	protected, err := db.IsConfigKnownGoodProtected("legacy")
	if err != nil || !protected {
		t.Fatalf("protected = %v, err=%v", protected, err)
	}
	if _, err := db.Exec(`DELETE FROM configs WHERE id = ?`, "legacy"); err == nil {
		t.Fatal("expected FK to protect known-good config content")
	}
}

func TestGetRollbackTargetPrefersKnownGoodThenPrevious(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w1")
	seedConfig(t, db, "old", "old-yaml")
	seedConfig(t, db, "current", "current-yaml")
	t0 := time.Now().UTC().Add(-2 * time.Hour)
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "old", AppliedAt: t0, Status: "applied"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "current", AppliedAt: t0.Add(time.Hour), Status: "applied"})

	target, err := db.GetRollbackTarget("w1", "current")
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.Kind != "previous" || target.Config.ConfigID != "old" {
		t.Fatalf("fallback target = %+v", target)
	}
	if _, _, err := db.SetWorkloadKnownGood("w1", "old", "u", ""); err != nil {
		t.Fatal(err)
	}
	target, err = db.GetRollbackTarget("w1", "current")
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.Kind != "last_known_good" || target.Config.ConfigID != "old" {
		t.Fatalf("known-good target = %+v", target)
	}
}

func TestGetWorkloadConfigHistoryExposesStateLabels(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w1")
	active := "current"
	wl, _ := db.GetWorkload("w1")
	wl.ActiveConfigHash = active
	_ = db.UpsertWorkload(wl)
	seedConfig(t, db, "known", "known-yaml")
	seedConfig(t, db, active, "current-yaml")
	seedConfig(t, db, "failed", "failed-yaml")
	t0 := time.Now().UTC().Add(-3 * time.Hour)
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "known", AppliedAt: t0, Status: "applied"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: active, AppliedAt: t0.Add(time.Hour), Status: "applied"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w1", ConfigID: "failed", AppliedAt: t0.Add(2 * time.Hour), Status: "failed", ErrorMessage: "boom"})
	_, _, _ = db.SetWorkloadKnownGood("w1", "known", "u", "")

	history, err := db.GetWorkloadConfigHistory("w1")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]models.WorkloadConfig{}
	for _, row := range history {
		byID[row.ConfigID] = row
	}
	if !byID[active].IsCurrent {
		t.Fatalf("current row not labeled: %+v", byID[active])
	}
	if !byID["known"].IsLastKnownGood || !byID["known"].IsPrevious {
		t.Fatalf("known row labels = %+v", byID["known"])
	}
	if !byID["failed"].IsFailedCandidate || !byID["failed"].ContentAvailable {
		t.Fatalf("failed row labels = %+v", byID["failed"])
	}
}
