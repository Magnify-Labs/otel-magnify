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
