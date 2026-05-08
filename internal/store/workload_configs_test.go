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
	if history[0].Status != "pending" {
		t.Errorf("Status = %q, want pending", history[0].Status)
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
