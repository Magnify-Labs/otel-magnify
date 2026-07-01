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

func TestRemoteConfigStatusJSONMarshalSanitizesSensitiveErrorMessage(t *testing.T) {
	raw := "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"
	status := RemoteConfigStatus{
		Status:       "failed",
		ConfigHash:   "hash-a",
		ErrorMessage: raw,
		UpdatedAt:    time.Unix(0, 0).UTC(),
	}

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	assertNoSensitiveRemoteStatusText(t, string(encoded))
	if !strings.Contains(string(encoded), "redacted") {
		t.Fatalf("encoded status should explain that details were redacted: %s", encoded)
	}
}

func TestRemoteConfigStatusSanitizedDoesNotMutateNestedPushStatus(t *testing.T) {
	raw := "collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"
	status := RemoteConfigStatus{
		Status:       "failed",
		ConfigHash:   "hash-a",
		ErrorMessage: raw,
		UpdatedAt:    time.Unix(0, 0).UTC(),
		PushStatus: &WorkloadConfig{
			WorkloadID:   "wl-sensitive",
			ConfigID:     "hash-a",
			Status:       "failed",
			ErrorMessage: raw,
			Timeline:     []WorkloadConfigTimelineEntry{{State: "failed", Message: raw, Terminal: true}},
			ErrorGroups:  []WorkloadConfigErrorGroup{{Cause: "unknown", SampleMessage: raw}},
			InstanceStatuses: []WorkloadConfigInstanceStatus{
				{InstanceUID: "instance-1", Status: "failed", ErrorMessage: raw},
			},
		},
	}

	sanitized := status.Sanitized()

	assertNoSensitiveRemoteStatusText(t, sanitized.ErrorMessage)
	assertNoSensitiveRemoteStatusText(t, sanitized.PushStatus.ErrorMessage)
	assertNoSensitiveRemoteStatusText(t, sanitized.PushStatus.Timeline[0].Message)
	assertNoSensitiveRemoteStatusText(t, sanitized.PushStatus.ErrorGroups[0].SampleMessage)
	assertNoSensitiveRemoteStatusText(t, sanitized.PushStatus.InstanceStatuses[0].ErrorMessage)

	if status.ErrorMessage != raw || status.PushStatus.ErrorMessage != raw || status.PushStatus.Timeline[0].Message != raw || status.PushStatus.ErrorGroups[0].SampleMessage != raw || status.PushStatus.InstanceStatuses[0].ErrorMessage != raw {
		t.Fatalf("Sanitized must not mutate the source status: %+v", status)
	}
}

func TestRemoteConfigStatusJSONUnmarshalSanitizesSensitiveErrorMessage(t *testing.T) {
	raw := `{"status":"failed","config_hash":"hash-a","error_message":"collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces","updated_at":"1970-01-01T00:00:00Z"}`

	var status RemoteConfigStatus
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	assertNoSensitiveRemoteStatusText(t, status.ErrorMessage)
	if !strings.Contains(status.ErrorMessage, "redacted") {
		t.Fatalf("error_message should explain that details were redacted: %q", status.ErrorMessage)
	}
}

func TestSanitizeRemoteConfigErrorMessageCanonicalRollbackContract(t *testing.T) {
	sensitiveRollbackMessage := strings.Join([]string{
		"collector failed while applying remote config for tenant tenant-a",
		"SECRET_TOKEN=abc123",
		"authorization=Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.secret",
		"bearer super-secret-token",
		"endpoint=https://tenant-a.internal:4318/v1/traces",
		"username=tenant-a password=hunter2",
		"config snippet: exporters:\n  otlp:\n    endpoint: https://tenant-a.internal:4317\n    headers:\n      authorization: Bearer SECRET_TOKEN",
	}, " ")

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "unknown sensitive rollback details",
			raw:  sensitiveRollbackMessage,
			want: "Remote config error details redacted",
		},
		{
			name: "collector validation summary",
			raw:  "invalid component in config snippet: exporters.otlp.headers.authorization=Bearer SECRET_TOKEN endpoint=https://tenant-a.internal:4317",
			want: "Collector rejected the config",
		},
		{
			name: "policy summary",
			raw:  "policy refused tenant tenant-a credentials username=tenant-a password=hunter2 authorization=Bearer SECRET_TOKEN",
			want: "Agent policy rejected the config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeRemoteConfigErrorMessage(tt.raw)
			if got != tt.want {
				t.Fatalf("SanitizeRemoteConfigErrorMessage() = %q, want %q", got, tt.want)
			}
			assertNoSensitiveRemoteStatusText(t, got)
		})
	}
}

