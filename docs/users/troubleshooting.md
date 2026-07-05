# Troubleshooting

> **Status:** This page is a stub. It will grow as user questions arrive.

## Planned content

- Agent connects but no workload appears in the inventory — check `OPAMP_ADDR`, WebSocket upgrade, reverse-proxy `Connection: Upgrade` headers; confirm the agent reports enough resource attributes to satisfy at least the `uid` fingerprint strategy (any OpAMP client provides this by default).
- Multiple pods land on different workloads when you expect one — the `k8s` fingerprint requires `k8s.namespace.name` plus a workload-kind attribute (`k8s.deployment.name`, `k8s.daemonset.name`, ...); enable the `resourcedetection` processor with the `k8s` detector to populate them.
- Push succeeds in the UI but an instance shows `FAILED` — inspect the error message stored in the workload's push history.
- Auto-rollback loops — usually a bad last-known-good config; reset via the API.
- Workload stays `connected` after the pods are gone — normal for up to `WORKLOAD_DISCONNECT_GRACE_SECONDS` (default 120 s); flips to `disconnected` afterwards.
- A workload disappeared from the inventory — check `WORKLOAD_RETENTION_DAYS` (default 30); archived workloads are kept in the database but hidden by default.
- The Activity tab shows heavy churn — high connect/disconnect rate is a K8s symptom (CrashLoopBackOff, OOMKill, eviction storms), not an otel-magnify problem.
- WebSocket disconnects in the UI — check whether the browser still has a valid `om_session` cookie; legacy clients using `/ws?token=` should check that token and its expiry.
- SQLite "database is locked" under load — switch to PostgreSQL.
