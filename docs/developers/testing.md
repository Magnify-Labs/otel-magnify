# Testing

Use this page as the quick pre-PR checklist for local verification. Run the smallest focused test first while iterating, then run the relevant CI-equivalent gate before opening a PR.

## Backend

The Go module lives at the repository root and currently declares Go `1.25.12` in `go.mod`.

```bash
go test ./...
go build ./...
```

If your local Go version is older than `go.mod`, use the same container pattern as CI:

```bash
docker run --rm \
  -v "$PWD:/app" \
  -w /app \
  -e GOFLAGS='-mod=mod -buildvcs=false' \
  golang:1.25.12 sh -c 'go build ./... && go test ./...'
```

Store tests use in-memory SQLite where possible. PostgreSQL-specific behavior should be covered with a targeted integration setup or Docker Compose when needed.

## Benchmarks

Targeted benchmarks are used as performance guardrails for hot validation paths. They are not thresholded in CI, but the pre-tag gate runs a one-iteration smoke check so regressions that break benchmark execution are caught before release tagging.

Run the config validation guardrail benchmark locally with:

```bash
go test -run '^$' -bench '^BenchmarkValidateMinimalConfig$' -benchmem ./internal/validator
```

## Frontend

Run frontend checks from `frontend/`:

```bash
cd frontend
npm ci
npm run lint
npm run build
npm run test:unit
```

`npm run build` runs TypeScript project checking (`tsc -b`) and then Vite build, which matches the CI frontend gate.

## End-to-end and integration checks

- Mocked Playwright E2E: `cd frontend && npm run test:e2e`.
- Real-backend Playwright flow: `./scripts/e2e-real.sh` or `cd frontend && npm run test:e2e:real` when you intentionally want Docker-backed services.
- SDK agent simulator: `cmd/sdkagent/` exercises the OpAMP pipeline without a real Collector.
- Docker Compose can be used for integration tests against real PostgreSQL when needed.

## Docs and hygiene checks

Documentation-only PRs still trigger docs quality checks:

```bash
python -m venv .venv
. .venv/bin/activate
pip install -r docs/requirements.txt
mkdocs build --strict
```

CI also runs markdownlint and a non-blocking lychee broken-link report for Markdown changes.