func TestRemoteConfigStatusModelBoundariesUseRollbackSanitizerContract(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "raw secret token redacts details", raw: "collector failed: SECRET_TOKEN=abc123", want: "Remote config error details redacted"},
		{name: "internal tenant endpoint redacts details", raw: "endpoint=https://tenant-a.internal:4318/v1/traces", want: "Remote config error details redacted"},
		{name: "authorization bearer header redacts details", raw: "authorization=Bearer eyJhbG...cret", want: "Remote config error details redacted"},
		{name: "lowercase bearer token redacts details", raw: "bearer super-secret-token", want: "Remote config error details redacted"},
		{name: "tenant identifier redacts details", raw: "remote config failed for tenant tenant-a", want: "Remote config error details redacted"},
		{name: "credentials redact details", raw: "credentials username=tenant-a password=hunter2", want: "Remote config error details redacted"},
		{name: "config snippet redacts details", raw: "config snippet: exporters:\n  otlp:\n    endpoint: https://tenant-a.internal:4317\n    headers:\n      authorization: Bearer ***", want: "Remote config error details redacted"},
		{name: "invalid component preserves collector validation context", raw: "invalid component in config snippet: exporters.otlp.headers.authorization=Bearer SECRET_TOKEN endpoint=https://tenant-a.internal:4317", want: "Collector rejected the config"},
		{name: "policy refused preserves policy context", raw: "policy refused tenant tenant-a credentials username=tenant-a password=hunter2 authorization=Bearer SECRET_TOKEN", want: "Agent policy rejected the config"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertRemoteConfigStatusModelBoundariesSanitize(t, tt.raw, tt.want)
		})
	}
}

func assertRemoteConfigStatusModelBoundariesSanitize(t *testing.T, raw, want string) {
	t.Helper()
	base := RemoteConfigStatus{
		Status:       "failed",
		ConfigHash:   "hash-a",
		ErrorMessage: raw,
		UpdatedAt:    time.Unix(0, 0).UTC(),
	}

	t.Run("Value", func(t *testing.T) {
		encoded, err := base.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		assertRemoteConfigStatusJSONErrorMessage(t, []byte(encoded), want)
	})

	t.Run("ScanString", func(t *testing.T) {
		legacy := remoteConfigStatusLegacyJSONFixture(t, raw)

		var status RemoteConfigStatus
		if err := status.Scan(string(legacy)); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if status.ErrorMessage != want {
			t.Fatalf("ErrorMessage = %q, want %q", status.ErrorMessage, want)
		}
		assertNoSensitiveRemoteStatusText(t, status.ErrorMessage)
	})

	t.Run("ScanBytes", func(t *testing.T) {
		legacy := remoteConfigStatusLegacyJSONFixture(t, raw)

		var status RemoteConfigStatus
		if err := status.Scan(legacy); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if status.ErrorMessage != want {
			t.Fatalf("ErrorMessage = %q, want %q", status.ErrorMessage, want)
		}
		assertNoSensitiveRemoteStatusText(t, status.ErrorMessage)
	})

	t.Run("MarshalJSON", func(t *testing.T) {
		encoded, err := json.Marshal(base)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		assertRemoteConfigStatusJSONErrorMessage(t, encoded, want)
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		legacy := remoteConfigStatusLegacyJSONFixture(t, raw)

		var status RemoteConfigStatus
		if err := json.Unmarshal(legacy, &status); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if status.ErrorMessage != want {
			t.Fatalf("ErrorMessage = %q, want %q", status.ErrorMessage, want)
		}
		assertNoSensitiveRemoteStatusText(t, status.ErrorMessage)
	})
}

func remoteConfigStatusLegacyJSONFixture(t *testing.T, raw string) []byte {
	t.Helper()
	legacy, err := json.Marshal(map[string]any{
		"status":        "failed",
		"config_hash":   "hash-a",
		"error_message": raw,
		"updated_at":    time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("Marshal legacy fixture: %v", err)
	}
	return legacy
}

func assertRemoteConfigStatusJSONErrorMessage(t *testing.T, payload []byte, want string) {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal encoded status: %v", err)
	}
	if got := decoded["error_message"]; got != want {
		t.Fatalf("error_message = %q, want %q", got, want)
	}
	assertNoSensitiveRemoteStatusText(t, string(payload))
}

func assertNoSensitiveRemoteStatusText(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{"SECRET_TOKEN", "abc123", "authorization=Bearer", "Bearer SECRET_TOKEN", "eyJhbGci", "super-secret", "super-secret-token", "tenant-a", "tenant-a.internal", "4318", "4317", "/v1/traces", "hunter2", "credentials", "password=", "username=", "endpoint:", "config snippet", "exporters:", "headers:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("text leaked forbidden marker %q", forbidden)
		}
	}
}
