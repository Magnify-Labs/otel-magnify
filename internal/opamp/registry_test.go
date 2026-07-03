package opamp

import (
	"sync"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestRegistryBindFreshReturnsTrueOnceThenFalse(t *testing.T) {
	r := NewInstanceRegistry()
	isFresh := r.BindInstance("uid-a", "wl-1", Instance{PodName: "pa", Version: "1.0", Healthy: true, ConnectedAt: time.Now().UTC()})
	if !isFresh {
		t.Fatalf("first bind should be fresh")
	}
	if wl, ok := r.LookupWorkload("uid-a"); !ok || wl != "wl-1" {
		t.Fatalf("lookup: %q %v", wl, ok)
	}
	// Re-bind same uid → not fresh
	isFresh = r.BindInstance("uid-a", "wl-1", Instance{PodName: "pa", Version: "1.0"})
	if isFresh {
		t.Fatalf("re-bind should not be fresh")
	}
}

func TestRegistryUnbindReturnsWorkloadAndShrinksCount(t *testing.T) {
	r := NewInstanceRegistry()
	r.BindInstance("uid-a", "wl-1", Instance{})
	r.BindInstance("uid-b", "wl-1", Instance{})
	if r.Count("wl-1") != 2 {
		t.Fatalf("count: %d", r.Count("wl-1"))
	}
	wl := r.UnbindInstance("uid-a")
	if wl != "wl-1" {
		t.Fatalf("unbind returned %q", wl)
	}
	if r.Count("wl-1") != 1 {
		t.Fatalf("count after unbind: %d", r.Count("wl-1"))
	}
	r.UnbindInstance("uid-b")
	if r.Count("wl-1") != 0 {
		t.Fatalf("count: %d", r.Count("wl-1"))
	}
	// Unbind of unknown uid returns empty string
	if r.UnbindInstance("uid-missing") != "" {
		t.Fatal("unknown unbind should return empty string")
	}
}

func TestRegistryInstancesSnapshot(t *testing.T) {
	r := NewInstanceRegistry()
	r.BindInstance("uid-a", "wl-1", Instance{PodName: "pa", Version: "1.0"})
	r.BindInstance("uid-b", "wl-1", Instance{PodName: "pb", Version: "1.1"})
	snap := r.Instances("wl-1")
	if len(snap) != 2 {
		t.Fatalf("snap: %d", len(snap))
	}
}

func TestRegistryInstancesSnapshotPreservesPerInstanceTopology(t *testing.T) {
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	r := NewInstanceRegistry()
	r.BindInstance("uid-healthy", "wl-1", Instance{
		PodName:             "collector-0",
		Version:             "0.98.0",
		EffectiveConfigHash: "hash-active",
		Healthy:             true,
		AcceptsRemoteConfig: true,
		RemoteConfigStatus:  &models.RemoteConfigStatus{Status: models.PushStatusApplied, ConfigHash: "hash-active", UpdatedAt: now},
	})
	r.BindInstance("uid-degraded", "wl-1", Instance{
		PodName:             "collector-1",
		Version:             "0.99.0",
		EffectiveConfigHash: "hash-canary",
		Healthy:             false,
		AcceptsRemoteConfig: true,
		RemoteConfigStatus:  &models.RemoteConfigStatus{Status: models.PushStatusFailed, ConfigHash: "hash-canary", ErrorMessage: "validation failed", UpdatedAt: now.Add(time.Minute)},
	})

	byUID := map[string]Instance{}
	for _, inst := range r.Instances("wl-1") {
		byUID[inst.InstanceUID] = inst
	}
	if len(byUID) != 2 {
		t.Fatalf("instances len = %d, want 2: %+v", len(byUID), byUID)
	}
	healthy := byUID["uid-healthy"]
	if healthy.PodName != "collector-0" || healthy.Version != "0.98.0" || healthy.EffectiveConfigHash != "hash-active" || !healthy.Healthy {
		t.Fatalf("healthy instance topology collapsed or corrupted: %+v", healthy)
	}
	if healthy.RemoteConfigStatus == nil || healthy.RemoteConfigStatus.Status != models.PushStatusApplied || healthy.RemoteConfigStatus.ConfigHash != "hash-active" {
		t.Fatalf("healthy remote config status = %+v", healthy.RemoteConfigStatus)
	}
	degraded := byUID["uid-degraded"]
	if degraded.PodName != "collector-1" || degraded.Version != "0.99.0" || degraded.EffectiveConfigHash != "hash-canary" || degraded.Healthy {
		t.Fatalf("degraded instance topology collapsed or corrupted: %+v", degraded)
	}
	if degraded.RemoteConfigStatus == nil || degraded.RemoteConfigStatus.Status != models.PushStatusFailed || degraded.RemoteConfigStatus.ConfigHash != "hash-canary" {
		t.Fatalf("degraded remote config status = %+v", degraded.RemoteConfigStatus)
	}
}

func TestRegistryUpdateInstance(t *testing.T) {
	r := NewInstanceRegistry()
	r.BindInstance("uid-a", "wl-1", Instance{Version: "1.0", Healthy: true})
	ok := r.UpdateInstance("uid-a", func(i *Instance) { i.Healthy = false })
	if !ok {
		t.Fatal("UpdateInstance should return true for known uid")
	}
	snap := r.Instances("wl-1")
	if len(snap) != 1 || snap[0].Healthy {
		t.Fatalf("expected unhealthy, got %+v", snap)
	}
	if ok := r.UpdateInstance("uid-missing", func(_ *Instance) {}); ok {
		t.Fatal("UpdateInstance should return false for unknown uid")
	}
}

func TestRegistryAggregatedStatus(t *testing.T) {
	r := NewInstanceRegistry()
	if s := r.AggregatedStatus("wl-1"); s != "disconnected" {
		t.Fatalf("empty workload: got %q, want disconnected", s)
	}
	r.BindInstance("uid-a", "wl-1", Instance{Healthy: true})
	if s := r.AggregatedStatus("wl-1"); s != "connected" {
		t.Fatalf("one healthy: got %q, want connected", s)
	}
	r.BindInstance("uid-b", "wl-1", Instance{Healthy: false})
	if s := r.AggregatedStatus("wl-1"); s != "degraded" {
		t.Fatalf("one unhealthy among two: got %q, want degraded", s)
	}
}

func TestRegistryPreviousVersion(t *testing.T) {
	r := NewInstanceRegistry()
	_, ok := r.PreviousVersion("uid-a")
	if ok {
		t.Fatal("expected not-found for unknown uid")
	}
	r.BindInstance("uid-a", "wl-1", Instance{Version: "1.0"})
	prev, ok := r.PreviousVersion("uid-a")
	if !ok || prev != "1.0" {
		t.Fatalf("PreviousVersion = %q, %v", prev, ok)
	}
}

func TestRegistryConcurrentBindUnbind(_ *testing.T) {
	r := NewInstanceRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		uid := string(rune('a' + (i % 26)))
		go func() {
			defer wg.Done()
			r.BindInstance(uid, "wl-1", Instance{})
		}()
		go func() {
			defer wg.Done()
			_ = r.Count("wl-1")
			_ = r.Instances("wl-1")
			_, _ = r.LookupWorkload(uid)
			r.UnbindInstance(uid)
		}()
	}
	wg.Wait()
	// Just ensure no crash / race — run with -race to surface issues.
}
