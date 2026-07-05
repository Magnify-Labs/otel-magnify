# Configuration

otel-magnify is configured entirely via environment variables. See the [reference table](../reference/environment.md) for the exhaustive list. The most common variables are highlighted below.

## Required

| Variable | Description |
|----------|-------------|
| `JWT_SECRET` | HS256 signing key for the JWT issued at login. Must be at least 32 characters, must not be the placeholder value, and should be a strong random value in production. |

## Persistence

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DRIVER` | `sqlite` | `sqlite` (default, pure Go via `modernc.org/sqlite`) or `pgx` (PostgreSQL). |
| `DB_DSN` | `otel-magnify.db` | SQLite file path or a PostgreSQL DSN. |

## Network

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | API + frontend listen address. |
| `OPAMP_ADDR` | `:4320` | OpAMP WebSocket listen address. |
| `CORS_ORIGINS` | `http://localhost:5173` | Comma-separated allowed origins for the API. |

## Bootstrap

| Variable | Description |
|----------|-------------|
| `SEED_ADMIN_EMAIL` | If set, creates a first admin user on startup. |
| `SEED_ADMIN_PASSWORD` | Password for the seeded admin. Use once, then rotate via the UI. |

## Alerting

| Variable | Description |
|----------|-------------|
| `WEBHOOK_URL` | Optional HTTP endpoint called when a new alert fires. |
| `MIN_AGENT_VERSION` | If set, workloads reporting a `service.version` below this are flagged by the alert engine. |

## Workload retention

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKLOAD_DISCONNECT_GRACE_SECONDS` | `120` | Seconds a workload stays `connected` after its last instance disconnects — absorbs rolling updates and pod restarts without flapping. |
| `WORKLOAD_RETENTION_DAYS` | `30` | After flipping to `disconnected`, a workload is archived if it has not reconnected within this window. |
| `WORKLOAD_EVENT_RETENTION_DAYS` | `30` | How long the append-only `workload_events` log is kept before the janitor trims it. |

## SQLite vs PostgreSQL

SQLite is sufficient for single-instance deployments and demos. PostgreSQL is required when:

- You run otel-magnify behind multiple replicas.
- You need off-host backup or point-in-time recovery.
- You operate at a scale where SQLite write contention becomes a bottleneck.

Migrations run automatically on startup via [`pressly/goose`](https://github.com/pressly/goose).
