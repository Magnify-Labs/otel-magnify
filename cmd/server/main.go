// Command server is the otel-magnify community binary. It is a thin
// wrapper around pkg/bootstrap — all bootstrap logic lives there so
// edition-specific binaries can reuse it.
package main

import (
	"context"
	"log"

	"github.com/magnify-labs/otel-magnify/internal/api"
	"github.com/magnify-labs/otel-magnify/pkg/bootstrap"
	"github.com/magnify-labs/otel-magnify/pkg/server"
)

func main() {
	opts := bootstrap.Options{
		ExtraServerOptions: []server.Option{
			server.WithFeatures(communityFeatures()),
		},
	}
	if err := bootstrap.Run(context.Background(), opts); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func communityFeatures() map[string]bool {
	return map[string]bool{
		api.FeatureConfigSafetyApprovals:     true,
		api.FeatureConfigSafetyPolicyPreview: true,
	}
}
