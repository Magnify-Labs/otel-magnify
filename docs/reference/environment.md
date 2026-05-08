# Environment variables

Exhaustive reference. See [Configuration](../users/configuration.md) for a user-oriented walkthrough.

| Variable | Required | Default | Scope | Description |
|----------|----------|---------|-------|-------------|
| `JWT_SECRET` | Yes | — | Auth | HS256 signing key for JWT tokens. |
| `LISTEN_ADDR` | No | `:8080` | API | HTTP listen address for the API and embedded frontend. |
| `OPAMP_ADDR` | No | `:4320` | OpAMP | WebSocket listen address for the OpAMP server. |
| `CORS_ORIGINS` | No | `http://localhost:5173` | API | Comma-separated list of allowed origins. |
| `DB_DRIVER` | No | `sqlite` | Store | `sqlite` (default) or `pgx` for PostgreSQL. |
| `DB_DSN` | No | `otel-magnify.db` | Store | SQLite file path or PostgreSQL DSN. |
| `SEED_ADMIN_EMAIL` | No | — | Bootstrap | If set with `SEED_ADMIN_PASSWORD`, creates a first admin user on startup. |
| `SEED_ADMIN_PASSWORD` | No | — | Bootstrap | Password for the seeded admin. |
| `WEBHOOK_URL` | No | — | Alerts | HTTP endpoint called when a new alert fires. |
| `MIN_AGENT_VERSION` | No | — | Alerts | Minimum `service.version`; workloads reporting a lower version are flagged. |
| `BINARY_OTELCOL` | No | `/usr/local/bin/otelcol-contrib` | Validation | Absolute path to an `otelcol`-compatible binary used by `POST /api/configs/validate`. Empty value disables the endpoint (returns 503). |
| `WORKLOAD_DISCONNECT_GRACE_SECONDS` | No | `120` | Workloads | Seconds a workload stays `connected` after its last instance disconnects. |
| `WORKLOAD_RETENTION_DAYS` | No | `30` | Workloads | Days a `disconnected` workload is kept before the janitor archives it. |
| `WORKLOAD_EVENT_RETENTION_DAYS` | No | `30` | Workloads | Days the `workload_events` log is kept before the janitor trims it. |
