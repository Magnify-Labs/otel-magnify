# Remote config status redaction boundary

`workloads.remote_config_status.error_message` may contain raw collector/agent strings from legacy rows or future non-OpAMP write paths. These strings can include tokens, endpoints, tenant hostnames, or config snippets.

Protection is applied at both boundaries:

- future writes: `models.RemoteConfigStatus.Value()` sanitizes `error_message` before JSON is persisted by store callers such as `UpsertWorkload`;
- legacy reads: `models.RemoteConfigStatus.Scan()` sanitizes decoded JSON before store/API list and get paths return a workload;
- broadcasts: WebSocket config status and auto-rollback events sanitize caller-provided status/reason payloads;
- frontend: rollback/status banners sanitize event text again before rendering.

Existing persisted rows are not rewritten by a SQL migration because `remote_config_status` is a JSON text column shared by SQLite and Postgres, and a dialect-portable in-place update would either lose sibling fields (`status`, `config_hash`, `updated_at`) or need separate JSON function implementations. The enforced compatibility boundary is therefore read-time safety plus at-rest safety for every subsequent write through the model/store path. A future destructive maintenance window can backfill rows through Go code by scanning each workload with `RemoteConfigStatus.Scan()` and re-writing through `RemoteConfigStatus.Value()`.
