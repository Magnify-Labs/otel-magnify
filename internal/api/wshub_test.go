package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// hookClient registers a receive-only ws client that captures broadcast frames.
func hookClient(h *Hub) *wsClient {
	c := &wsClient{send: make(chan []byte, 8)}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	return c
}

func TestBroadcastWorkloadUpdatePayload(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()
	c := hookClient(h)

	h.BroadcastWorkloadUpdate(models.Workload{ID: "w1", Status: "connected"}, 2, 1)

	select {
	case raw := <-c.send:
		body := string(raw)
		for _, want := range []string{
			`"type":"workload_update"`,
			`"connected_instance_count":2`,
			`"drifted_instance_count":1`,
			`"id":"w1"`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("payload missing %q: %s", want, body)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestBroadcastWorkloadUpdate_RedactsSensitiveRemoteConfigStatus(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()
	c := hookClient(h)

	h.BroadcastWorkloadUpdate(models.Workload{
		ID: "w1",
		RemoteConfigStatus: &models.RemoteConfigStatus{
			Status:       "failed",
			ConfigHash:   "abc",
			ErrorMessage: "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces",
			UpdatedAt:    time.Unix(0, 0).UTC(),
		},
	}, 2, 1)

	select {
	case raw := <-c.send:
		assertNoSensitiveWSPayload(t, string(raw))
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestBroadcastWorkloadEventPayload(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()
	c := hookClient(h)

	h.BroadcastWorkloadEvent(models.WorkloadEvent{ID: 42, WorkloadID: "w1", EventType: "connected", InstanceUID: "uid"})

	select {
	case raw := <-c.send:
		body := string(raw)
		for _, want := range []string{
			`"type":"workload_event"`,
			`"workload_id":"w1"`,
			`"event_type":"connected"`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("payload missing %q: %s", want, body)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestBroadcastConfigStatus_SerializesEvent(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[&wsClient{send: ch}] = true
	h.mu.Unlock()

	h.BroadcastConfigStatus("workload-1", models.RemoteConfigStatus{
		Status: "failed", ConfigHash: "abc", ErrorMessage: "boom",
		UpdatedAt: time.Unix(0, 0).UTC(),
	})

	select {
	case b := <-ch:
		var ev map[string]any
		_ = json.Unmarshal(b, &ev)
		if ev["type"] != "workload_config_status" || ev["workload_id"] != "workload-1" {
			t.Fatalf("unexpected event: %s", string(b))
		}
		st := ev["status"].(map[string]any)
		if st["status"] != "failed" || st["error_message"] != models.SanitizeRemoteConfigErrorMessage("boom") {
			t.Fatalf("status payload: %+v", st)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestBroadcastAutoRollback_SerializesEvent(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[&wsClient{send: ch}] = true
	h.mu.Unlock()

	h.BroadcastAutoRollback("workload-1", "bbbbbbbb", "aaaaaaaa", "oops", "last_known_good")
	select {
	case b := <-ch:
		var ev map[string]any
		_ = json.Unmarshal(b, &ev)
		if ev["type"] != "auto_rollback_applied" || ev["workload_id"] != "workload-1" || ev["from_hash"] != "bbbbbbbb" || ev["to_hash"] != "aaaaaaaa" || ev["target_kind"] != "last_known_good" {
			t.Fatalf("payload: %s", string(b))
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast")
	}
}

func TestBroadcastConfigStatus_RedactsSensitiveErrorMessage(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[&wsClient{send: ch}] = true
	h.mu.Unlock()

	h.BroadcastConfigStatus("workload-1", models.RemoteConfigStatus{
		Status:       "failed",
		ConfigHash:   "abc",
		ErrorMessage: "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces",
		UpdatedAt:    time.Unix(0, 0).UTC(),
	})

	select {
	case b := <-ch:
		assertNoSensitiveWSPayload(t, string(b))
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestBroadcastAutoRollback_RedactsSensitiveReason(t *testing.T) {
	h := NewHub()
	go h.Run()
	defer h.Stop()

	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.clients[&wsClient{send: ch}] = true
	h.mu.Unlock()

	h.BroadcastAutoRollback("workload-1", "bbbbbbbb", "aaaaaaaa", "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces", "last_known_good")
	select {
	case b := <-ch:
		assertNoSensitiveWSPayload(t, string(b))
	case <-time.After(time.Second):
		t.Fatal("no broadcast")
	}
}

func assertNoSensitiveWSPayload(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"SECRET_TOKEN", "abc123", "authorization=Bearer", "super-secret", "tenant-a.internal", "4318", "/v1/traces"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("payload leaked forbidden marker %q", forbidden)
		}
	}
	if !strings.Contains(body, "redacted") {
		t.Fatalf("payload should explain redacted details")
	}
}
