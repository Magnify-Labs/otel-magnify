package opamp

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// fakeStore records every store call made by the Server so tests can assert
// against the sequence of mutations. Thread-safe — onMessage may call into
// the store from goroutines (auto-push).
type fakeStore struct {
	mu sync.Mutex

	workloads map[string]models.Workload
	configs   map[string]models.Config

	upsertCalls       []models.Workload
	disconnectedCalls []struct {
		id    string
		until time.Time
	}
	clearRetentionCalls []string
	events              []models.WorkloadEvent
	workloadConfigs     []models.WorkloadConfig
	statusUpdates       []struct {
		workloadID, configID, status, errorMessage string
	}
	// Prepared lastApplied return: returned verbatim by
	// GetLastAppliedWorkloadConfig regardless of arg. nil means "none".
	lastApplied *models.WorkloadConfig
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		workloads: make(map[string]models.Workload),
		configs:   make(map[string]models.Config),
	}
}

func (f *fakeStore) GetWorkload(id string) (models.Workload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.workloads[id]
	if !ok {
		return models.Workload{}, fmt.Errorf("not found: %s", id)
	}
	return w, nil
}

func (f *fakeStore) UpsertWorkload(w models.Workload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Deep-copy labels / keys to avoid observers sharing the same map.
	if w.Labels == nil {
		w.Labels = models.Labels{}
	}
	f.workloads[w.ID] = w
	f.upsertCalls = append(f.upsertCalls, w)
	return nil
}

func (f *fakeStore) MarkWorkloadDisconnected(id string, until time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnectedCalls = append(f.disconnectedCalls, struct {
		id    string
		until time.Time
	}{id, until})
	if w, ok := f.workloads[id]; ok {
		w.Status = "disconnected"
		u := until
		w.RetentionUntil = &u
		f.workloads[id] = w
	}
	return nil
}

func (f *fakeStore) ClearWorkloadRetention(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearRetentionCalls = append(f.clearRetentionCalls, id)
	if w, ok := f.workloads[id]; ok {
		w.RetentionUntil = nil
		f.workloads[id] = w
	}
	return nil
}

func (f *fakeStore) GetConfig(id string) (models.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.configs[id]
	if !ok {
		return models.Config{}, fmt.Errorf("not found: %s", id)
	}
	return c, nil
}

func (f *fakeStore) CreateConfig(c models.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs[c.ID] = c
	return nil
}

func (f *fakeStore) RecordWorkloadConfig(wc models.WorkloadConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workloadConfigs = append(f.workloadConfigs, wc)
	return nil
}

func (f *fakeStore) UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusUpdates = append(f.statusUpdates, struct {
		workloadID, configID, status, errorMessage string
	}{workloadID, configID, status, errorMessage})
	return nil
}

func (f *fakeStore) MarkWorkloadConfigSent(workloadID, configID string, sentAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.workloadConfigs) - 1; i >= 0; i-- {
		if f.workloadConfigs[i].WorkloadID == workloadID && f.workloadConfigs[i].ConfigID == configID {
			f.workloadConfigs[i].Status = models.PushStatusSent
			f.workloadConfigs[i].SentAt = &sentAt
			return nil
		}
	}
	return nil
}

func (f *fakeStore) UpdateWorkloadConfigInstanceStatus(workloadID, configID, instanceUID, status, errorMessage string, updatedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.workloadConfigs) - 1; i >= 0; i-- {
		if f.workloadConfigs[i].WorkloadID == workloadID && f.workloadConfigs[i].ConfigID == configID {
			f.workloadConfigs[i].InstanceStatuses = append(f.workloadConfigs[i].InstanceStatuses, models.WorkloadConfigInstanceStatus{InstanceUID: instanceUID, Status: status, ErrorMessage: errorMessage, UpdatedAt: updatedAt})
			f.workloadConfigs[i].HydratePushStatus(updatedAt)
			return nil
		}
	}
	return nil
}

func (f *fakeStore) GetLatestWorkloadConfig(workloadID string) (*models.WorkloadConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.workloadConfigs) - 1; i >= 0; i-- {
		if f.workloadConfigs[i].WorkloadID == workloadID {
			wc := f.workloadConfigs[i]
			wc.HydratePushStatus(time.Now().UTC())
			return &wc, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) GetLastAppliedWorkloadConfig(_ string) (*models.WorkloadConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastApplied, nil
}

func (f *fakeStore) InsertWorkloadEvent(e models.WorkloadEvent) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e.ID = int64(len(f.events) + 1)
	f.events = append(f.events, e)
	return e.ID, nil
}

