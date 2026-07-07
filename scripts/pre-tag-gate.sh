#!/usr/bin/env bash
set -euo pipefail

echo "pre-tag gate: go test ./..."
go test ./...

echo "pre-tag gate: benchmark smoke for config validation guardrail"
go test -run '^$' -bench '^BenchmarkValidateMinimalConfig$' -benchtime=1x -benchmem ./internal/validator

echo "pre-tag gate: go build ./cmd/server"
go build ./cmd/server
