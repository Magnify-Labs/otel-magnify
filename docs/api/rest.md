# REST API

All endpoints return JSON. Most expect JSON request bodies; the two config endpoints (`POST /api/workloads/{id}/config` and `POST /api/workloads/{id}/config/validate`) are exceptions and take raw YAML. Authenticated endpoints require the header `Authorization: Bearer <jwt>`.

## Endpoint summary

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/auth/login` | No | Log in, returns a JWT. |
| `GET` | `/api/workloads` | Yes | List all workloads. |
| `GET` | `/api/workloads/{id}` | Yes | Get workload details. |
| `GET` | `/api/workloads/{id}/instances` | Yes | Live OpAMP-connected pods for the workload (in-memory, not persisted). |
| `GET` | `/api/workloads/{id}/events` | Yes | Append-only pod-lifecycle log (connect / disconnect / version change). |
| `GET` | `/api/workloads/{id}/events/stats` | Yes | Event counts for the Activity tab sparkline (takes `?window=`). |
| `GET` | `/api/workloads/{id}/configs` | Yes | Config push history for the workload. |
| `POST` | `/api/workloads/{id}/config` | Yes | Push a config to the workload. |
| `GET` | `/api/workloads/{id}/config/approvals` | Yes | List config approval requests for the workload. |
| `POST` | `/api/workloads/{id}/config/approvals` | Yes | Create or update a validated config approval draft. |
| `POST` | `/api/workloads/{id}/config/approvals/{approval_id}/approve` | Yes | Approve a pending config approval draft. |
| `POST` | `/api/workloads/{id}/config/approvals/{approval_id}/push` | Yes | Push an approved draft, or an audited break-glass draft. |
| `POST` | `/api/workloads/{id}/config/validate` | Yes | Lightweight server-side validation of a config. |
| `DELETE` | `/api/workloads/{id}` | Yes | Archive a workload (admin only). |
| `GET` | `/api/configs` | Yes | List all configs. |
| `POST` | `/api/configs` | Yes | Create a new config. |
| `GET` | `/api/configs/{id}` | Yes | Fetch a config by ID. |
| `GET` | `/api/alerts` | Yes | List active alerts. |
| `POST` | `/api/alerts/{id}/resolve` | Yes | Resolve an alert. |
| `GET` | `/ws?token={jwt}` | Yes | WebSocket hub (see [WebSocket](websocket.md)). |
| `GET` | `/healthz` | No | Liveness probe. |

!!! note "Legacy `/api/agents/*` compatibility"
    The previous `/api/agents/*` routes still resolve — they reply with HTTP `307 Temporary Redirect` to the matching `/api/workloads/*` path (`?query` string preserved). New integrations should call `/api/workloads/*` directly.

## Representative payloads

### `POST /api/auth/login`

Request:

```json
{
  "email": "admin@local",
  "password": "changeme"
}
```

Response:

```json
{
  "token": "eyJhbGciOi...",
  "user": { "id": "...", "email": "admin@local", "role": "admin" }
}
```

### `GET /api/workloads`

Response is an array of workload summaries. The exact fields are defined in `pkg/models/workload.go`. Treat it as the source of truth; do not hand-maintain the shape here — link to the file from the rendered doc instead.

### `POST /api/workloads/{id}/config`

Request body is the **raw YAML** (no JSON wrapper), with `Content-Type: application/yaml` or `text/plain`. The server computes the SHA-256 config hash itself. The push is rejected up front if the light validator finds a problem — callers should hit `/validate` first for UX.

Response (202 Accepted):

```json
{
  "status": "config push initiated",
  "config_hash": "3f9a..."
}
```

On validation failure, 400 with `{ "error": "...", "validation_errors": [ ... ] }`. Follow-up push status (`pending` → `applied` | `failed`) arrives via the WebSocket.

### `POST /api/workloads/{id}/config/approvals`

Create or update the single pending approval draft for the same workload and `target_group`. This backend contract lives in community core because it owns the safety gate, persistence, validation, audit, and OpAMP push path; Pro/Enterprise should gate the richer approval UX and any commercial workflow policy with feature flags on top of these shared endpoints.

Request body is JSON:

```json
{
  "draft_yaml": "receivers:\n  otlp:\n...",
  "target_group": "prod-collectors",
  "target_env": "prod",
  "comment": "why this config should be reviewed",
  "prod_confirmation": true
}
```

Validation is mandatory here and again before approval/push. Empty or invalid drafts return `400` with `{ "error": "configuration failed validation", "validation_errors": [...] }` (or `empty config draft`). Prod-targeted drafts require `prod_confirmation=true` before the request is saved. Unsupported remote config returns `409` with `code=remote_config_unsupported`.

Response `201 Created` is `ConfigApprovalRequest`:

```json
{
  "id": "car_...",
  "workload_id": "w1",
  "draft_yaml": "receivers:\n...",
  "target_group": "prod-collectors",
  "target_env": "prod",
  "requester": "operator@example.com",
  "request_comment": "why this config should be reviewed",
  "status": "pending",
  "prod_target": true,
  "prod_confirmation": true,
  "prod_double_confirmed": false,
  "break_glass": false,
  "created_at": "2026-07-02T20:00:00Z",
  "updated_at": "2026-07-02T20:00:00Z"
}
```

Audit action: `config.approval.request`.

### `GET /api/workloads/{id}/config/approvals`

Returns `200 OK` with an array of `ConfigApprovalRequest`, newest first. Any authenticated user can read it; mutations use the same `PushConfig` permission as direct config pushes.

### `POST /api/workloads/{id}/config/approvals/{approval_id}/approve`

Request body:

```json
{ "comment": "approved after reviewing the diff" }
```

Requires a non-empty comment and a still-valid draft. Response `200 OK` is the updated request with `status=approved`, `approved_by`, and `approved_at`. Non-pending approvals return `409` with `code=approval_not_pending`. Audit action: `config.approval.approve`.

### `POST /api/workloads/{id}/config/approvals/{approval_id}/push`

Request body:

```json
{
  "comment": "operator confirmation before pushing",
  "prod_double_confirmed": true,
  "break_glass": false,
  "break_glass_reason": ""
}
```

Normal push requires `status=approved`; otherwise `409` with `code=approval_required`. Prod-targeted pushes require a non-empty `comment` and `prod_double_confirmed=true`, otherwise `400`. Break-glass push is allowed from a pending request only when `break_glass=true`, `comment` is non-empty, and `break_glass_reason` is non-empty; it emits `config.approval.break_glass_push` instead of the normal `config.approval.push` audit action.

Response `202 Accepted` is the updated request with `status=pushed`, `config_hash`, `push_comment`, `pushed_at`, and break-glass metadata when used. The endpoint reuses the standard validator, remote-config gate, config hash, `RecordWorkloadConfig`, OpAMP `PushConfig`, and audit-unavailable semantics: if audit fails after the push side effect, response is `503` with `{ "error": "audit unavailable", "side_effect_status": "applied" }`.

### `POST /api/workloads/{id}/config/validate`

Same request shape as the push endpoint (raw YAML). Optional query parameter: `target_collector_version` overrides the workload-reported collector version for compatibility/runtime proof messaging.

For a non-empty readable body, the endpoint always returns 200 with an enriched validation result. `valid` and top-level `errors[]` are preserved for existing clients; new clients should prefer `overall_status`, `warnings[]`, and `checks[]`.

```json
{
  "valid": false,
  "overall_status": "failed",
  "summary": "Configuration failed 1 blocking validation error(s).",
  "target_collector_version": "0.150.1",
  "validated_at": "2026-06-30T15:09:00Z",
  "checks": [
    {
      "id": "yaml_static",
      "label": "YAML syntax",
      "source": "server.static_yaml",
      "status": "passed",
      "required": true,
      "messages": [
        { "code": "yaml_parse_ok", "severity": "info", "message": "YAML parsed successfully.", "check_id": "yaml_static" }
      ],
      "metadata": { "bytes": 1842 }
    },
    {
      "id": "otelcol_runtime",
      "label": "Collector runtime validation",
      "source": "otelcol.binary",
      "status": "failed",
      "required": false,
      "messages": [
        { "code": "otelcol_validation_failed", "severity": "error", "message": "otelcol validate failed: ...", "check_id": "otelcol_runtime" }
      ],
      "metadata": {
        "binary_path": "/usr/local/bin/otelcol",
        "binary_version": "0.150.1",
        "binary_distribution": "otelcol-contrib",
        "exit_code": 1,
        "duration_ms": 126,
        "command_mode": "otelcol validate --config"
      }
    }
  ],
  "errors": [
    { "code": "otelcol_validation_failed", "message": "otelcol validate failed: ...", "check_id": "otelcol_runtime" }
  ],
  "warnings": []
}
```

Stable check IDs: `yaml_static`, `collector_structure`, `component_availability`, `collector_version_compatibility`, `otelcol_runtime`. Check statuses are `passed`, `warning`, `failed`, or `skipped`; message severities are `info`, `warning`, or `error`. Warnings do not block push. Errors make `valid=false` and are also returned by push/rollback as `validation_errors[]`.

## Error format

Errors follow the shape:

```json
{ "error": "human-readable message" }
```

HTTP status codes follow REST conventions: `400` for bad input, `401` for missing/expired JWT, `403` for insufficient role, `404` for unknown IDs, `409` for conflicts such as pushing to a workload whose reported capabilities do not include `AcceptsRemoteConfig`, `500` for server errors.
