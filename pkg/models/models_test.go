package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWorkloadJSONRoundTrip(t *testing.T) {
	w := Workload{
		ID:                "abc123",
		FingerprintSource: "k8s",
		FingerprintKeys:   FingerprintKeys{"cluster": "prod", "namespace": "obs", "kind": "deployment", "name": "otel"},
		DisplayName:       "otel-collector",
		Type:              "collector",
		Version:           "0.100.0",
		Status:            "connected",
		LastSeenAt:        time.Unix(0, 0).UTC(),
		Labels:            Labels{"k8s.pod.name": "otel-abc"},
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Workload
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.FingerprintSource != "k8s" || back.FingerprintKeys["namespace"] != "obs" {
		t.Fatalf("lost fields: %+v", back)
	}
}

func TestWorkloadEventJSONRoundTrip(t *testing.T) {
	e := WorkloadEvent{
		ID:          42,
		WorkloadID:  "abc123",
		InstanceUID: "uid-1",
		PodName:     "pod-a",
		EventType:   "connected",
		Version:     "0.100.0",
		OccurredAt:  time.Unix(0, 0).UTC(),
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back WorkloadEvent
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.EventType != "connected" || back.PodName != "pod-a" {
		t.Fatalf("lost fields: %+v", back)
	}
}

func TestRemoteConfigStatusValueSanitizesSensitiveErrorMessage(t *testing.T) {
	raw := "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"
	status := RemoteConfigStatus{
		Status:       "failed",
		ConfigHash:   "hash-a",
		ErrorMessage: raw,
		UpdatedAt:    time.Unix(0, 0).UTC(),
	}

	encoded, err := status.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}

	assertNoSensitiveRemoteStatusText(t, encoded)
	if !strings.Contains(encoded, "redacted") {
		t.Fatalf("encoded status should explain that details were redacted: %s", encoded)
	}
}

func TestRemoteConfigStatusScanSanitizesLegacySensitiveErrorMessage(t *testing.T) {
	raw := `{"status":"failed","config_hash":"hash-a","error_message":"collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces","updated_at":"1970-01-01T00:00:00Z"}`

	var status RemoteConfigStatus
	if err := status.Scan(raw); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	assertNoSensitiveRemoteStatusText(t, status.ErrorMessage)
	if !strings.Contains(status.ErrorMessage, "redacted") {
		t.Fatalf("error_message should explain that details were redacted: %q", status.ErrorMessage)
	}
}

func assertNoSensitiveRemoteStatusText(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{"SECRET_TOKEN", "abc123", "authorization=Bearer", "super-secret", "tenant-a.internal", "4318", "/v1/traces"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("text leaked %q: %s", forbidden, text)
		}
	}
}
