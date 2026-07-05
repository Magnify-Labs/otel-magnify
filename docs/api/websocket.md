# WebSocket

The WebSocket hub at `/ws` streams real-time events to browsers and backend integrations. Browser clients authenticate with the `om_session` HttpOnly session cookie set by `/api/auth/login`, so the JWT is not placed in the WebSocket URL. The legacy `/ws?token=<jwt>` form remains available for non-browser or older clients that cannot send the session cookie.

## Message envelope

Every message is a JSON object with a `type` discriminator. Additional fields are flat on the root — there is no `payload` wrapper:

```json
{
  "type": "workload_update",
  "workload": { /* models.Workload */ },
  "connected_instance_count": 3,
  "drifted_instance_count": 0
}
```

## Event types

| Type | When it fires | Other fields |
|------|---------------|--------------|
| `workload_update` | A workload is registered or its state changes (status, labels, version, active config, live instance count). | `workload` — full `models.Workload`; `connected_instance_count`; `drifted_instance_count`. |
| `workload_event` | A single append-only pod-lifecycle event is recorded (connect / disconnect / version change). | `event` — full `models.WorkloadEvent`. |
| `workload_config_status` | A workload reports a `RemoteConfigStatus` for a pushed config (aggregated across its instances). | `workload_id`, `status` — `{ status, config_hash, error_message, updated_at }`. |
| `alert_update` | An alert transitions (fired, acknowledged, resolved). | `alert` — full `models.Alert`. |
| `auto_rollback_applied` | The server auto-pushed the last-known-good config after a failure. | `workload_id`, `from_hash`, `to_hash`, `reason`. |

For authoritative payload shapes, consult `internal/api/wshub.go` (or the matching file in the current revision).

## Reconnect behavior

The browser client reconnects with exponential backoff capped at a few seconds. Integrations should do the same and treat any missed events as eventually reconciled by a fresh `GET /api/workloads` call.
