package api

import "github.com/magnify-labs/otel-magnify/pkg/capabilities"

func testCapabilities(features map[string]bool) capabilities.Registry {
	return capabilities.FromFeatures(features)
}
