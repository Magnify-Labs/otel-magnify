package main

import (
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/api"
)

func TestCommunityFeaturesEnableGovernedActivationWorkflow(t *testing.T) {
	features := communityFeatures()
	if len(features) != 2 {
		t.Fatalf("community features = %#v, want exactly two features", features)
	}
	for _, feature := range []string{
		api.FeatureConfigSafetyApprovals,
		api.FeatureConfigSafetyPolicyPreview,
	} {
		if !features[feature] {
			t.Fatalf("community features = %#v, want %s enabled", features, feature)
		}
	}
}
