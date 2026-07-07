# Configuration

otel-magnify is configured primarily through environment variables. See the [reference table](../reference/environment.md) for the exhaustive list. The most common settings are highlighted below.

## Required

| Variable | Description |
|----------|-------------|
| `JWT_SECRET` | HS256 signing key for JWTs issued at login. Startup fails when this is unset, when the placeholder value is used, or when the value is shorter than 32 characters. Use a strong random value in production; at least 32 bytes is recommended. |

## Persistence

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DRIVER` | `sqlite` | `sqlite` (default, pure Go via `modernc.org/sqlite`) or `pgx` for PostgreSQL. |
| `DB_DSN` | `otel-magnify.db` | SQLite file path or a PostgreSQL DSN. |

SQLite is sufficient for local demos and single-instance deployments. PostgreSQL is recommended or required when:

- You run otel-magnify behind multiple replicas.
- You need off-host backup or point-in-time recovery.
- You operate at a scale where SQLite write contention becomes a bottleneck.

Migrations run automatically on startup via [`pressly/goose`](https://github.com/pressly/goose).

## Network

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | API, embedded frontend, health check, and browser WebSocket hub listen address. |
| `OPAMP_ADDR` | `:4320` | OpAMP WebSocket listen address. Agents connect to `/v1/opamp` on this listener. |
| `OPAMP_SHARED_SECRET` | *(empty)* | Optional bearer token required from OpAMP clients. Empty keeps `:4320` unauthenticated for local/dev demos; set a random value for production, shared, or exposed networks. |
| `CORS_ORIGINS` | `http://localhost:5173` | Comma-separated allowed origins for browser API requests. Set this to the exact UI origin(s) in production. |

The browser WebSocket hub lives on the API listener at `/ws` and uses the `om_session` HttpOnly cookie set by login. The legacy `/ws?token=<jwt>` form remains available for non-browser or older clients. The OpAMP protocol is served separately on `OPAMP_ADDR` at `/v1/opamp`.

### OpAMP shared secret

`OPAMP_SHARED_SECRET` protects the OpAMP HTTP/WebSocket handshake on `OPAMP_ADDR`.

- Unset or empty: OpAMP clients can connect without an `Authorization` header. This is intended for local development and demo collectors on a trusted machine or private Docker network.
- Set: every OpAMP client must send the exact value as a bearer token, for example `Authorization: Bearer ***`. Missing or mismatched tokens are rejected with `401 Unauthorized` before any OpAMP message is processed.

Use placeholders in examples and store the real value in your deployment secret manager or shell environment; do not commit real OpAMP secrets.

## Bootstrap

| Variable | Description |
|----------|-------------|
| `SEED_ADMIN_EMAIL` | If set with `SEED_ADMIN_PASSWORD`, creates a first admin user on startup when that email does not already exist. |
| `SEED_ADMIN_PASSWORD` | Password for the seeded admin. Use once, then rotate through the UI or your operational process. |

Remove seed variables after first bootstrap in shared environments.

## Alerting

| Variable | Description |
|----------|-------------|
| `WEBHOOK_URL` | Optional HTTP endpoint called when a new alert fires. Treat credential-bearing URLs as sensitive. |
| `MIN_AGENT_VERSION` | If set, workloads reporting a `service.version` below this are flagged by the alert engine. Empty disables the rule. |

## Workload lifecycle

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKLOAD_DISCONNECT_GRACE_SECONDS` | `120` | Seconds a workload stays `connected` after its last instance disconnects; absorbs rolling updates and pod restarts without flapping. |
| `WORKLOAD_RETENTION_DAYS` | `30` | After flipping to `disconnected`, a workload is archived if it has not reconnected within this window. |
| `WORKLOAD_JANITOR_INTERVAL_SECONDS` | `300` | How often the janitor checks for expired workloads and old events. |
| `WORKLOAD_EVENT_RETENTION_DAYS` | `30` | How long the append-only `workload_events` log is kept before the janitor trims it. |

Archived workloads are hidden from the default inventory but remain available for direct detail, audit, and history views. Operators with archive permission can manually archive a `disconnected` workload from its detail page; connected/degraded workloads stay visible to avoid hiding active fleet members. Use the inventory's "Show archived" filter to include archived rows, and use hard delete only for permanent removal.

Invalid or non-positive duration values fall back to safe defaults in code. For day-based settings the fallback is 30 days; for second-based settings the floor is one second.

## Feature flags

Community otel-magnify does not expose runtime feature-flag environment variables. Feature flags are registered by a binary at construction time through `server.WithFeatures(...)` and surfaced by `GET /api/features`.

The default community response is:

```json
{ "features": {} }
```

Edition binaries may expose flags such as `sso.admin`. Flags control discovery and UI rendering only; protected APIs still require bearer auth and RBAC permissions.

## Secret handling

Do not paste real values for these settings into public docs, issue trackers, or support transcripts:

- `JWT_SECRET`
- `OPAMP_SHARED_SECRET`
- credential-bearing `DB_DSN`
- credential-bearing `WEBHOOK_URL`
- bearer JWTs or `/ws?token=...` URLs
- Collector YAML containing exporter credentials or API keys
