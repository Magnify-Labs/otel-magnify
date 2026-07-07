# OpAMP endpoint

otel-magnify accepts OpAMP client connections on a dedicated WebSocket listener. The default endpoint is:

```text
ws://<host>:4320/v1/opamp
```

The listen address is configured with `OPAMP_ADDR`; the path is always `/v1/opamp`.

## Authentication and transport

Set `OPAMP_SHARED_SECRET` to require an HTTP `Authorization` header during the WebSocket handshake:

```http
Authorization: Bearer <shared-secret>
```

Missing or mismatched tokens receive `401 Unauthorized` before OpAMP messages are processed. Leaving `OPAMP_SHARED_SECRET` empty preserves unauthenticated local/dev connections.

The community server speaks plain WebSocket. Terminate TLS at a reverse proxy or load balancer for production deployments, and keep the OpAMP listener scoped to trusted agents/networks where possible.

## Capabilities used by otel-magnify

| Capability | How it is used |
|------------|----------------|
| `ReportsEffectiveConfig` | Tracks the effective config hash reported by each instance. |
| `ReportsRemoteConfig` / `AcceptsRemoteConfig` | Records whether the workload can accept remote config; the UI/API gate push affordances and return `409 remote_config_unsupported` when a workload cannot accept it. |
| `ReportsHealth` | Updates live instance health and workload status. |
| `ReportsAvailableComponents` | Captures Collector receivers/processors/exporters/extensions and feeds config validation. |

Remote config status error messages are sanitized before they are persisted, broadcast, or rendered. See [Remote config status redaction](../security/remote-config-status-redaction.md) for the boundary.

## Workload identity

OpAMP `AgentDescription` resource attributes are mapped into otel-magnify workloads. Fingerprinting prefers Kubernetes workload identity, then host/service identity, then instance UID. See [Workload identity](../users/connecting-agents.md#workload-identity) for the exact attribute strategies.

Agent type detection uses `service.name` patterns: Collector-like names such as `otelcol*` are shown as collectors, while other OpAMP clients are shown as SDK agents.
