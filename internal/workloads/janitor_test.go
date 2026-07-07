package workloads

import (
	"context"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type fakeStore struct {
	archived  []string
	purged    int64
	workloads map[string]models.Workload
}

func (f *fakeStore) ArchiveExpiredWorkloads(now time.Time) (int64, error) {
	var n int64
	for id, w := range f.workloads {
		if w.ArchivedAt == nil && w.RetentionUntil != nil && w.RetentionUntil.Before(now) {
			t := now
			w.ArchivedAt = &t
			f.workloads[id] = w
			f.archived = append(f.archived, id)
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) PurgeOldWorkloadEvents(_ time.Time) (int64, error) {
	f.purged = 1
	return 1, nil
}

func TestRunOnceArchivesExpiredAndPurgesEvents(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-time.Hour)
	f := &fakeStore{workloads: map[string]models.Workload{
		"old": {ID: "old", RetentionUntil: &past},
	}}
	j := New(f, Options{EventRetention: 24 * time.Hour})
	j.RunOnce(context.Background(), time.Now())
	if len(f.archived) != 1 || f.archived[0] != "old" {
		t.Fatalf("archived: %v", f.archived)
	}
	if f.purged != 1 {
		t.Fatalf("purged: %d", f.purged)
	}
}

func TestRunOnceSkipsWorkloadsWithFutureRetention(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(time.Hour)
	f := &fakeStore{workloads: map[string]models.Workload{
		"young": {ID: "young", RetentionUntil: &future},
	}}
	j := New(f, Options{})
	j.RunOnce(context.Background(), time.Now())
	if len(f.archived) != 0 {
		t.Fatalf("unexpected archive: %v", f.archived)
	}
}

func TestRunOnceSkipsAlreadyArchived(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-time.Hour)
	f := &fakeStore{workloads: map[string]models.Workload{
		"already": {ID: "already", RetentionUntil: &past, ArchivedAt: &past},
	}}
	j := New(f, Options{})
	j.RunOnce(context.Background(), time.Now())
	if len(f.archived) != 0 {
		t.Fatalf("already-archived re-archived: %v", f.archived)
	}
}

func TestStartStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	f := &fakeStore{workloads: map[string]models.Workload{}}
	j := New(f, Options{Interval: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { j.Start(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Start did not return after context cancel")
	}
}
