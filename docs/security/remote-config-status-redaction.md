# Remote config status redaction boundary

`workloads.remote_config_status.error_message` may contain raw collector/agent strings from legacy rows or future non-OpAMP write paths. These strings can include tokens, endpoints, tenant hostnames, or config snippets.

Protection is applied at these boundaries:

- future writes: `models.RemoteConfigStatus.Value()` sanitizes `error_message` before JSON is persisted by store callers such as `UpsertWorkload`;
- legacy at-rest rows: `DB.Migrate()` runs a Go backfill after embedded goose migrations, scanning each non-empty `workloads.remote_config_status` value with `models.RemoteConfigStatus.Scan()` and re-writing it through `models.RemoteConfigStatus.Value()`;
- legacy reads: `models.RemoteConfigStatus.Scan()` also sanitizes decoded JSON before store/API list and get paths return a workload, preserving protection if an old database has not yet run the latest migration path;
- broadcasts: WebSocket config status and auto-rollback events sanitize caller-provided status/reason payloads;
- frontend: rollback/status banners sanitize event text again before rendering.

The backfill is implemented in Go rather than SQL because `remote_config_status` is a JSON text column shared by SQLite and Postgres. Using the model scanner/value path keeps the sanitizer canonical and preserves sibling fields such as `status`, `config_hash`, and `updated_at` without dialect-specific JSON mutations. The backfill does not log raw pre-sanitized values; failures identify the workload id only.

Safety boundary for this release: rows reachable through normal startup migration, future store writes, store reads, API responses, and broadcast payloads are sanitized. Unsafe raw legacy values may still exist only in database copies that have not been started with a build containing this backfill, or in out-of-band writes that bypass the Go store/model path; those copies must run the current binary or an equivalent maintenance job that scans `workloads.remote_config_status` through `RemoteConfigStatus.Scan()` and persists via `RemoteConfigStatus.Value()`.
