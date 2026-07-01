package opamp

import (
	"context"
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server/types"

	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type fakeNotifier struct {
	workloads []workloadBroadcast
	events    []models.WorkloadEvent
	statuses  []configStatusBroadcast
	rollbacks []rollbackBroadcast
}

type workloadBroadcast struct {
	workload  models.Workload
	connected int
	drifted   int
}

type configStatusBroadcast struct {
	workloadID string
	status     models.RemoteConfigStatus
}

type rollbackBroadcast struct {
	workloadID, fromHash, toHash, reason, targetKind string
}

func (f *fakeNotifier) BroadcastWorkloadUpdate(w models.Workload, connected, drifted int) {
	f.workloads = append(f.workloads, workloadBroadcast{w, connected, drifted})
}

func (f *fakeNotifier) BroadcastWorkloadEvent(e models.WorkloadEvent) {
	f.events = append(f.events, e)
}

func (f *fakeNotifier) BroadcastConfigStatus(workloadID string, s models.RemoteConfigStatus) {
	f.statuses = append(f.statuses, configStatusBroadcast{workloadID, s})
}

func (f *fakeNotifier) BroadcastAutoRollback(workloadID, fromHash, toHash, reason, targetKind string) {
	f.rollbacks = append(f.rollbacks, rollbackBroadcast{workloadID, fromHash, toHash, reason, targetKind})
}

func newTestServer(t *testing.T) (*Server, *store.DB, *fakeNotifier) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	n := &fakeNotifier{}
	// Short grace so tests don't wait forever for rolling-restart behavior.
	srv := New(db, n, Options{DisconnectGrace: 20 * time.Millisecond, RetentionDuration: time.Hour})
	return srv, db, n
}

// fingerprintUIDHex returns the workload ID the UID-based fingerprint would
// produce for the given instance UID. Tests that seed the workload row up
// front need this to match what onMessage computes.
func fingerprintUIDHex(uidHex string) string {
	return Fingerprint(map[string]string{}, uidHex).ID
}

func TestOnMessage_RemoteConfigStatusApplied(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xAA
	uidHex := hex.EncodeToString(uid)
	wlID := fingerprintUIDHex(uidHex)

	if err := db.UpsertWorkload(models.Workload{
		ID: wlID, Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatalf("seed workload: %v", err)
	}
	if err := db.CreateConfig(models.Config{
		ID: "deadbeef", Name: "n", Content: "x",
		CreatedAt: time.Now().UTC(), CreatedBy: "u",
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: wlID, ConfigID: "deadbeef", Status: "pending",
	}); err != nil {
		t.Fatalf("seed workload_config: %v", err)
	}

	// Bind the instance first via an AgentDescription so subsequent
	// heartbeats know which workload to resolve to.
	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-contrib"}}},
			},
		},
	})

	hashBytes, _ := hex.DecodeString("deadbeef")
	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hashBytes,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
		},
	})

	hist, _ := db.GetWorkloadConfigHistory(wlID)
	var applied bool
	for _, h := range hist {
		if h.ConfigID == "deadbeef" && h.Status == "applied" {
			applied = true
		}
	}
	if !applied {
		t.Fatalf("expected applied row, got %+v", hist)
	}
	var gotApplied bool
	for _, st := range n.statuses {
		if st.status.Status == "applied" {
			gotApplied = true
		}
	}
	if !gotApplied {
		t.Fatalf("expected applied status broadcast, got %+v", n.statuses)
	}
}

func TestOnMessage_RemoteConfigStatusFailed_AutoRollback(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xBB
	uidHex := hex.EncodeToString(uid)
	wlID := fingerprintUIDHex(uidHex)

	if err := db.UpsertWorkload(models.Workload{
		ID: wlID, Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatalf("seed workload: %v", err)
	}
	_ = db.CreateConfig(models.Config{ID: "aaaaaaaa", Name: "A", Content: "good-yaml", CreatedAt: time.Now().UTC().Add(-time.Hour), CreatedBy: "u"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: wlID, ConfigID: "aaaaaaaa", Status: "applied", AppliedAt: time.Now().UTC().Add(-time.Hour)})
	_ = db.CreateConfig(models.Config{ID: "bbbbbbbb", Name: "B", Content: "bad-yaml", CreatedAt: time.Now().UTC(), CreatedBy: "u"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: wlID, ConfigID: "bbbbbbbb", Status: "pending"})

	// Bind the instance so heartbeats resolve.
	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-contrib"}}},
			},
		},
	})

	type pushArgs struct {
		workloadID, instance string
		yaml                 []byte
	}
	var pushes []pushArgs
	s.pushFn = func(workloadID string, yaml []byte, instance string) error {
		pushes = append(pushes, pushArgs{workloadID, instance, yaml})
		return nil
	}

	hashB, _ := hex.DecodeString("bbbbbbbb")
	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hashB,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
			ErrorMessage:         "unknown exporter 'othttp'",
		},
	})

	hist, _ := db.GetWorkloadConfigHistory(wlID)
	var bRow *models.WorkloadConfig
	for i := range hist {
		if hist[i].ConfigID == "bbbbbbbb" {
			bRow = &hist[i]
		}
	}
	if bRow == nil || bRow.Status != "failed" || bRow.ErrorMessage != models.SanitizeRemoteConfigErrorMessage("unknown exporter 'othttp'") {
		t.Fatalf("B row not updated to failed: %+v", bRow)
	}
	if len(pushes) != 1 || string(pushes[0].yaml) != "good-yaml" {
		t.Fatalf("expected auto-rollback to re-push A, pushes=%v", pushes)
	}
	if len(n.rollbacks) != 1 || n.rollbacks[0].toHash != "aaaaaaaa" {
		t.Fatalf("expected rollback notification, got %+v", n.rollbacks)
	}
}

