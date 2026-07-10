#!/usr/bin/env bash
set -euo pipefail

if [ -z "${TEST_POSTGRES_DSN:-}" ]; then
  echo "pre-tag gate: TEST_POSTGRES_DSN must be set to a PostgreSQL test database" >&2
  exit 1
fi

echo "pre-tag gate: go test ./..."
go test ./...

echo "pre-tag gate: benchmark smoke for config validation guardrail"
go test -run '^$' -bench '^BenchmarkValidateMinimalConfig$' -benchtime=1x -benchmem ./internal/validator

echo "pre-tag gate: go build ./cmd/server"
go build ./cmd/server
