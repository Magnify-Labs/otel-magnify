package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
)

func TestAgentCapabilitiesRemainReadOnlyByDefault(t *testing.T) {
	got := agentCapabilities(false)
	want := protobufs.AgentCapabilities_AgentCapabilities_ReportsStatus
	if got != want {
		t.Fatalf("agentCapabilities(false) = %v, want %v", got, want)
	}
}

func TestAgentCapabilitiesEnableRemoteConfigReportingExplicitly(t *testing.T) {
	got := agentCapabilities(true)
	for _, capability := range []protobufs.AgentCapabilities{
		protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig,
		protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig,
		protobufs.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig,
	} {
		if got&capability == 0 {
			t.Fatalf("agentCapabilities(true) = %v, missing %v", got, capability)
		}
	}
}

func TestRemoteConfigStateApplyRejectsMissingHash(t *testing.T) {
	state := &remoteConfigState{}
	status, err := state.apply(&protobufs.AgentRemoteConfig{
		Config: &protobufs.AgentConfigMap{},
	})
	if err == nil {
		t.Fatal("apply returned nil error for a remote config without a hash")
	}
	if status != nil {
		t.Fatalf("apply status = %#v, want nil", status)
	}
}

func TestRemoteConfigStateApplyRejectsMissingConfig(t *testing.T) {
	state := &remoteConfigState{}
	status, err := state.apply(&protobufs.AgentRemoteConfig{
		ConfigHash: []byte("remote-config-hash"),
	})
	if err == nil {
		t.Fatal("apply returned nil error for a remote config without content")
	}
	if status != nil {
		t.Fatalf("apply status = %#v, want nil", status)
	}
}

func TestRemoteConfigStateApplyStoresIndependentEffectiveConfig(t *testing.T) {
	state := &remoteConfigState{}
	remote := &protobufs.AgentRemoteConfig{
		ConfigHash: []byte("remote-config-hash"),
		Config: &protobufs.AgentConfigMap{ConfigMap: map[string]*protobufs.AgentConfigFile{
			"": {
				Body:        []byte("receivers:\n  otlp: {}\n"),
				ContentType: "text/yaml",
			},
		}},
	}

	status, err := state.apply(remote)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if status.Status != protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED {
		t.Fatalf("status = %v, want APPLIED", status.Status)
	}
	if !bytes.Equal(status.LastRemoteConfigHash, remote.ConfigHash) {
		t.Fatalf("status hash = %q, want %q", status.LastRemoteConfigHash, remote.ConfigHash)
	}

	remote.Config.ConfigMap[""].Body[0] = 'X'
	first, err := state.effectiveConfig(context.Background())
	if err != nil {
		t.Fatalf("effectiveConfig: %v", err)
	}
	wantBody := []byte("receivers:\n  otlp: {}\n")
	if got := first.ConfigMap.ConfigMap[""].Body; !bytes.Equal(got, wantBody) {
		t.Fatalf("effective body = %q, want %q", got, wantBody)
	}

	first.ConfigMap.ConfigMap[""].Body[0] = 'Y'
	second, err := state.effectiveConfig(context.Background())
	if err != nil {
		t.Fatalf("second effectiveConfig: %v", err)
	}
	if got := second.ConfigMap.ConfigMap[""].Body; !bytes.Equal(got, wantBody) {
		t.Fatalf("effectiveConfig returned shared mutable state: %q", got)
	}
}
