# Testing

Use this page as the quick pre-PR checklist for local verification.

## Backend

The Go module lives at the repository root, so run backend tests from the root directory:

```bash
go test ./...
```

Store tests use in-memory SQLite where possible.

## Benchmarks

Targeted benchmarks are used as performance guardrails for hot validation paths. They are not thresholded in CI, but the pre-tag gate runs a one-iteration smoke check so regressions that break benchmark execution are caught before release tagging.

Run the config validation guardrail benchmark locally with:

```bash
go test -run '^$' -bench '^BenchmarkValidateMinimalConfig$' -benchmem ./internal/validator
```

## Frontend

Run the TypeScript check from the frontend workspace:

```bash
cd frontend
npx tsc --noEmit
```

## End-to-end and integration checks

- Playwright E2E: `cd frontend && npm run test:e2e`.
- SDK agent simulator: `cmd/sdkagent/` exercises the OpAMP pipeline without a real Collector.
- Docker Compose can be used for integration tests against real PostgreSQL when needed.
