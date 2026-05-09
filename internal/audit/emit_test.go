package audit_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type spyAuditLogger struct {
	mu     sync.Mutex
	events []ext.AuditEvent
	err    error // forced return when non-nil
}

func (s *spyAuditLogger) Log(_ context.Context, e ext.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, e)
	return nil
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

	if err := audit.Emit(ctx, spy, "config.create", "config", "cfg-123", ""); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	got := spy.snapshot()
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].UserID != "u1" || got[0].Email != "alice@example.com" {
		t.Errorf("identity = (%q, %q)", got[0].UserID, got[0].Email)
	}
}

func TestEmit_UnauthenticatedLeavesIdentityEmpty(t *testing.T) {
	spy := &spyAuditLogger{}
	if err := audit.Emit(context.Background(), spy, "auth.login.failure", "user", "", "mallory@example.com"); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	got := spy.snapshot()
	if len(got) != 1 || got[0].Detail != "mallory@example.com" {
		t.Fatalf("event = %+v", got)
	}
}

func TestEmit_NilLoggerNoOps(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	if err := audit.Emit(context.Background(), nil, "noop", "noop", "", ""); err != nil {
		t.Errorf("nil logger should return nil err, got %v", err)
	}
}

func TestEmit_NopLoggerAcceptsCalls(_ *testing.T) {
	_ = audit.Emit(context.Background(), ext.NopAuditLogger{}, "noop", "noop", "", "")
}

func TestEmit_PropagatesLoggerError(t *testing.T) {
	wantErr := errors.New("audit DB down")
	spy := &spyAuditLogger{err: wantErr}

	got := audit.Emit(context.Background(), spy, "test.fail", "x", "y", "")
	if !errors.Is(got, wantErr) {
		t.Fatalf("err = %v, want %v", got, wantErr)
	}
}

func TestEmit_ConcurrentSafeWithSpy(t *testing.T) {
	spy := &spyAuditLogger{}
	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = audit.Emit(context.Background(), spy, "test.parallel", "x", "y", "")
		}()
	}
	wg.Wait()
	if got := len(spy.snapshot()); got != n {
		t.Fatalf("len = %d, want %d", got, n)
	}
}
