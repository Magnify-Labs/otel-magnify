# Alerts

otel-magnify evaluates workload alerts in the backend and broadcasts alert changes to the UI. The alert engine runs every 30 seconds from `pkg/server` and stores alerts until they are resolved.

## Built-in rules

| Rule | Severity | When it fires | How it resolves |
|------|----------|---------------|-----------------|
| `workload_down` | `critical` | A workload is disconnected after the configured grace window. | The workload reconnects or an operator resolves it. |
| `config_drift` | `warning` | A workload's remote config status indicates a pushed config is not the effective applied config. | The workload reports the expected config as applied or an operator resolves it. |
| `version_outdated` | `warning` | `MIN_AGENT_VERSION` is set and the workload reports a lower semantic `service.version`. | The workload reports an equal/newer version, `MIN_AGENT_VERSION` is unset, or an operator resolves it. |

Invalid or missing workload versions do not fire `version_outdated`; they are treated as unknown rather than stale.

## Webhook delivery

Set `WEBHOOK_URL` to POST every newly fired alert to an external receiver. The notifier sends JSON with this shape:

```json
{
  "event": "alert_fired",
  "fired_at": "2026-07-07T12:00:00Z",
  "alert": {
    "id": "alert-123",
    "workload_id": "k8s:prod:deployment:checkout",
    "rule": "workload_down",
    "severity": "critical",
    "message": "Workload k8s:prod:deployment:checkout is down",
    "fired_at": "2026-07-07T12:00:00Z"
  }
}
```

Webhook calls use a 10-second timeout. Delivery failures and HTTP 4xx/5xx responses are logged by the server; they do not block alert persistence.

Treat `WEBHOOK_URL` as sensitive when it embeds credentials.

## Resolving alerts

Operators can resolve an alert from the UI or with `POST /api/alerts/{id}/resolve`. Resolved alerts are hidden from the default alerts list; use `GET /api/alerts?include_resolved=true` when you need history.
