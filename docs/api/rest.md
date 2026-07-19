# REST API

All REST endpoints return JSON unless noted otherwise. Most request bodies are JSON; workload config validation takes raw YAML. The legacy raw-YAML direct push endpoint remains registered for compatibility but returns `410 Gone`; use config approval or rollback workflows instead.

Protected endpoints require:

```http
Authorization: Bearer ***
```

See [Authentication](authentication.md) for login, token lifetime, WebSocket auth, and RBAC notes.

## Endpoint summary

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| `POST` | `/api/auth/login` | No | Email/password login. Returns `{ "token": "..." }`. |
| `GET` | `/api/auth/methods` | No | Lists available login methods. Community default is password login only. |
| `GET` | `/api/v1/capabilities` | No | Canonical capability discovery. It returns the versioned capability document. |
| `GET` | `/api/features` | No | Legacy boolean compatibility endpoint. It returns `{ "features": { ... } }`. |
| `GET` | `/healthz` | No | Plain `ok` liveness response; independent of database connectivity. |
| `GET` | `/readyz` | No | Plain `ready` response when PostgreSQL is reachable; otherwise `503 not ready`. |
| `GET` | `/api/system/database` | Yes + `ManageSettings` permission | Numeric PostgreSQL connection-pool statistics. |
| `GET` | `/api/workloads` | Yes | Lists active, non-archived workloads; pass `?include_archived=true` to include archived rows. |
| `GET` | `/api/workloads/version-intelligence?recommended_version=...` | Yes + feature gate | Fleet version posture; gated by `config_safety.version_intelligence`. |
| `GET` | `/api/workloads/{id}` | Yes | Fetches one workload. |
| `GET` | `/api/workloads/{id}/instances` | Yes | Live OpAMP-connected instances for the workload; in-memory, not persisted. |
| `GET` | `/api/workloads/{id}/topology` | Yes | Versioned live topology wrapper plus workload-level heterogeneity summary. |
| `GET` | `/api/workloads/{id}/events` | Yes | Append-only lifecycle events for the workload. |
| `GET` | `/api/workloads/{id}/events/stats?window=24h` | Yes | Event-count buckets for the Activity tab sparkline. |
| `GET` | `/api/workloads/{id}/configs` | Yes | Config push history for the workload. |
| `GET` | `/api/workloads/{id}/configs/{hash}` | Yes + `ReadConfigContent` permission | Fetches one pushed config revision by hash. |
| `POST` | `/api/workloads/{id}/config` | Yes + `PushConfig` permission | Legacy direct push endpoint; disabled and returns `410 Gone` with `code=config_approval_required`. |
| `GET` | `/api/workloads/{id}/config/approvals` | Yes + feature + `PushConfig` permission | Lists config approval requests; gated by `config_safety.approvals`. |
| `POST` | `/api/workloads/{id}/config/approvals` | Yes + feature + `PushConfig` permission | Creates or updates a validated approval draft. |
| `POST` | `/api/workloads/{id}/config/approvals/{approval_id}/approve` | Yes + feature + `PushConfig` permission | Approves a pending draft. |
| `POST` | `/api/workloads/{id}/config/approvals/{approval_id}/push` | Yes + feature + `PushConfig` permission | Pushes an approved draft, or audited break-glass draft. |
| `POST` | `/api/workloads/{id}/config/validate` | Yes + `ValidateConfig` permission | Validates raw YAML against syntax/components/runtime checks without pushing it. |
| `POST` | `/api/workloads/{id}/configs/{hash}/label` | Yes + `PushConfig` permission | Sets or clears a user-facing revision label. |
| `POST` | `/api/workloads/{id}/configs/{hash}/rollback` | Yes + feature + `PushConfig` permission | Pushes a previous config revision again; gated by `config_safety.guided_rollback`. |
| `POST` | `/api/workloads/{id}/archive` | Yes + `ArchiveWorkload` permission | Archives a disconnected workload; hidden from default inventory and retained for audit/history. |
| `DELETE` | `/api/workloads/{id}` | Yes + `DeleteWorkload` permission | Hard-deletes a workload. |
| `GET` | `/api/configs` | Yes | Lists named config templates. |
| `POST` | `/api/configs` | Yes + `CreateConfigTpl` permission | Creates a named config template. |
| `POST` | `/api/configs/diff` | Yes | Computes a config diff. |
| `GET` | `/api/configs/{id}` | Yes + `ReadConfigContent` permission | Fetches a named config template. |
| `POST` | `/api/configs/migration-assistant/preview` | Yes + `ValidateConfig` permission | Converts a pasted vendor config snippet into a Collector draft preview. |
| `GET` | `/api/alerts?include_resolved=true` | Yes | Lists active alerts by default; include resolved alerts when requested. |
| `POST` | `/api/alerts/{id}/resolve` | Yes + `ResolveAlert` permission | Resolves an alert. |
| `POST` | `/api/reports/evidence-pack` | Yes + `ExportReports` permission | Preview a deterministic evidence-pack JSON payload. |
| `POST` | `/api/reports/evidence-pack/export` | Yes + `ExportReports` permission | Export an evidence pack as Markdown, CSV, or PDF (`?format=`). |
| `GET` | `/api/pushes/activity?window=7d` | Yes | Push activity buckets. Only `7d` is supported today. |
| `GET` | `/api/push-groups` | Yes | Lists configured push groups. |
| `GET` | `/api/me` | Yes | Current user, groups, permissions, and preferences. |
| `PUT` | `/api/me/password` | Yes | Changes the current user's password. |
| `PUT` | `/api/me/preferences` | Yes | Updates current user's UI preferences. |
| `GET` | `/ws` | Yes | Browser WebSocket hub. Browser clients use the HttpOnly session cookie; `?token={jwt}` is a legacy fallback (see [WebSocket](websocket.md)). |

