# OpAMP endpoint

> **Status:** Stub — to be expanded.

## Planned content

- Default endpoint: `ws://<host>:4320/v1/opamp`. Configurable via `OPAMP_ADDR`.
- Capabilities currently honored by otel-magnify:
    - `ReportsEffectiveConfig`
    - `ReportsRemoteConfig` / `AcceptsRemoteConfig` — `AcceptsRemoteConfig` is persisted on the `workloads` table as `accepts_remote_config` and exposed in the Workload JSON; the frontend uses it to gate Edit affordances and the API returns `409 Conflict` (`code: remote_config_unsupported`) on `POST /api/workloads/{id}/config` when it's false.
    - `ReportsHealth`
    - `ReportsAvailableComponents`
- `AvailableComponents` capture and how it is used to validate pushed configs (see commit `5bf11da`).
- Mapping from OpAMP `AgentDescription` fields to the otel-magnify workload model, including the workload fingerprint strategies (`k8s` → `host` → `uid`) — see [Workload identity](../users/connecting-agents.md#workload-identity).
- Agent type detection via `service.name` (collectors vs SDK agents).
- Authentication: set `OPAMP_SHARED_SECRET` to require an `Authorization` header using the bearer token scheme on the HTTP/WebSocket handshake. Missing or mismatched tokens receive `401 Unauthorized`; empty `OPAMP_SHARED_SECRET` preserves local/dev unauthenticated connections.
- TLS: currently served plain-WS; terminate TLS at a reverse proxy for production.
