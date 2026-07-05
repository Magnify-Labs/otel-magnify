package opamp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
)

type recordingConn struct {
	mu   sync.Mutex
	sent []*protobufs.ServerToAgent
}

func (c *recordingConn) Connection() net.Conn { return nil }
func (c *recordingConn) Disconnect() error    { return nil }
func (c *recordingConn) Send(_ context.Context, msg *protobufs.ServerToAgent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, msg)
	return nil
}

func (c *recordingConn) sentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *recordingConn) onlyMessage() *protobufs.ServerToAgent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 1 {
		return nil
	}
	return c.sent[0]
}

func TestNewOpAMPServer(t *testing.T) {
	srv := New(nil, nil, Options{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestIsCollectorName(t *testing.T) {
	collectors := []string{
		"otelcol",
		"otelcol-contrib",
		"otelcol-custom",
		"OtelCol-Contrib",
		"io.opentelemetry.collector",
		"my-opentelemetry-collector",
	}
	for _, name := range collectors {
		if !isCollectorName(name) {
			t.Errorf("isCollectorName(%q) = false, want true", name)
		}
	}

	sdks := []string{
		"my-service",
		"payment-api",
		"",
		"flask-app",
	}
	for _, name := range sdks {
		if isCollectorName(name) {
			t.Errorf("isCollectorName(%q) = true, want false", name)
		}
	}
}

func TestClassifyAgent_CollectorByOtelcolVersion(t *testing.T) {
	attrs := map[string]string{
		"otelcol.version": "0.150.1",
		"service.name":    "my-custom-collector",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_CollectorByOsDescription(t *testing.T) {
	attrs := map[string]string{
		"os.description": "otelcol/0.150.1 (linux/amd64)",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_SDKByLanguage(t *testing.T) {
	attrs := map[string]string{
		"telemetry.sdk.language": "go",
		"service.name":           "otelcol-trap",
	}
	if got := classifyAgent(attrs); got != "sdk" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "sdk")
	}
}

func TestClassifyAgent_FallbackByServiceName_Collector(t *testing.T) {
	attrs := map[string]string{
		"service.name": "otelcol-foo",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_FallbackByServiceName_SDK(t *testing.T) {
	attrs := map[string]string{
		"service.name": "my-app",
	}
	if got := classifyAgent(attrs); got != "sdk" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "sdk")
	}
}

func TestInstanceCountStartsZero(t *testing.T) {
	srv := New(nil, nil, Options{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.ConnectedInstanceCount() != 0 {
		t.Errorf("expected 0 connected instances, got %d", srv.ConnectedInstanceCount())
	}
}

func TestPushConfig_TargetInstanceSendsOnlyToThatConnection(t *testing.T) {
	srv := New(nil, nil, Options{})
	srv.registry.BindInstance("uid-a", "w1", Instance{Healthy: true})
	srv.registry.BindInstance("uid-b", "w1", Instance{Healthy: true})
	connA := &recordingConn{}
	connB := &recordingConn{}
	srv.mu.Lock()
	srv.conns["uid-a"] = connA
	srv.conns["uid-b"] = connB
	srv.mu.Unlock()

	if err := srv.PushConfig(context.Background(), "w1", []byte("receivers: {}"), "uid-b"); err != nil {
		t.Fatalf("PushConfig target uid-b: %v", err)
	}

	if got := connA.sentCount(); got != 0 {
		t.Fatalf("uid-a received %d messages, want 0", got)
	}
	msg := connB.onlyMessage()
	if msg == nil {
		t.Fatalf("uid-b messages = %d, want 1", connB.sentCount())
	}
	if string(msg.InstanceUid) != "uid-b" {
		t.Fatalf("message InstanceUid = %q, want uid-b", string(msg.InstanceUid))
	}
}

func TestPushConfig_TargetInstanceRejectsCrossWorkloadBinding(t *testing.T) {
	srv := New(nil, nil, Options{})
	srv.registry.BindInstance("uid-a", "w1", Instance{Healthy: true})
	srv.registry.BindInstance("uid-other", "w2", Instance{Healthy: true})
	connOther := &recordingConn{}
	srv.mu.Lock()
	srv.conns["uid-other"] = connOther
	srv.mu.Unlock()

	if err := srv.PushConfig(context.Background(), "w1", []byte("receivers: {}"), "uid-other"); err == nil {
		t.Fatal("PushConfig accepted a target instance bound to another workload")
	}

	if got := connOther.sentCount(); got != 0 {
		t.Fatalf("cross-workload target received %d messages, want 0", got)
	}
}

func TestOpAMPAuthRejectsMissingBearerWhenSharedSecretConfigured(t *testing.T) {
	srv := New(nil, nil, Options{SharedSecret: "expected-token"})
	req := httptest.NewRequest(http.MethodGet, "/v1/opamp", nil)

	resp := srv.authenticateRequest(req)

	if resp.Accept {
		t.Fatal("missing bearer token was accepted")
	}
	if resp.HTTPStatusCode != http.StatusUnauthorized {
		t.Fatalf("HTTPStatusCode = %d, want %d", resp.HTTPStatusCode, http.StatusUnauthorized)
	}
}

func TestOpAMPAuthAcceptsMatchingBearerWhenSharedSecretConfigured(t *testing.T) {
	srv := New(nil, nil, Options{SharedSecret: "expected-token"})
	req := httptest.NewRequest(http.MethodGet, "/v1/opamp", nil)
	req.Header.Set("Authorization", "Bearer expected-token")

	resp := srv.authenticateRequest(req)

	if !resp.Accept {
		t.Fatalf("matching bearer token was rejected with status %d", resp.HTTPStatusCode)
	}
}

func TestOpAMPAuthRejectsWrongBearerWhenSharedSecretConfigured(t *testing.T) {
	srv := New(nil, nil, Options{SharedSecret: "expected-token"})
	req := httptest.NewRequest(http.MethodGet, "/v1/opamp", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp := srv.authenticateRequest(req)

	if resp.Accept {
		t.Fatal("wrong bearer token was accepted")
	}
	if resp.HTTPStatusCode != http.StatusUnauthorized {
		t.Fatalf("HTTPStatusCode = %d, want %d", resp.HTTPStatusCode, http.StatusUnauthorized)
	}
}

func TestOpAMPAuthAllowsConnectionsWhenSharedSecretUnset(t *testing.T) {
	srv := New(nil, nil, Options{})
	req := httptest.NewRequest(http.MethodGet, "/v1/opamp", nil)

	resp := srv.authenticateRequest(req)

	if !resp.Accept {
		t.Fatalf("dev-mode OpAMP connection was rejected with status %d", resp.HTTPStatusCode)
	}
}

// TestOnMessage_UnknownInstance_RequestsFullState guards the resync path:
// when an agent sends a heartbeat for a UID we have no record of (typical
// after a server restart with ephemeral DB), we must set ReportFullState so
// the agent re-sends its AgentDescription and the workload can be bootstrapped.
func TestOnMessage_UnknownInstance_RequestsFullState(t *testing.T) {
	srv := New(nil, nil, Options{})
	uid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	reply := srv.onMessage(context.Background(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 5,
	})

	if reply == nil {
		t.Fatal("onMessage returned nil reply")
	}
	wantFlag := uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState)
	if reply.Flags&wantFlag == 0 {
		t.Errorf("expected ReportFullState flag set, got Flags=0x%x", reply.Flags)
	}
}

// TestOnMessage_KnownInstance_DoesNotRequestFullState is the regression guard:
// once the registry knows the instance, subsequent heartbeats must not carry
// the ReportFullState flag (we already have the state we need).
func TestOnMessage_KnownInstance_DoesNotRequestFullState(t *testing.T) {
	srv := New(nil, nil, Options{})
	ctx := context.Background()
	uid := []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
		0x90, 0xa0, 0xb0, 0xc0, 0xd0, 0xe0, 0xf0, 0x11}

	// Seed the registry with an AgentDescription-bearing message.
	_ = srv.onMessage(ctx, nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 1,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{
					Key: "service.name",
					Value: &protobufs.AnyValue{
						Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol"},
					},
				},
			},
		},
	})

	reply := srv.onMessage(ctx, nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 2,
	})

	if reply == nil {
		t.Fatal("onMessage returned nil reply")
	}
	flag := uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState)
	if reply.Flags&flag != 0 {
		t.Errorf("known-instance heartbeat must not request full state, got Flags=0x%x", reply.Flags)
	}
}