Feature-gated endpoints return `403` with `code=feature_disabled` when their server-side feature is not enabled. Feature flags are discovery metadata; they do not replace authentication or RBAC.

!!! note "Legacy `/api/agents/*` compatibility"
    The previous `/api/agents/*` routes still resolve for the core workload endpoints. They reply with HTTP `307 Temporary Redirect` to the matching `/api/workloads/*` path with the query string preserved. New integrations should call `/api/workloads/*` directly.

## Public endpoints

### `GET /healthz`

Returns plain text `ok` with `200 OK` while the HTTP process is live. This probe does not query PostgreSQL and remains successful when the database is unavailable.

### `GET /readyz`

Checks PostgreSQL connectivity with a one-second timeout derived from the HTTP request context. It returns plain text `ready` with `200 OK` when the check succeeds, or the generic plain text `not ready` with `503 Service Unavailable` otherwise. Database errors and connection details are never included in the response.

### `POST /api/auth/login`

Request:

```json
{ "email": "<bootstrap-email>", "password": "<bootstrap-password>" }
```

Response:

```json
{ "token": "eyJhbGciOi..." }
```

The server emits an audit event for login success/failure. If the audit sink is unavailable, the response is `503` with no business side effect:

```json
{ "error": "audit unavailable", "side_effect_status": "none" }
```

### `GET /api/auth/methods`

Community response:

```json
{
  "methods": [
    {
      "id": "password",
      "type": "password",
      "display_name": "Email + password",
      "login_url": "/api/auth/login"
    }
  ]
}
```

Edition binaries may replace or extend this list through server options.

### `GET /api/v1/capabilities`

This is the canonical public capability-discovery endpoint. The exact Community response in this release is:

```json
{
  "api_version": "v1",
  "capabilities": [
    {
      "id": "config_safety.approvals",
      "state": "enabled"
    },
    {
      "id": "config_safety.policy_preview",
      "state": "enabled"
    }
  ]
}
```

Community advertises only `config_safety.approvals` and `config_safety.policy_preview` in this release.

### Legacy `GET /api/features`

`GET /api/features` remains a legacy boolean compatibility endpoint. Its exact Community response is:

```json
{
  "features": {
    "config_safety.approvals": true,
    "config_safety.policy_preview": true
  }
}
```

Capability discovery is not authorization; protected APIs still enforce authentication, RBAC, and server-side gates. `WithCapabilities` is preferred for typed declarations; `WithFeatures` remains supported for legacy edition overlays.

## Workload contracts

Workload response bodies use `pkg/models.Workload` as the source of truth. Important fields include:

```json
{
  "id": "k8s:prod:deployment:checkout",
  "fingerprint_source": "k8s",
  "fingerprint_keys": { "namespace": "prod", "deployment": "checkout" },
  "display_name": "prod/checkout",
  "type": "collector",
  "version": "0.98.0",
  "status": "connected",
  "last_seen_at": "2026-07-05T20:00:00Z",
  "labels": { "env": "prod" },
  "active_config_hash": "3f9a...",
  "remote_config_status": {
    "status": "applied",
    "config_hash": "3f9a...",
    "updated_at": "2026-07-05T20:00:00Z"
  },
  "available_components": {
    "components": { "receivers": ["otlp"], "exporters": ["debug"] },
    "hash": "..."
  },
  "accepts_remote_config": true
}
```

Status values currently used by the model are `connected`, `disconnected`, and `degraded`. `fingerprint_source` is one of `k8s`, `host`, or `uid`.

