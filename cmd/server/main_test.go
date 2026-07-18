package main

import (
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/api"
	"github.com/magnify-labs/otel-magnify/pkg/capabilities"
)

func TestCommunityCapabilitiesEnableGovernedActivationWorkflow(t *testing.T) {
	registry, err := communityCapabilities()
	if err != nil {
		t.Fatalf("communityCapabilities(): %v", err)
	}
	document := registry.Document()
	if len(document.Capabilities) != 2 {
		t.Fatalf("community capabilities = %#v, want exactly two capabilities", document.Capabilities)
	}
	for _, id := range []string{
		api.FeatureConfigSafetyApprovals,
		api.FeatureConfigSafetyPolicyPreview,
	} {
		if !registry.Enabled(id) {
			t.Fatalf("community capabilities = %#v, want %s enabled", document.Capabilities, id)
		}
	}
	for _, capability := range document.Capabilities {
		if capability.State != capabilities.StateEnabled {
			t.Fatalf("capability = %#v, want enabled", capability)
		}
	}
}