func TestOnMessage_RemoteConfigStatusFailed_NoRollbackTarget(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xCC
	uidHex := hex.EncodeToString(uid)
	wlID := fingerprintUIDHex(uidHex)

	if err := db.UpsertWorkload(models.Workload{
		ID: wlID, Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatalf("seed workload: %v", err)
	}
	_ = db.CreateConfig(models.Config{ID: "cccccccc", Name: "C", Content: "bad", CreatedAt: time.Now().UTC(), CreatedBy: "u"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: wlID, ConfigID: "cccccccc", Status: "pending"})

	// Bind first.
	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-contrib"}}},
			},
		},
	})

	var pushes [][]byte
	s.pushFn = func(_ string, y []byte, _ string) error { pushes = append(pushes, y); return nil }

	hash, _ := hex.DecodeString("cccccccc")
	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hash,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
			ErrorMessage:         "boom",
		},
	})

	if len(pushes) != 0 {
		t.Fatalf("expected no rollback push, got %d", len(pushes))
	}
	if len(n.rollbacks) != 0 {
		t.Fatalf("expected no rollback notification")
	}
}

func TestOnMessage_RemoteConfigStatusFailed_RedactsErrorBeforePersistBroadcastAndRollback(t *testing.T) {
	s, db, n := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xDD
	uidHex := hex.EncodeToString(uid)
	wlID := fingerprintUIDHex(uidHex)
	raw := "collector failed: exporters.otlp.headers.authorization=Bearer SECRET_TOKEN endpoint=https://tenant-a.internal:4317"

	if err := db.UpsertWorkload(models.Workload{
		ID: wlID, Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	}); err != nil {
		t.Fatalf("seed workload: %v", err)
	}
	_ = db.CreateConfig(models.Config{ID: "aaaaaaaa", Name: "A", Content: "good-yaml", CreatedAt: time.Now().UTC().Add(-time.Hour), CreatedBy: "u"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: wlID, ConfigID: "aaaaaaaa", Status: "applied", AppliedAt: time.Now().UTC().Add(-time.Hour)})
	_ = db.CreateConfig(models.Config{ID: "dddddddd", Name: "D", Content: "bad-yaml", CreatedAt: time.Now().UTC(), CreatedBy: "u"})
	_ = db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: wlID, ConfigID: "dddddddd", Status: "pending"})

	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-contrib"}}},
			},
		},
	})
	s.pushFn = func(string, []byte, string) error { return nil }

	hash, _ := hex.DecodeString("dddddddd")
	s.onMessage(context.TODO(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		RemoteConfigStatus: &protobufs.RemoteConfigStatus{
			LastRemoteConfigHash: hash,
			Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED,
			ErrorMessage:         raw,
		},
	})

	for _, forbidden := range []string{"SECRET_TOKEN", "tenant-a.internal", "authorization=Bearer"} {
		assertNoOpAMPTestLeak(t, forbidden, db, n, wlID)
	}
	if len(n.statuses) != 1 || n.statuses[0].status.ErrorMessage == "" {
		t.Fatalf("expected redacted status broadcast, got %+v", n.statuses)
	}
	if len(n.rollbacks) != 1 || n.rollbacks[0].reason == "" {
		t.Fatalf("expected redacted rollback reason, got %+v", n.rollbacks)
	}
}

