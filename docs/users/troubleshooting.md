# Troubleshooting

Use this page to map common symptoms to the first checks to run.

## Agents and workloads

- Agent connects but no workload appears in the inventory — check `OPAMP_ADDR`, WebSocket upgrade, reverse-proxy `Connection: Upgrade` headers, and OpAMP bearer auth if `OPAMP_SHARED_SECRET` is set. Also confirm the agent reports enough resource attributes to satisfy at least the `uid` fingerprint strategy; any OpAMP client should provide this by default.
- Multiple pods land on different workloads when you expect one — the `k8s` fingerprint requires `k8s.namespace.name` plus a workload-kind attribute such as `k8s.deployment.name` or `k8s.daemonset.name`. Enable the Collector `resourcedetection` processor with the `k8s` detector to populate them.
- Workload stays `connected` after the pods are gone — normal for up to `WORKLOAD_DISCONNECT_GRACE_SECONDS` (default 120 seconds); it flips to `disconnected` after the grace window elapses.
- A workload disappeared from the inventory — check whether it was manually archived or archived by `WORKLOAD_RETENTION_DAYS` (default 30). Archived workloads are kept in the database but hidden by default; include archived workloads from the UI/API when you need audit history.
- The Activity tab shows heavy churn — high connect/disconnect rate is usually a Kubernetes symptom such as CrashLoopBackOff, OOMKill, or eviction storms, not an otel-magnify problem.

## Config push and rollback

- Push succeeds but an instance shows `FAILED` — inspect the sanitized error message in the workload push history and verify the Collector accepts remote config.
- Direct raw-YAML push returns `410 Gone` — use the approval workflow under `/api/workloads/{id}/config/approvals` or validate YAML with `/api/workloads/{id}/config/validate` before creating an approval draft.
- Auto-rollback loops — usually a bad last-known-good config or an environment-specific validation failure; compare the failing revision with the known-good hash before pushing again.
- Application rollback after a database migration — do not point the old artifact at the upgraded database. Restore the pre-upgrade backup into a separate PostgreSQL 18 database and follow the [lifecycle runbook](../operations/postgresql-lifecycle.md#7-roll-back-with-a-separate-restored-database).

## Browser and storage

- WebSocket disconnects in the UI — check whether the browser still has a valid `om_session` cookie. Legacy clients using `/ws?token=` should check the token and its expiry.
- Login repeatedly returns `401` — verify the seeded admin credentials or reset the user's password through the configured admin process.
- `/healthz` is healthy but `/readyz` is `503` — the process is alive but PostgreSQL is unavailable. Verify `DB_DSN`, network reachability, PostgreSQL 18 server status, credentials, and TLS hostname/root-certificate validation.
- Goose migration fails with a permission or ownership error — the current application role must own the application schemas and objects, including `goose_db_version`; schema `USAGE, CREATE` alone cannot alter or drop objects owned by another role.
- Connections wait or the database reaches its limit — inspect `GET /api/system/database` with a user holding `settings:manage`, compare `in_use`, `wait_count`, and `wait_duration_ms` with `DB_MAX_OPEN_CONNS`, and preserve capacity for administration and migrations.
- Login fails after changing Compose `POSTGRES_PASSWORD` — that variable only initializes the role on an empty volume. Change the actual role password through the provider or `ALTER ROLE`, then update the application credential. Changing only the variable on an initialized volume has no effect.
