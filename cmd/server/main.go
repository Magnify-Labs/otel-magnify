// Command server is the otel-magnify community binary. It is a thin
// wrapper around pkg/bootstrap — all bootstrap logic lives there so
// edition-specific binaries can reuse it.
package main

import (
	"context"
	"log"

	"github.com/magnify-labs/otel-magnify/internal/api"
	"github.com/magnify-labs/otel-magnify/pkg/bootstrap"
	"github.com/magnify-labs/otel-magnify/pkg/capabilities"
	"github.com/magnify-labs/otel-magnify/pkg/server"
)

func main() {
	registry, err := communityCapabilities()
	if err != nil {
		log.Fatalf("server capabilities: %v", err)
	}
	opts := bootstrap.Options{
		ExtraServerOptions: []server.Option{server.WithCapabilities(registry)},
	}
	if err := bootstrap.Run(context.Background(), opts); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func communityCapabilities() (capabilities.Registry, error) {
	return capabilities.New([]capabilities.Capability{
		{ID: api.FeatureConfigSafetyApprovals, State: capabilities.StateEnabled},
		{ID: api.FeatureConfigSafetyPolicyPreview, State: capabilities.StateEnabled},
	})
}