### `GET /api/workloads`

Response is an array of workload summaries. Archived workloads are excluded by default; add `?include_archived=true` to render an audit/history view that includes rows with `archived_at` set. The exact fields are defined by `models.Workload` in `pkg/models/models.go`; treat that model as the source of truth and avoid hand-maintaining every field here.

### `POST /api/workloads/{id}/archive`

Manual archive is an operational cleanup action for stale workloads only: the workload must be `disconnected`, and callers need `workload:archive`. A successful archive returns `204 No Content`, stamps `archived_at`, keeps the row available for direct detail/audit/history views, and hides it from `GET /api/workloads` unless `include_archived=true` is set. If the workload reconnects through OpAMP, the normal upsert path clears `archived_at` and it reappears in the default inventory.

Connected or degraded workloads return `409 Conflict` with `code=workload_not_disconnected`; use the detail page state instead of forcing an archive for live fleet members.

### `GET /api/workloads/{id}/instances`

Returns the live OpAMP-connected instances for a workload as an array. This endpoint is backed by the in-memory OpAMP registry, not the database; after a server restart it is empty until agents reconnect. If the workload has no live instances, the response is `[]`, not `null`.

Each item uses `internal/opamp.Instance` fields such as `instance_uid`, `pod_name`, `version`, `connected_at`, `last_message_at`, `healthy`, `effective_config_hash`, `accepts_remote_config`, and optional sanitized `remote_config_status`.

### `GET /api/workloads/{id}/topology`

Returns the same live instance array as `/instances`, wrapped with `schema_version`, `workload_id`, and a `summary`. The summary centralizes workload-level heterogeneity rules for frontend topology views:

```json
{
  "schema_version": "workload-topology.v1",
  "workload_id": "checkout-collector",
  "instances": [],
  "summary": {
    "connected_count": 0,
    "healthy_count": 0,
    "unhealthy_count": 0,
    "drifted_count": 0,
    "heterogeneous": false,
    "version_diversity": [],
    "config_hash_diversity": [],
    "remote_config_status_counts": {
      "capable": 0,
      "no_status": 0,
      "sent": 0,
      "applying": 0,
      "applied": 0,
      "failed": 0
    },
    "heterogeneity": {
      "mixed_versions": false,
      "mixed_effective_config_hashes": false,
      "unhealthy_instances": false,
      "mixed_remote_config_statuses": false,
      "applying_remote_config": false,
      "failed_remote_config": false
    },
    "heterogeneity_reasons": []
  }
}
```

`/topology` does not replace the existing `/instances` array contract. Clients should ignore unknown fields for forward compatibility.

## Config workflow contracts

### Legacy direct push: `POST /api/workloads/{id}/config`

Legacy direct config push is intentionally disabled after the approval flow. Even callers with `workload:push_config` receive `410 Gone` and must create, approve, and push through `/api/workloads/{id}/config/approvals`.

Response:

```json
{
  "error": "direct config push is disabled; create, approve, and push a config approval under /api/workloads/{id}/config/approvals",
  "code": "config_approval_required"
}
```

Use `POST /api/workloads/{id}/config/validate` for raw-YAML validation feedback before creating an approval draft.

### Validate only: `POST /api/workloads/{id}/config/validate`

Request body is raw YAML. Optional query parameter: `target_collector_version` overrides the workload-reported collector version for compatibility/runtime proof messaging.

For a non-empty readable body, the endpoint returns `200` with a validation result. `valid` and top-level `errors[]` are preserved for existing clients; newer clients should also inspect `overall_status`, `warnings[]`, and `checks[]`.

Stable check IDs: `yaml_static`, `collector_structure`, `component_availability`, `collector_version_compatibility`, `otelcol_runtime`. Check statuses are `passed`, `warning`, `failed`, or `skipped`; message severities are `info`, `warning`, or `error`. Warnings do not block push. Errors make `valid=false` and are also returned by push/rollback as `validation_errors[]`.

### Approval draft: `POST /api/workloads/{id}/config/approvals`

Request body is JSON:

```json
{
  "draft_yaml": "receivers:
  otlp:
...",
  "target_group": "prod-collectors",
  "target_env": "prod",
  "comment": "why this config should be reviewed",
  "prod_confirmation": true
}
```

Validation is mandatory here and again before approval/push. Empty or invalid drafts return `400` with `{ "error": "configuration failed validation", "validation_errors": [...] }` or `empty config draft`. Prod-targeted drafts require `prod_confirmation=true`. Unsupported remote config returns `409` with `code=remote_config_unsupported`.

Response `201 Created` is a `ConfigApprovalRequest`; audit action: `config.approval.request`.

### List approvals: `GET /api/workloads/{id}/config/approvals`

