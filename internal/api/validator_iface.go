package api

import (
	"context"

	"github.com/magnify-labs/otel-magnify/internal/validator"
)

// ConfigValidator runs a deeper, schema-aware validation of an OTel Collector
// configuration. Implementations may shell out to the upstream `otelcol`
// binary (see internal/validator.OtelcolValidator) or stub the call in tests.
//
// Returning a Result with Valid=true means the configuration parses and every
// component option is recognised by the validator's component set; it does
// NOT guarantee the target agent will accept it (its component set may be
// narrower — that check is the light validator's job).
type ConfigValidator interface {
	Validate(ctx context.Context, content []byte) validator.Result
}
