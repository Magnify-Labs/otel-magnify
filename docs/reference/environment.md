# Environment variables

Exhaustive community-server runtime reference. See [Configuration](../users/configuration.md) for a user-oriented walkthrough.

| Variable | Required | Default | Scope | Description |
|----------|----------|---------|-------|-------------|
| `JWT_SECRET` | Yes | — | Auth | HS256 signing key for JWT tokens. Startup fails when this is unset, when the placeholder value is used, or when the value is shorter than 32 characters. Use a strong random value; at least 32 bytes is recommended for HMAC-SHA256. |
| `LISTEN_ADDR` | No | `:8080` | API | HTTP listen address for the REST API, embedded frontend, health check, and browser WebSocket hub. |
| `OPAMP_ADDR` | No | `:4320` | OpAMP | Listen address for the OpAMP WebSocket server. The OpAMP path is `/v1/opamp`. |
| `OPAMP_SHARED_SECRET` | No | — | OpAMP | Optional bearer token required from OpAMP clients during the HTTP/WebSocket handshake. Empty keeps local/dev OpAMP connections unauthenticated; set a random value for production, shared, or exposed networks. |
| `CORS_ORIGINS` | No | `http://localhost:5173` | API | Comma-separated list of allowed browser origins. Docker Compose sets this to `http://localhost:8080` for same-origin production-style access. |
| `DB_DRIVER` | No | `sqlite` | Store | `sqlite` (default) or `pgx` for PostgreSQL. |
| `DB_DSN` | No | `otel-magnify.db` | Store | SQLite file path or PostgreSQL DSN. Docker and Helm commonly set this to a `/data/...` path or a secret-backed PostgreSQL URL. |
| `SEED_ADMIN_EMAIL` | No | — | Bootstrap | If set with `SEED_ADMIN_PASSWORD`, creates a first admin user on startup when that email does not already exist. |
| `SEED_ADMIN_PASSWORD` | No | — | Bootstrap | Password for the seeded admin. Use only for initial bootstrap, then rotate through the UI or your operational process. |
| `WEBHOOK_URL` | No | — | Alerts | HTTP endpoint called when a new alert fires. Treat as sensitive if it contains embedded credentials. |
| `MIN_AGENT_VERSION` | No | — | Alerts | Minimum `service.version`; workloads reporting a lower semantic version are flagged by the alert engine. Empty disables this rule. |
| `WORKLOAD_DISCONNECT_GRACE_SECONDS` | No | `120` | Workloads | Seconds a workload remains `connected` after its last live instance disconnects, absorbing rolling updates and short restarts. Invalid or non-positive values fall back to one second. |
| `WORKLOAD_RETENTION_DAYS` | No | `30` | Workloads | Days a `disconnected` workload is kept before archival by the workload janitor. Invalid or non-positive values fall back to 30 days. |
| `WORKLOAD_JANITOR_INTERVAL_SECONDS` | No | `300` | Workloads | Workload janitor tick interval. The janitor archives expired workloads and purges old events. Invalid or non-positive values fall back to one second. |
| `WORKLOAD_EVENT_RETENTION_DAYS` | No | `30` | Workloads | Days the `workload_events` log is kept before the janitor trims it. Invalid or non-positive values fall back to 30 days. |

## Feature flags

Feature flags are not configured through environment variables in the community binary. They are static server options registered by the binary with `server.WithFeatures(...)`, optionally backed by an edition `WithLicenseChecker(...)`, and exposed by the public endpoint `GET /api/features`.

Default community response:

```json
{ "features": {} }
```

Edition or extension binaries may advertise capabilities such as:

```json
{ "features": { "sso.admin": true } }
```

Known server-side feature gate names are code-defined in `internal/api/feature_gate.go`. They include `config_safety.approvals`, `config_safety.guided_rollback`, `config_safety.canary_rollout`, `config_safety.scoped_push`, `config_safety.drift_dashboard`, `config_safety.version_intelligence`, `config_safety.gitops_export`, `config_safety.policy_preview`, `reports.evidence_pack`, and `audit.viewer`.

Do not use feature flags as an authorization boundary. They are UI/API discovery metadata; protected handlers must still enforce authentication and RBAC.

## Sensitive values

The following values should not be pasted into public issues, docs, logs, or screenshots:

- Real `JWT_SECRET` values.
- Real `OPAMP_SHARED_SECRET` values.
- PostgreSQL credentials inside `DB_DSN`.
- Credential-bearing `WEBHOOK_URL` values.
- Bearer JWTs and WebSocket URLs containing `?token=`.
- Collector YAML that embeds exporter credentials, API keys, or endpoint passwords.