Requires `workload:push_config` because list responses include draft YAML and operator comments. Viewers or other users without that permission receive `403 Forbidden`. Authorized callers receive `200 OK` with an array of `ConfigApprovalRequest`, newest first.

### Approve: `POST /api/workloads/{id}/config/approvals/{approval_id}/approve`

Request body:

```json
{ "comment": "approved after reviewing the diff" }
```

Requires a non-empty comment and a still-valid draft. Response `200 OK` is the updated request with `status=approved`, `approved_by`, and `approved_at`. Non-pending approvals return `409` with `code=approval_not_pending`. Audit action: `config.approval.approve`.

### Push approval: `POST /api/workloads/{id}/config/approvals/{approval_id}/push`

Request body:

```json
{
  "comment": "operator confirmation before pushing",
  "prod_double_confirmed": true,
  "break_glass": false,
  "break_glass_reason": ""
}
```

Normal push requires `status=approved`; otherwise `409` with `code=approval_required`. Prod-targeted pushes require a non-empty `comment` and `prod_double_confirmed=true`. Break-glass push is allowed from a pending request only when `break_glass=true`, `comment` is non-empty, and `break_glass_reason` is non-empty; it emits `config.approval.break_glass_push` instead of the normal `config.approval.push` audit action.

Response `202 Accepted` is the updated request with `status=pushed`, `config_hash`, `push_comment`, `pushed_at`, and break-glass metadata when used. If audit fails after the push side effect, response is `503` with `{ "error": "audit unavailable", "side_effect_status": "applied" }`.

### Revision label: `POST /api/workloads/{id}/configs/{hash}/label`

Request:

```json
{ "label": "stable-2026-07" }
```

Use an empty string to clear the label. Labels are bounded server-side; overly long labels return `400`.

### Rollback: `POST /api/workloads/{id}/configs/{hash}/rollback`

The endpoint validates and re-pushes the YAML from a previous revision through the same OpAMP path as an approved push. Follow-up status arrives through the WebSocket hub. Guided rollback and known-good helper endpoints are gated by `config_safety.guided_rollback`.

## Config template contracts

### `POST /api/configs`

Request:

```json
{ "name": "prod baseline", "content": "receivers:
  otlp:
" }
```

Response is a `pkg/models.Config`:

```json
{
  "id": "...",
  "name": "prod baseline",
  "content": "receivers:
  otlp:
",
  "created_at": "2026-07-05T20:00:00Z",
  "created_by": "user-id"
}
```

## System operations

### `GET /api/system/database`

Requires the `settings:manage` permission (`ManageSettings`). The response contains only numeric `database/sql.DBStats` pool counters and durations:

```json
{
  "max_open_connections": 40,
  "open_connections": 3,
  "in_use": 1,
  "idle": 2,
  "wait_count": 0,
  "wait_duration_ms": 0,
  "max_idle_closed": 0,
  "max_idle_time_closed": 0,
  "max_lifetime_closed": 0
}
```

The endpoint never returns a DSN, host, database name, user, or SQL error. If the configured store cannot provide pool statistics, it fails closed with `503 Service Unavailable`.

## User self-service contracts

### `GET /api/me`

Returns the current user's identity, groups, permissions, and preferences. This endpoint is what the SPA uses to decide which actions to render after login.

### `PUT /api/me/password`

Request:

```json
{ "current_password": "<current-password>", "new_password": "<new-random-password>" }
```

The new password must be at least 12 characters and must differ from the current password.

### `PUT /api/me/preferences`

Request:

```json
{ "theme": "system", "language": "en" }
```

Valid themes are `light`, `dark`, and `system`. Valid languages are `en` and `fr`.

## Error format

Most handler errors follow:

```json
{ "error": "human-readable message" }
```

Audit-sink failures use a richer `503` shape:

```json
{ "error": "audit unavailable", "side_effect_status": "applied" }
```

`side_effect_status` is either:

- `none` — no business mutation was persisted; retrying is usually safe.
- `applied` — the business mutation happened before audit emission failed; reconcile state before retrying.

Common status codes:

| Status | Meaning |
|--------|---------|
| `400` | Bad input, invalid JSON/YAML, unsupported query value, or validation error. |
| `401` | Missing, invalid, or expired JWT. |
| `403` | Authenticated user lacks the required permission, or a feature-gated endpoint is disabled. |
| `404` | Unknown ID/hash. |
| `409` | Conflict, such as pushing remote config to a workload that cannot accept it. |
| `410` | Legacy endpoint intentionally disabled. |
| `500` | Unexpected server/store error. |
| `503` | Audit sink unavailable, database readiness failed, or database statistics are unsupported. |
