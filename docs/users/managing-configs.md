# Managing configs

otel-magnify stores configurations centrally and pushes them to connected workloads over OpAMP. Each push is recorded with its hash, the operator who triggered it, the reported status, and the error message (if any). Configs are pushed to a **workload**, not to an individual pod — every live instance of the workload receives the push, and any new pod that connects later is immediately brought in line with the active config (P.2 auto-push).

## Workflow

1. Open a workload from the **Inventory** page.
2. Edit the YAML in the embedded CodeMirror editor.
3. Click **Validate** — the backend runs a light structural check and blocks the push if errors are found. Errors are listed inline.
4. Click **Push** to send the configuration to every live instance of the workload.
5. Each instance reports a `RemoteConfigStatus` — the UI aggregates them and updates live via WebSocket.

## Validation

The `POST /api/workloads/{id}/config/validate` endpoint returns an enriched validation result with stable `checks[]` for YAML syntax, Collector structure, component availability, target-version compatibility, and optional `otelcol` runtime validation. Runtime validation is enabled only when the server is configured with `OTELCOL_RUNTIME_VALIDATION_ENABLED=true`; if the binary is absent or runtime validation is disabled, the response includes a warning/skipped check but push can continue when no error-severity checks fail. The agent/Collector remains the ultimate authority after push.

## Auto-rollback

When a workload reports a `failed` status, otel-magnify automatically re-pushes the last known-good configuration. The rollback is recorded as a **new** `workload_configs` row (status `pending`, `pushed_by = "auto-rollback"`) — the failed row is left in place for auditing. An `auto_rollback_applied` event is broadcast on the WebSocket.

## Push history

Every push is stored in the `workload_configs` table with:

- Config content hash
- Operator (`pushed_by` — the user's email, or `auto-rollback` for automated recoveries)
- Timestamp
- Status (`pending`, `applied`, or `failed`)
- Error message if the agent rejected the config

The history is visible from the workload detail page.