// waitFor polls cond every ~2ms up to timeout, failing the test if cond
// never returns true. Used by tests that depend on the grace-timer
// goroutine firing (or, crucially, NOT firing within the grace window).
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

// opts20 is a compact "short-grace" Options used across these tests so
// grace-window behavior exercises in hundreds of ms rather than minutes.
func opts20() Options {
	return Options{DisconnectGrace: 20 * time.Millisecond, RetentionDuration: time.Hour}
}

func stringVal(v string) *protobufs.AnyValue {
	return &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: v}}
}

func TestOnMessageUpsertsWorkloadWithFingerprint(t *testing.T) {
	store := newFakeStore()
	n := &fakeNotifier{}
	srv := New(store, n, opts20())

	uid := make([]byte, 16)
	uid[0] = 0x42

	msg := &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: stringVal("payments")},
				{Key: "service.version", Value: stringVal("1.2.3")},
			},
			NonIdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "k8s.namespace.name", Value: stringVal("prod")},
				{Key: "k8s.deployment.name", Value: stringVal("payments-api")},
				{Key: "k8s.pod.name", Value: stringVal("payments-api-abc123")},
			},
		},
	}
	srv.onMessage(context.TODO(), nil, msg)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.upsertCalls) == 0 {
		t.Fatal("expected UpsertWorkload to be called")
	}
	got := store.upsertCalls[len(store.upsertCalls)-1]
	if got.FingerprintSource != "k8s" {
		t.Errorf("FingerprintSource = %q, want %q", got.FingerprintSource, "k8s")
	}
	if got.FingerprintKeys["kind"] != "deployment" || got.FingerprintKeys["name"] != "payments-api" {
		t.Errorf("FingerprintKeys = %+v, want deployment/payments-api", got.FingerprintKeys)
	}
	if got.DisplayName != "payments" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "payments")
	}
	if got.Type != "sdk" {
		t.Errorf("Type = %q, want sdk", got.Type)
	}
	if got.Labels["k8s.namespace.name"] != "prod" {
		t.Errorf("Labels[namespace] = %q, want prod", got.Labels["k8s.namespace.name"])
	}
	// service.name / service.version must NOT be duplicated into labels.
	if _, ok := got.Labels["service.name"]; ok {
		t.Error("service.name should not appear in labels (projected to DisplayName)")
	}
}

func TestConnectedEventEmittedOnFreshBind(t *testing.T) {
	store := newFakeStore()
	n := &fakeNotifier{}
	srv := New(store, n, opts20())

	uid := make([]byte, 16)
	uid[0] = 0x43

	srv.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: stringVal("svc")},
				{Key: "service.version", Value: stringVal("1.0.0")},
			},
			NonIdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "k8s.pod.name", Value: stringVal("svc-pod-xyz")},
			},
		},
	})

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(store.events), store.events)
	}
	e := store.events[0]
	if e.EventType != "connected" {
		t.Errorf("EventType = %q, want connected", e.EventType)
	}
	if e.InstanceUID != hex.EncodeToString(uid) {
		t.Errorf("InstanceUID = %q, want %q", e.InstanceUID, hex.EncodeToString(uid))
	}
	if e.PodName != "svc-pod-xyz" {
		t.Errorf("PodName = %q, want svc-pod-xyz", e.PodName)
	}
	if e.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", e.Version)
	}
	if len(n.events) != 1 || n.events[0].EventType != "connected" {
		t.Errorf("expected 1 broadcast event of type connected, got %+v", n.events)
	}
}

func TestDisconnectedEventEmittedOnClose(t *testing.T) {
	store := newFakeStore()
	n := &fakeNotifier{}
	srv := New(store, n, opts20())

	uid := make([]byte, 16)
	uid[0] = 0x44
	uidHex := hex.EncodeToString(uid)

	// Bind first via AgentDescription.
	srv.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: stringVal("svc")},
			},
			NonIdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "k8s.pod.name", Value: stringVal("svc-pod-1")},
			},
		},
	})

	// onMessage was called with a nil conn, so the connToUID map is empty.
	// Simulate the close flow by manually registering a fake conn first.
	var conn = fakeConn{}
	srv.mu.Lock()
	srv.conns[uidHex] = conn
	srv.connToUID[conn] = uidHex
	srv.mu.Unlock()

	srv.onConnectionClose(conn)

	// A disconnected event should be recorded.
	store.mu.Lock()
	var gotDisc *models.WorkloadEvent
	for i := range store.events {
		if store.events[i].EventType == "disconnected" {
			gotDisc = &store.events[i]
			break
		}
	}
	store.mu.Unlock()
	if gotDisc == nil {
		t.Fatalf("no disconnected event emitted; events=%+v", store.events)
	}
	if gotDisc.PodName != "svc-pod-1" {
		t.Errorf("PodName = %q, want svc-pod-1 (must be captured BEFORE unbind)", gotDisc.PodName)
	}
	if gotDisc.InstanceUID != uidHex {
		t.Errorf("InstanceUID = %q, want %q", gotDisc.InstanceUID, uidHex)
	}
}

