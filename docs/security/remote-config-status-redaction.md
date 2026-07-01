# Remote config status redaction boundary

`workloads.remote_config_status.error_message` may contain raw collector/agent strings from legacy rows or future non-OpAMP write paths. These strings can include tokens, endpoints, tenant hostnames, or config snippets.

Protection is applied at these concrete boundaries:

- future writes: `internal/store/workloads.go` calls `models.RemoteConfigStatus.Value()` from `DB.UpsertWorkload()` before persisting JSON;
- legacy at-rest rows: `internal/store/db.go` runs `DB.sanitizeLegacyRemoteConfigStatuses()` from `DB.Migrate()` after embedded goose migrations, scanning each non-empty `workloads.remote_config_status` value with `models.RemoteConfigStatus.Scan()` and re-writing it through `models.RemoteConfigStatus.Value()`;
- legacy reads: `DB.GetWorkload()` and `DB.ListWorkloads()` in `internal/store/workloads.go` decode with `models.RemoteConfigStatus.Scan()` before store/API list and get paths return a workload, preserving protection if an old database has not yet run the latest migration path;
- broadcasts: `internal/api/wshub.go` sanitizes config status and auto-rollback payloads in `Hub.BroadcastConfigStatus()`, `Hub.BroadcastWorkloadUpdate()`, and `Hub.BroadcastAutoRollback()`;
- frontend: `frontend/src/components/workloads/PushStatusBanner.tsx` renders rollback/status text only through `safeRemoteErrorText()` or `safeRollbackReasonText()` from `frontend/src/lib/safeRemoteErrorText.ts`.

The backfill is implemented in Go rather than SQL because `remote_config_status` is a JSON text column shared by SQLite and Postgres. Using the model scanner/value path keeps `models.SanitizeRemoteConfigErrorMessage()` as the canonical sanitizer and preserves sibling fields such as `status`, `config_hash`, and `updated_at` without dialect-specific JSON mutations. The backfill does not log raw pre-sanitized values; failures identify the workload id only.

Safety boundary for this release: rows reachable through normal startup migration, future store writes, store reads, API responses, and broadcast payloads are sanitized. Unsafe raw legacy values may still exist only in database copies that have not been started with a build containing this backfill, or in out-of-band writes that bypass the Go store/model path; those copies must run the current binary or an equivalent maintenance job that scans `workloads.remote_config_status` through `RemoteConfigStatus.Scan()` and persists via `RemoteConfigStatus.Value()`.

Security review should stay focused on the remaining bypasses and regressions: callers that persist `remote_config_status` without `RemoteConfigStatus.Value()`, SQL or maintenance scripts that mutate the JSON column directly, API or WebSocket paths that expose unscanned legacy rows, frontend rendering that bypasses `safeRemoteErrorText()`/`safeRollbackReasonText()`, and tests or diagnostics that might print raw fixture values on failure. Database backups, replicas, and exports taken before this backfill are outside the runtime guarantee until they are restored and migrated through the same model sanitizer path.
