package api

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// recordingAuditLogger captures every event emitted by the API so tests can
// assert that the right surfaces are wired through audit.Emit. The mutex
// covers Log so concurrent emissions in tests don't race.
type recordingAuditLogger struct {
	mu     sync.Mutex
	events []ext.AuditEvent
}

func (r *recordingAuditLogger) Log(_ context.Context, e ext.AuditEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingAuditLogger) snapshot() []ext.AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ext.AuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

// newAuditTestAPI mirrors newTestAPI but threads in a recording audit logger
// so tests can assert the audit emission surface end-to-end.
func newAuditTestAPI(t *testing.T) (ext.Store, http.Handler, *fakeOpAMPPusher, *recordingAuditLogger) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	a := auth.New("test-secret-key-at-least-32-bytes!")
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	fake := &fakeOpAMPPusher{instances: make(map[string][]opamp.Instance)}
	rec := &recordingAuditLogger{}
	router := NewRouter(db, a, hub, fake, rec, "", nil, nil, 30*24*time.Hour, nil, nil)
	return db, router, fake, rec
}
