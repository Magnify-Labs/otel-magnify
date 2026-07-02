# API — overview

otel-magnify exposes API integration surfaces through REST, WebSocket, and OpAMP:

- **[REST API](rest.md)** — JSON over HTTP for inventory, configs, alerts.
- **[Report export API](report-exports.md)** — report/evidence-pack request, response, export, signing, and error contract.
- **[WebSocket](websocket.md)** — live events from the server to browsers and integrations.
- **[OpAMP](opamp.md)** — the agent-management protocol on port `:4320`.

All HTTP APIs (except `POST /api/auth/login` and `GET /healthz`) require a bearer JWT. See [Authentication](authentication.md) for the login flow.

## Stability

otel-magnify is pre-1.0. REST payloads and WebSocket event shapes may change between minor versions. Pin your integration to a specific release tag.

## Base URL

```text
http://<host>:8080
```

All REST endpoints are under `/api/`. The WebSocket hub is at `/ws`. The OpAMP server runs on a separate port: `:4320` by default.
