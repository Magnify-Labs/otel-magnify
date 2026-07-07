package opamp

import (
	"testing"
	"time"
)

func TestGraceFiresAfterDelayIfNotCancelled(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(20 * time.Millisecond)
	fired := make(chan struct{}, 1)
	gc.Schedule("wl", func() { fired <- struct{}{} })
	select {
	case <-fired:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected grace callback to fire")
	}
}

func TestGraceCancellationPreventsFiring(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(30 * time.Millisecond)
	fired := make(chan struct{}, 1)
	gc.Schedule("wl", func() { fired <- struct{}{} })
	gc.Cancel("wl")
	select {
	case <-fired:
		t.Fatal("unexpected grace callback after cancel")
	case <-time.After(70 * time.Millisecond):
	}
}

func TestGraceRescheduleReplacesExisting(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(30 * time.Millisecond)
	fired := make(chan int, 2)
	gc.Schedule("wl", func() { fired <- 1 })
	gc.Schedule("wl", func() { fired <- 2 }) // should cancel the first
	select {
	case got := <-fired:
		if got != 2 {
			t.Fatalf("expected 2 (second only), got %d", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected rescheduled grace callback to fire")
	}
	select {
	case got := <-fired:
		t.Fatalf("expected 2 (second only), got %d", got)
	case <-time.After(70 * time.Millisecond):
	}
}

func TestGraceCancelOfUnknownIDIsNoop(_ *testing.T) {
	gc := NewGraceController(10 * time.Millisecond)
	gc.Cancel("wl-never-scheduled") // should not panic
}

func TestGraceMultipleWorkloadsIndependent(t *testing.T) {
	t.Parallel()

	gc := NewGraceController(20 * time.Millisecond)
	a := make(chan struct{}, 1)
	b := make(chan struct{}, 1)
	gc.Schedule("wl-a", func() { a <- struct{}{} })
	gc.Schedule("wl-b", func() { b <- struct{}{} })
	gc.Cancel("wl-a")
	select {
	case <-b:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("wl-b should have fired once")
	}
	select {
	case <-a:
		t.Fatal("wl-a should have been cancelled")
	case <-time.After(50 * time.Millisecond):
	}
}