func assertNoOpAMPTestLeak(t *testing.T, forbidden string, db *store.DB, n *fakeNotifier, wlID string) {
	t.Helper()
	hist, err := db.GetWorkloadConfigHistory(wlID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range hist {
		if containsRemoteSecret(row.ErrorMessage, forbidden) {
			t.Fatalf("history error_message leaked %q: %+v", forbidden, row)
		}
		for _, inst := range row.InstanceStatuses {
			if containsRemoteSecret(inst.ErrorMessage, forbidden) {
				t.Fatalf("instance error_message leaked %q: %+v", forbidden, inst)
			}
		}
	}
	wl, err := db.GetWorkload(wlID)
	if err != nil {
		t.Fatal(err)
	}
	if wl.RemoteConfigStatus != nil && containsRemoteSecret(wl.RemoteConfigStatus.ErrorMessage, forbidden) {
		t.Fatalf("workload remote status leaked %q: %+v", forbidden, wl.RemoteConfigStatus)
	}
	for _, status := range n.statuses {
		if containsRemoteSecret(status.status.ErrorMessage, forbidden) {
			t.Fatalf("broadcast status leaked %q: %+v", forbidden, status.status)
		}
	}
	for _, rb := range n.rollbacks {
		if containsRemoteSecret(rb.reason, forbidden) {
			t.Fatalf("rollback reason leaked %q: %+v", forbidden, rb)
		}
	}
}

func containsRemoteSecret(s, forbidden string) bool { return strings.Contains(s, forbidden) }

func TestOnMessage_AcceptsRemoteConfigCapabilityPersisted(t *testing.T) {
	s, db, _ := newTestServer(t)

	uid := make([]byte, 16)
	uid[0] = 0xCC
	uidHex := hex.EncodeToString(uid)
	wlID := fingerprintUIDHex(uidHex)
	_ = wlID

	// Full-status message with AcceptsRemoteConfig set.
	full := &protobufs.AgentToServer{
		InstanceUid: uid,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{Key: "service.name", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol-contrib"}}},
				{Key: "service.version", Value: &protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "0.150.1"}}},
			},
		},
		Capabilities: uint64(protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig),
	}
	s.onMessage(context.TODO(), nil, full)

	wl, err := db.GetWorkload(wlID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !wl.AcceptsRemoteConfig {
		t.Fatalf("after full-status: accepts_remote_config=false, want true")
	}

	// Heartbeat (no AgentDescription): must preserve the previous value.
	hb := &protobufs.AgentToServer{InstanceUid: uid}
	s.onMessage(context.TODO(), nil, hb)
	wl, _ = db.GetWorkload(wlID)
	if !wl.AcceptsRemoteConfig {
		t.Fatalf("after heartbeat: accepts_remote_config flipped to false — should be preserved")
	}

	// A full-status without the bit flips it off.
	fullOff := &protobufs.AgentToServer{
		InstanceUid:      uid,
		AgentDescription: full.AgentDescription,
		Capabilities:     0,
	}
	s.onMessage(context.TODO(), nil, fullOff)
	wl, _ = db.GetWorkload(wlID)
	if wl.AcceptsRemoteConfig {
		t.Fatalf("after full-status with caps=0: accepts_remote_config stayed true")
	}
}

// fakeConn is a no-op types.Connection for exercising onConnectionClose.
type fakeConn struct{}

func (fakeConn) Connection() net.Conn                                     { return nil }
func (fakeConn) Send(_ context.Context, _ *protobufs.ServerToAgent) error { return nil }
func (fakeConn) Disconnect() error                                        { return nil }

func TestOnConnectionClose_UnknownConnection_NoLockLeak(t *testing.T) {
	s, _, _ := newTestServer(t)

	var conn types.Connection = fakeConn{}

	// First call: conn is NOT registered in s.conns / s.connToUID.
	// Triggers the early-return branch. A missing Unlock on that branch
	// would leak the mutex.
	s.onConnectionClose(conn)

	// Register the conn so the second call exercises the past-early-return
	// path. No registry binding is needed for the deadlock check — what we
	// want to verify is purely that the mutex was released by the first
	// call, which means the second Lock() must not block.
	uid := make([]byte, 16)
	uid[0] = 0x11
	uidHex := hex.EncodeToString(uid)
	s.mu.Lock()
	s.conns[uidHex] = conn
	s.connToUID[conn] = uidHex
	s.mu.Unlock()

	// Second call: must complete without deadlocking on s.mu.
	done := make(chan struct{})
	go func() {
		s.onConnectionClose(conn)
		close(done)
	}()
	select {
	case <-done:
		// Success: no deadlock.
	case <-time.After(1 * time.Second):
		t.Fatal("onConnectionClose deadlocked after an unknown-connection early return — s.mu was leaked")
	}
}
