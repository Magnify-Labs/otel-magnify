# CLAUDE.md — otel-magnify

## Project

Centralized OpenTelemetry agent management platform via OpAMP.
Go backend + React frontend, single binary deployment.

## Architecture

Go module `github.com/magnify-labs/otel-magnify` (lives at the repo root — `go.mod` is at the top level):
- `cmd/server/` — entrypoint, embeds frontend via `embed.FS`
- `internal/opamp/` — OpAMP server (opamp-go), agent registry, config push
- `internal/api/` — chi router, REST handlers, WebSocket hub
- `internal/alerts/` — alert engine (30s tick), webhook notifier
- `internal/auth/` — JWT HS256, middleware
- `internal/store/` — SQLite/Postgres via goose migrations
- `pkg/models/` — shared structs
- `frontend/` — React 18 + TypeScript + Vite
  - Zustand for state, TanStack Query for fetching, CodeMirror 6 for YAML editor
  - Design system: "Signal Deck" (warm gold accent, Plus Jakarta Sans + Fira Code)
- `agents/` — sample OTel Collector configs for demo
- `helm/otel-magnify/` — Kubernetes Helm chart

## Commands

```bash
# Backend
go test ./...
go build ./cmd/server/

# Frontend
cd frontend && npx tsc --noEmit
cd frontend && npm run dev

# Docker
JWT_SECRET=xxx docker compose up --build
# With Postgres:
DB_DRIVER=pgx DB_DSN="postgres://magnify:magnify@postgres:5432/magnify?sslmode=disable" docker compose --profile postgres up

# Demo collectors
docker run -d --name collector-prod-eu --network otel-magnify_default \
  -v $(pwd)/agents/collector-prod-eu.yaml:/etc/otelcol-contrib/config.yaml \
  otel/opentelemetry-collector-contrib:0.98.0
```

## Conventions

- **Language**: all repo content (documentation, code, commits) in English
- **Commits**: conventional (`feat:`, `fix:`, `docs:`, `refactor:`)
- **No Co-Authored-By** in commit messages
- **Go**: standard `testing`, in-memory SQLite for tests, `chi` for routing
- **Frontend**: CSS classes in `styles/global.css`, no inline styles, CSS variables for theming
- **Pages route**: `/inventory` (not `/agents`) — contains both collectors and SDK agents

## Key design decisions

- `pressly/goose` over `golang-migrate` — better `modernc.org/sqlite` support (pure Go, no CGO)
- OpAMP server uses `Attach()` to mount on chi mux (not standalone)
- Agent type detection via `isCollectorName()` — matches `otelcol*` prefix patterns
- WebSocket auth prefers the HttpOnly session cookie; `?token=` remains as a legacy fallback because browsers cannot set WS auth headers
- Frontend embed.FS with SPA fallback for production single-binary deployment

## Release workflow

```bash
# 1. Tag the version
git tag v0.x.y -m "release: v0.x.y"

# 2. Generate changelog (requires git-cliff)
git-cliff --output CHANGELOG.md
git add CHANGELOG.md && git commit -m "docs: update changelog for v0.x.y"

# 3. Push
git push origin main && git push origin v0.x.y

# 4. Create GitHub release manually (paste CHANGELOG.md content)
```

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `JWT_SECRET` | Yes | JWT signing key |
| `DB_DRIVER` | No (sqlite) | `sqlite` or `pgx` |
| `DB_DSN` | No | DB connection string |
| `WEBHOOK_URL` | No | Alert webhook endpoint |
| `MIN_AGENT_VERSION` | No | Minimum agent version for alerts |
| `SEED_ADMIN_EMAIL` | No | Create admin on startup |
| `SEED_ADMIN_PASSWORD` | No | Admin password |
