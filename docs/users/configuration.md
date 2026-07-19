# Configuration

otel-magnify is configured primarily through environment variables. See the [reference table](../reference/environment.md) for the exhaustive list. The most common settings are highlighted below.

## Required

| Variable | Description |
|----------|-------------|
| `JWT_SECRET` | HS256 signing key for JWTs issued at login. Startup fails when this is unset, when the placeholder value is used, or when the value is shorter than 32 characters. Use a strong random value in production; at least 32 bytes is recommended. |
| `DB_DSN` | PostgreSQL 18 connection string. Startup fails when this is unset or empty. Use `sslmode=verify-full` with a trusted root certificate and hostname verification in production. |

## Persistence

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DSN` | *(required)* | PostgreSQL connection string. |
| `DB_MAX_OPEN_CONNS` | `40` | Maximum PostgreSQL connections held open. |
| `DB_MAX_IDLE_CONNS` | `10` | Maximum idle PostgreSQL connections retained. |
| `DB_CONN_MAX_IDLE_TIME_SECONDS` | `300` | Maximum idle time for a pooled connection in seconds. |
| `DB_CONN_MAX_LIFETIME_SECONDS` | `1800` | Maximum lifetime for a pooled connection in seconds. |

PostgreSQL 18.x is the only supported database for development and deployment.
Use a managed service or an operator-managed PostgreSQL instance for backups,
recovery, and scaling. See the [PostgreSQL lifecycle
runbook](../operations/postgresql-lifecycle.md) before an upgrade or credential
rotation.

Migrations run automatically on startup via
[`pressly/goose`](https://github.com/pressly/goose). The current single
application role owns the schemas and objects so Goose can perform DDL; a
separate least-privilege runtime role is future hardening, not the current
deployment contract.

Call `GET /api/system/database` with the `settings:manage` permission to inspect
numeric connection-pool counters. Budget `DB_MAX_OPEN_CONNS` across every
database client and leave capacity for administration and migrations.

### Helm deployment controls

The Helm chart fixes `replicaCount` at `1` while the OpAMP registry and live
connections remain process-local, and it uses `Recreate`. Upgrades therefore
have brief downtime and collectors reconnect to the replacement process.
`/healthz` is used for startup and liveness checks; `/readyz` is the readiness
check and depends on PostgreSQL.

When an external Secret changes, apply the Secret first and then set a new
opaque value in `deployment.secretRevision` so Helm changes the pod template.
The chart does not look up external Secret contents. Helm rollback behavior
does not cover the database: `helm upgrade --atomic` never restores PostgreSQL
schemas or data.

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
| `SEED_ADMIN_EMAIL` | Bootstrap email for the first admin. Must be set with `SEED_ADMIN_PASSWORD`; partial configuration fails startup. |
| `SEED_ADMIN_PASSWORD` | Bootstrap password, minimum 12 characters. Use once, then remove it from the runtime environment. |

Bootstrap is fail-closed: it creates the administrator only when the users table
is empty. A restart using the same existing administrator email is idempotent
and does not change the stored password. A different existing user, or a user
with the same email who is not an administrator, makes startup fail. Remove
both seed variables after the first successful login.

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

The Community response is:

```json
{
  "features": {
    "config_safety.approvals": true,
    "config_safety.policy_preview": true
  }
}
```

Together, these flags enable the safety plan and request → approve → push
workflow while direct config push remains disabled. Edition binaries may
expose additional flags such as `sso.admin`. Flags control discovery and UI
rendering only; protected APIs still require authentication and RBAC
permissions.

## Secret handling

Do not paste real values for these settings into public docs, issue trackers, or support transcripts:

- `JWT_SECRET`
- `OPAMP_SHARED_SECRET`
- credential-bearing `DB_DSN`
- credential-bearing `WEBHOOK_URL`
- bearer JWTs or `/ws?token=...` URLs
- Collector YAML containing exporter credentials or API keys
