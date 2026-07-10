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

## Browser and storage

- WebSocket disconnects in the UI — check whether the browser still has a valid `om_session` cookie. Legacy clients using `/ws?token=` should check the token and its expiry.
- Login repeatedly returns `401` — verify the seeded admin credentials or reset the user's password through the configured admin process.
- PostgreSQL connection failures — verify `DB_DSN`, network reachability, credentials, TLS settings, and the database connection limit. Use `sslmode=require` outside the local Compose network.
