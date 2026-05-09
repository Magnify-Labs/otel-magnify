package audit_test

import (
	"context"
	"sync"
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// spyAuditLogger records every event for assertion. The mutex covers the
// Log path so concurrent emissions in tests don't race.
type spyAuditLogger struct {
	mu     sync.Mutex
	events []ext.AuditEvent
}

func (s *spyAuditLogger) Log(_ context.Context, e ext.AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *spyAuditLogger) snapshot() []ext.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ext.AuditEvent, len(s.events))
	copy(out, s.events)
	return out
}

func TestEmit_PullsUserFromContext(t *testing.T) {
	spy := &spyAuditLogger{}
	ctx := ext.ContextWithUserInfo(context.Background(), &ext.UserInfo{
		UserID: "u1",
		Email:  "alice@example.com",
	})

	audit.Emit(ctx, spy, "config.create", "config", "cfg-123", "")

	got := spy.snapshot()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].UserID != "u1" || got[0].Email != "alice@example.com" {
		t.Errorf("identity = (%q, %q), want (u1, alice@example.com)", got[0].UserID, got[0].Email)
	}
	if got[0].Action != "config.create" || got[0].Resource != "config" || got[0].ResourceID != "cfg-123" {
		t.Errorf("action/resource/id mismatch: %+v", got[0])
	}
	if got[0].Detail != "" {
		t.Errorf("Detail = %q, want empty", got[0].Detail)
	}
}

// Unauthenticated paths (e.g. failed login attempts) call Emit with a context
// that has no UserInfo. UserID/Email stay empty; the caller passes the
// attempted email through Detail.
func TestEmit_UnauthenticatedLeavesIdentityEmpty(t *testing.T) {
	spy := &spyAuditLogger{}

	audit.Emit(context.Background(), spy, "auth.login.failure", "user", "", "mallory@example.com")

	got := spy.snapshot()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].UserID != "" || got[0].Email != "" {
		t.Errorf("identity = (%q, %q), want empty", got[0].UserID, got[0].Email)
	}
	if got[0].Detail != "mallory@example.com" {
		t.Errorf("Detail = %q, want mallory@example.com", got[0].Detail)
	}
}

// A nil logger must not panic — community binaries that skip
// WithAuditLogger should still be safe to call audit.Emit from anywhere.
func TestEmit_NilLoggerNoOps(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	audit.Emit(context.Background(), nil, "noop", "noop", "", "")
}

// NopAuditLogger is the default community sink — events go nowhere but
// must not panic and must accept any context.
func TestEmit_NopLoggerAcceptsCalls(_ *testing.T) {
	audit.Emit(context.Background(), ext.NopAuditLogger{}, "noop", "noop", "", "")
}

func TestEmit_ConcurrentSafeWithSpy(t *testing.T) {
	spy := &spyAuditLogger{}
	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			audit.Emit(context.Background(), spy, "test.parallel", "x", "y", "")
		}()
	}
	wg.Wait()
	if got := len(spy.snapshot()); got != n {
		t.Fatalf("len = %d, want %d", got, n)
	}
}
