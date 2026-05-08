# Configuration

otel-magnify is configured entirely via environment variables. See the [reference table](../reference/environment.md) for the exhaustive list. The most common variables are highlighted below.

## Required

| Variable | Description |
|----------|-------------|
| `JWT_SECRET` | HS256 signing key for the JWT issued at login. Must be a strong random value in production. |

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

## Pre-push validation

Before a configuration is pushed to a workload, otel-magnify validates it in two passes:

1. **Light validation (always on)** — `service.pipelines` must be present, every component reference (`receivers`, `processors`, `exporters`, `extensions`) must resolve to a declaration in the matching top-level section, and — when the agent has reported its inventory — the component type must be installed there. This runs in-process, no external dependency.
2. **Schema validation (when configured)** — the API exposes `POST /api/configs/validate`, which shells out to `otelcol-contrib validate --config /dev/stdin`. This catches what the light pass cannot: invalid keys inside a component block, type mismatches, missing required options.

The editor's **Validate** button fires both endpoints in parallel and merges the diagnostics. The **Push** button enables only when both passes are green — unless the operator clicks **Override validation** (emergency path).

| Variable | Default | Description |
|----------|---------|-------------|
| `BINARY_OTELCOL` | `/usr/local/bin/otelcol-contrib` | Absolute path to the `otelcol` binary invoked by `POST /api/configs/validate`. Empty → the endpoint responds `503` and only the light pass runs. |

### Example diagnostics

Component not installed on the target agent (light pass, workload-scoped):

```
component_not_installed   service.pipelines.traces.exporters[0]
  exporter type "kafka" is not installed on the target agent (available: logging, otlp, otlphttp)
```

Invalid field inside a component (schema pass):

```
otelcol_validate          receivers.otlp
  invalid configuration: receivers::otlp: 1 error(s) decoding:
    * '' has invalid keys: bogus_field
```

### Override (emergency)

When an operator knows a configuration is correct but the validator rejects it incorrectly (for example a newly added component the agent's inventory has not yet caught up to), an **Override validation** link surfaces next to the disabled Push button. With override engaged, the push is sent with `?override=true`:

- the server-side light validation safety net is bypassed (the schema validation remains informative but no longer blocks the UI),
- a `WARN` log line is emitted (`config push override=true workload=<id> actor=<email>`),
- an audit event `config.push` is recorded with `Detail=override=true`.

This path is intentionally more prominent than the regular Push button; it is meant for incidents, not the day-to-day flow.

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