func TestRollingRestartDoesNotMarkDisconnected(t *testing.T) {
	store := newFakeStore()
	n := &fakeNotifier{}
	srv := New(store, n, opts20())

	// Pod A connects.
	uidA := make([]byte, 16)
	uidA[0] = 0xA1
	uidAHex := hex.EncodeToString(uidA)
	srv.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uidA,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: stringVal("rolling")},
			},
			NonIdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "k8s.namespace.name", Value: stringVal("default")},
				{Key: "k8s.deployment.name", Value: stringVal("rolling")},
				{Key: "k8s.pod.name", Value: stringVal("rolling-a")},
			},
		},
	})

	connA := fakeConn{}
	srv.mu.Lock()
	srv.conns[uidAHex] = connA
	srv.connToUID[connA] = uidAHex
	srv.mu.Unlock()

	// Pod A disconnects. Count goes to 0 → grace timer is scheduled.
	srv.onConnectionClose(connA)

	// Well within the 20ms grace, pod B comes up on the same workload.
	time.Sleep(5 * time.Millisecond)
	uidB := make([]byte, 16)
	uidB[0] = 0xB1
	srv.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uidB,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: stringVal("rolling")},
			},
			NonIdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "k8s.namespace.name", Value: stringVal("default")},
				{Key: "k8s.deployment.name", Value: stringVal("rolling")},
				{Key: "k8s.pod.name", Value: stringVal("rolling-b")},
			},
		},
	})

	// Wait past the grace window so the timer would have fired if not cancelled.
	time.Sleep(50 * time.Millisecond)

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.disconnectedCalls) != 0 {
		t.Fatalf("MarkWorkloadDisconnected called during rolling restart: %+v", store.disconnectedCalls)
	}
}

func TestAutoPushWhenConfigHashDiverges(t *testing.T) {
	store := newFakeStore()
	n := &fakeNotifier{}
	srv := New(store, n, opts20())

	// Seed a config and a workload that points at it.
	cfgID := "thecfg00"
	cfgHash := "targethash"
	_ = store.CreateConfig(models.Config{ID: cfgID, Name: "pinned", Content: "keeper-yaml", CreatedAt: time.Now().UTC(), CreatedBy: "u"})

	// Compute the workload ID the message will yield so we can seed the
	// matching row BEFORE onMessage runs its own upsert.
	uid := make([]byte, 16)
	uid[0] = 0x91
	attrs := map[string]string{"service.name": "svc"}
	wlID := Fingerprint(attrs, hex.EncodeToString(uid)).ID

	acID := cfgID
	_ = store.UpsertWorkload(models.Workload{
		ID: wlID, Type: "sdk", Status: "connected", LastSeenAt: time.Now().UTC(),
		Labels: models.Labels{}, ActiveConfigID: &acID, ActiveConfigHash: cfgHash,
	})

	// Capture push invocations.
	type pushCall struct {
		workloadID, instance string
		yaml                 []byte
	}
	var pushes []pushCall
	var pushMu sync.Mutex
	srv.pushFn = func(workloadID string, yaml []byte, instance string) error {
		pushMu.Lock()
		defer pushMu.Unlock()
		pushes = append(pushes, pushCall{workloadID, instance, yaml})
		return nil
	}

	// Agent reports a DIFFERENT effective hash → auto-push triggers.
	divergent, _ := hex.DecodeString("deadbeef")
	srv.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: stringVal("svc")},
			},
		},
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: divergent,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
		},
	})

	// triggerAutoPush fires in a goroutine; wait for it.
	waitFor(t, 500*time.Millisecond, func() bool {
		pushMu.Lock()
		defer pushMu.Unlock()
		return len(pushes) >= 1
	})

	pushMu.Lock()
	defer pushMu.Unlock()
	if len(pushes) != 1 {
		t.Fatalf("expected exactly 1 auto-push, got %d: %+v", len(pushes), pushes)
	}
	got := pushes[0]
	if got.workloadID != wlID {
		t.Errorf("workloadID = %q, want %q", got.workloadID, wlID)
	}
	if got.instance != hex.EncodeToString(uid) {
		t.Errorf("instance = %q, want %q", got.instance, hex.EncodeToString(uid))
	}
	if string(got.yaml) != "keeper-yaml" {
		t.Errorf("yaml = %q, want keeper-yaml", string(got.yaml))
	}
}
