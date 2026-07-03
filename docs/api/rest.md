# REST API

All endpoints return JSON. Most expect JSON request bodies; `POST /api/workloads/{id}/config/validate` is the raw-YAML validation exception. The legacy raw-YAML direct push endpoint `POST /api/workloads/{id}/config` remains registered for compatibility but now returns `410 Gone`. Authenticated endpoints require the header `Authorization: Bearer JWT`.

## Endpoint summary

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/auth/login` | No | Log in, returns a JWT. |
| `GET` | `/api/workloads` | Yes | List all workloads. |
| `GET` | `/api/workloads/{id}` | Yes | Get workload details. |
| `GET` | `/api/workloads/{id}/instances` | Yes | Live OpAMP-connected pods for the workload (in-memory, not persisted). |
| `GET` | `/api/workloads/{id}/topology` | Yes | Live instance topology plus workload-level heterogeneity summary. |
| `GET` | `/api/workloads/{id}/events` | Yes | Append-only pod-lifecycle log (connect / disconnect / version change). |
| `GET` | `/api/workloads/{id}/events/stats` | Yes | Event counts for the Activity tab sparkline (takes `?window=`). |
| `GET` | `/api/workloads/{id}/configs` | Yes | Config push history for the workload. |
| `POST` | `/api/workloads/{id}/config` | Yes + `PushConfig` | Legacy direct push endpoint; now disabled and returns `410 Gone` with `code=config_approval_required`. |
| `GET` | `/api/workloads/{id}/config/approvals` | Yes + `PushConfig` | List config approval requests for the workload. |
| `POST` | `/api/workloads/{id}/config/approvals` | Yes + `PushConfig` | Create or update a validated config approval draft. |
| `POST` | `/api/workloads/{id}/config/approvals/{approval_id}/approve` | Yes + `PushConfig` | Approve a pending config approval draft. |
| `POST` | `/api/workloads/{id}/config/approvals/{approval_id}/push` | Yes + `PushConfig` | Push an approved draft, or an audited break-glass draft. |
| `POST` | `/api/workloads/{id}/config/validate` | Yes + `ValidateConfig` | Lightweight server-side validation of a config. |
| `DELETE` | `/api/workloads/{id}` | Yes | Archive a workload (admin only). |
| `GET` | `/api/configs` | Yes | List all configs. |
| `POST` | `/api/configs` | Yes | Create a new config. |
| `GET` | `/api/configs/{id}` | Yes | Fetch a config by ID. |
| `GET` | `/api/alerts` | Yes | List active alerts. |
| `POST` | `/api/alerts/{id}/resolve` | Yes | Resolve an alert. |
| `POST` | `/api/reports/evidence-pack` | Yes | Preview a deterministic evidence-pack JSON payload. |
| `POST` | `/api/reports/evidence-pack/export` | Yes | Export an evidence pack as Markdown, CSV, or PDF (`?format=`). |
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

Response is an array of workload summaries. The exact fields are defined by `models.Workload` in `pkg/models/models.go`. Treat it as the source of truth; do not hand-maintain the shape here — link to the file from the rendered doc instead.

### `GET /api/workloads/{id}/instances`

Returns the live OpAMP-connected instances for a workload as an array. This endpoint is backed by the in-memory OpAMP registry, not the database; after a server restart it is empty until agents reconnect. If the workload has no live instances, the response is the JSON array `[]`, not `null`.

Each item uses `internal/opamp.Instance`:

| Field | Meaning |
|-------|---------|
| `instance_uid` | Stable OpAMP instance UID for this live connection. |
| `pod_name` | Kubernetes pod name when reported; omitted otherwise. |
| `version` | Collector/agent version when reported; omitted otherwise. |
| `connected_at` | UTC timestamp for when this instance entered the registry. |
| `last_message_at` | UTC timestamp for the latest OpAMP message from the instance. |
| `effective_config_hash` | Hash from the latest reported remote config status; omitted until known. |
| `healthy` | `true` when the registry considers this instance healthy. Any unhealthy live instance makes the workload aggregate `status` become `degraded`. |
| `accepts_remote_config` | Whether this instance reports OpAMP remote-config support. |
| `remote_config_status` | Per-instance `RemoteConfigStatus`; omitted until the instance reports one. Status values currently used here are `applying`, `applied`, and `failed`; error text is sanitized before JSON serialization. |

Compatibility notes:

- Existing clients can keep using `GET /api/workloads/{id}/instances`; its top-level response shape remains an array.
- The instance objects now include per-instance config fields (`effective_config_hash`, `accepts_remote_config`, and optional `remote_config_status`) in addition to the existing live identity/health fields. Clients should ignore unknown fields for forward compatibility.
- Consumers that need workload-level rollups should prefer `GET /api/workloads/{id}/topology` instead of recalculating mixed versions, config drift, or remote-config states from this array.

### `GET /api/workloads/{id}/topology`

Returns the same live instance array as `/instances`, wrapped with a versioned schema and a workload-level summary. This is the preferred contract for frontend topology views because it centralizes heterogeneity rules in the backend.

Response fields:

| Field | Meaning |
|-------|---------|
| `schema_version` | Contract identifier. Current value: `workload-topology.v1`. |
| `workload_id` | The workload id from the path. The endpoint currently returns a well-formed empty topology for unknown or disconnected workloads; it does not 404 just because there are no live instances. |
| `instances` | Live `internal/opamp.Instance[]`; same per-instance shape as `/instances`. Always an array. |
| `summary.connected_count` | Number of live instances in `instances`. |
| `summary.healthy_count` / `summary.unhealthy_count` | Health split across live instances. |
| `summary.drifted_count` | When two or more distinct non-empty `effective_config_hash` values are present, the number of instances not on the majority reported hash. `0` when there are zero or one distinct reported hashes. |
| `summary.version_diversity` | Sorted unique non-empty versions reported by instances. Always an array. |
| `summary.config_hash_diversity` | Sorted unique non-empty effective config hashes reported by instances. Always an array. |
| `summary.remote_config_status_counts` | Object with stable keys: `capable`, `no_status`, `sent`, `applying`, `applied`, `failed`. `capable` counts instances with `accepts_remote_config=true`; the other keys count the latest per-instance remote-config state, with `no_status` used when `remote_config_status` is absent or has an empty `status`. |
| `summary.heterogeneity` | Object with stable boolean flags: `mixed_versions`, `mixed_effective_config_hashes`, `unhealthy_instances`, `mixed_remote_config_statuses`, `applying_remote_config`, `failed_remote_config`. |
| `summary.heterogeneity_reasons` | Ordered list of the `true` heterogeneity flags, in backend evaluation order. Always an array. |

Non-empty mixed topology example:

```json
{
  "schema_version": "workload-topology.v1",
  "workload_id": "checkout-collector",
  "instances": [
    {
      "instance_uid": "uid-a",
      "pod_name": "checkout-collector-7c9d8f6f5f-a",
      "version": "0.98.0",
      "connected_at": "2026-07-02T11:57:00Z",
      "last_message_at": "2026-07-02T12:00:00Z",
      "effective_config_hash": "hash-a",
      "healthy": true,
      "accepts_remote_config": true,
      "remote_config_status": {
        "status": "applied",
        "config_hash": "hash-a",
        "updated_at": "2026-07-02T12:00:00Z"
      }
    },
    {
      "instance_uid": "uid-b",
      "pod_name": "checkout-collector-7c9d8f6f5f-b",
      "version": "0.99.0",
      "connected_at": "2026-07-02T11:58:00Z",
      "last_message_at": "2026-07-02T12:00:00Z",
      "effective_config_hash": "hash-b",
      "healthy": false,
      "accepts_remote_config": true,
      "remote_config_status": {
        "status": "failed",
        "config_hash": "hash-b",
        "error_message": "Remote config error details redacted",
        "updated_at": "2026-07-02T12:00:00Z"
      }
    },
    {
      "instance_uid": "uid-c",
      "pod_name": "checkout-collector-7c9d8f6f5f-c",
      "version": "0.99.0",
      "connected_at": "2026-07-02T11:59:00Z",
      "last_message_at": "2026-07-02T12:00:00Z",
      "effective_config_hash": "hash-b",
      "healthy": true,
      "accepts_remote_config": true,
      "remote_config_status": {
        "status": "applying",
        "config_hash": "hash-b",
        "updated_at": "2026-07-02T12:00:00Z"
      }
    },
    {
      "instance_uid": "uid-d",
      "pod_name": "checkout-collector-7c9d8f6f5f-d",
      "version": "0.99.0",
      "connected_at": "2026-07-02T11:59:30Z",
      "last_message_at": "2026-07-02T12:00:00Z",
      "effective_config_hash": "hash-b",
      "healthy": true,
      "accepts_remote_config": true
    }
  ],
  "summary": {
    "connected_count": 4,
    "healthy_count": 3,
    "unhealthy_count": 1,
    "drifted_count": 1,
    "version_diversity": ["0.98.0", "0.99.0"],
    "config_hash_diversity": ["hash-a", "hash-b"],
    "remote_config_status_counts": {
      "capable": 4,
      "no_status": 1,
      "sent": 0,
      "applying": 1,
      "applied": 1,
      "failed": 1
    },
    "heterogeneity": {
      "mixed_versions": true,
      "mixed_effective_config_hashes": true,
      "unhealthy_instances": true,
      "mixed_remote_config_statuses": true,
      "applying_remote_config": true,
      "failed_remote_config": true
    },
    "heterogeneity_reasons": [
      "mixed_versions",
      "mixed_effective_config_hashes",
      "unhealthy_instances",
      "mixed_remote_config_statuses",
      "applying_remote_config",
      "failed_remote_config"
    ]
  }
}
```

Empty topology example:

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

Intentional response shape changes introduced for topology work:

- New endpoint: `GET /api/workloads/{id}/topology`.
- `/topology` returns a wrapper object with `schema_version`, `workload_id`, `instances`, and `summary`; it does not replace the existing `/instances` array contract.
- The `/instances` item shape now exposes per-instance config status/capability fields so frontend consumers can render each pod's latest remote-config state without relying on workload-level aggregation.

### `POST /api/workloads/{id}/config`

Legacy direct config push is intentionally disabled after the P2.14 approval flow. Even callers with `workload:push_config` receive `410 Gone` and must create, approve, and push through `/api/workloads/{id}/config/approvals`. This keeps prod confirmation, approval status, break-glass reason, audit, and OpAMP push semantics on one server-enforced path instead of relying on UI-only routing.

Historical clients may still send the **raw YAML** body (no JSON wrapper), but the body is not validated, persisted, audited as `config.push`, or forwarded to OpAMP by this endpoint.

Response (410 Gone):

```json
{
  "error": "direct config push is disabled; create, approve, and push a config approval under /api/workloads/{id}/config/approvals",
  "code": "config_approval_required"
}
```

Use `POST /api/workloads/{id}/config/validate` for raw-YAML validation feedback before creating an approval draft.

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

Requires the same `workload:push_config` permission as the approval mutation endpoints because list responses include draft YAML and operator comments. Viewers or other users without that permission receive `403 Forbidden` and cannot retrieve `draft_yaml`, `request_comment`, `approval_comment`, `push_comment`, or `break_glass_reason` from this endpoint. Authorized callers receive `200 OK` with an array of `ConfigApprovalRequest`, newest first.

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
