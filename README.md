# otel-magnify

![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)
![Status: pre-1.0](https://img.shields.io/badge/Status-pre--1.0-yellow.svg)
[![Docs](https://img.shields.io/badge/docs-mkdocs--material-blue.svg)](https://magnify-labs.github.io/otel-magnify/)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Magnify-Labs/otel-magnify/badge)](https://scorecard.dev/viewer/?uri=github.com/Magnify-Labs/otel-magnify)
<!-- Add once https://www.bestpractices.dev project is registered: -->
<!-- [![OpenSSF Best Practices](https://www.bestpractices.dev/projects/<PROJECT_ID>/badge)](https://www.bestpractices.dev/projects/<PROJECT_ID>) -->


→ [Documentation](https://magnify-labs.github.io/otel-magnify/) · [Roadmap](ROADMAP.md)

> **⚠️ Status: pre-1.0 — not recommended for critical deployments without commercial support**
>
> The REST API and OpAMP protocol are still being stabilized.
> Endpoints and data formats may change without notice between minor versions.
> For production use in a critical environment, [open an issue](https://github.com/magnify-labs/otel-magnify/issues).

Centralized management platform for OpenTelemetry agents via [OpAMP](https://opentelemetry.io/docs/specs/opamp/) (Open Agent Management Protocol).

Monitor, configure, and alert on your OTel Collectors and SDK agents from a single interface.

## Features

- **Workload inventory** — real-time view of every connected workload (Kubernetes Deployment/DaemonSet/StatefulSet/Job/CronJob, or host+service for non-K8s collectors and SDK agents), with status, version, labels, and live instance count
- **Remote config push** — edit YAML configs in-browser and push them to workloads via OpAMP; new pods inherit the active config on connect (P.2 auto-push)
- **Activity log** — append-only record of pod connect/disconnect/version transitions, per workload
- **Alert engine** — built-in rules for workload downtime, config drift (pushed config not applied), and version-outdated checks; webhook notifier for external delivery
- **Real-time updates** — WebSocket fan-out keeps the dashboard live without polling
- **Audit log** — security-relevant actions (login success/failure, password change, config create/push/rollback/label, workload archive/delete) are emitted through a pluggable `AuditLogger` interface; community defaults to a no-op sink, a persistent backend ships with the Enterprise edition. When the audit sink fails, handlers respond 503 with a `side_effect_status` body field (`applied` / `none`) so callers can reconcile.
- **Multi-deployment** — runs locally, in Docker Compose, or on Kubernetes via Helm

## Architecture

```text
┌─────────────────────────────────────────────────────┐
│                   otel-magnify                      │
│                                                     │
│  ┌──────────────┐    ┌──────────────────────────┐  │
│  │  React/Vite  │◄──►│     Go Backend           │  │
│  │  (frontend)  │    │  ┌────────────────────┐  │  │
│  │              │    │  │  OpAMP Server      │  │  │
│  │  REST + WS   │    │  │  (opamp-go)        │  │  │
│  └──────────────┘    │  └────────┬───────────┘  │  │
│                      │           │               │  │
│                      │  ┌────────▼───────────┐  │  │
│                      │  │  REST API + WS hub │  │  │
│                      │  └────────┬───────────┘  │  │
│                      │           │               │  │
│                      │  ┌────────▼───────────┐  │  │
│                      │  │  SQLite / Postgres  │  │  │
│                      │  └────────────────────┘  │  │
│                      └──────────────────────────┘  │
└─────────────────────────────────────────────────────┘
         ▲                         ▲
         │ OpAMP WebSocket         │ OpAMP WebSocket
    OTel Collectors          SDK Agents (Java/Python/Go)
```

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go, [chi](https://github.com/go-chi/chi), [opamp-go](https://github.com/open-telemetry/opamp-go), [goose](https://github.com/pressly/goose) |
| Frontend | React 18, TypeScript, Vite, Zustand, TanStack Query, CodeMirror 6 |
| Database | SQLite (dev) / PostgreSQL (prod) |
| Auth | JWT (HS256), bcrypt |
| Deployment | Docker, Docker Compose, Helm |

## Quick Start

### Prerequisites

- Go 1.22+
- Node.js 20+

### Development

```bash
# Backend
JWT_SECRET=dev-secret go run ./cmd/server/

# Frontend (separate terminal)
cd frontend
npm install
npm run dev
```

The API runs on `:8080`, OpAMP on `:4320`, frontend dev server on `:5173` (proxied to backend).

### Seed an admin user

```bash
SEED_ADMIN_EMAIL=admin@local SEED_ADMIN_PASSWORD=changeme JWT_SECRET=dev-secret go run ./cmd/server/
```

### Docker Compose

```bash
JWT_SECRET=mysecret docker compose up --build
```

App available at `http://localhost:8080`.

### Kubernetes (Helm)

```bash
helm install magnify helm/otel-magnify/ \
  --set jwtSecret=your-secret \
  --set config.dbDSN="postgres://user:pass@host:5432/magnify?sslmode=require"
```

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DRIVER` | `sqlite` | Database driver (`sqlite` or `pgx`) |
| `DB_DSN` | `otel-magnify.db` | Database connection string |
| `LISTEN_ADDR` | `:8080` | API server listen address |
| `OPAMP_ADDR` | `:4320` | OpAMP server listen address |
| `JWT_SECRET` | *(required)* | Secret key for JWT signing |
| `CORS_ORIGINS` | `http://localhost:5173` | Comma-separated allowed origins |
| `SEED_ADMIN_EMAIL` | *(optional)* | Create admin user on startup |
| `SEED_ADMIN_PASSWORD` | *(optional)* | Password for seed admin user |

## Connecting Agents

otel-magnify manages agents via the [OpAMP](https://opentelemetry.io/docs/specs/opamp/) protocol. Each agent must be configured to connect to the OpAMP WebSocket endpoint exposed on port `4320`.

### OTel Collector

Add the `opamp` extension to your Collector config and reference it in `service.extensions`:

```yaml
extensions:
  opamp:
    server:
      ws:
        endpoint: ws://<magnify-host>:4320/v1/opamp
        tls:
          insecure: true   # set to false with a valid certificate in production

service:
  extensions: [opamp]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
```

> The `opamp` extension is included in [opentelemetry-collector-contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib). Use `otel/opentelemetry-collector-contrib:0.98.0` or later.

Sample configs ready to use are available in [`agents/`](agents/).

### SDK Agent (Java / Python / Go)

SDK agents connect the same way — point the OpAMP client to the WebSocket endpoint:

**Java** (OpenTelemetry Java agent with OpAMP support):

```properties
otel.opamp.service.endpoint=ws://<magnify-host>:4320/v1/opamp
```

**Python** (`opamp-client` package):

```python
from opamp import OpAMPClient

client = OpAMPClient(
    server_url="ws://<magnify-host>:4320/v1/opamp",
)
client.start()
```

**Go** (`opamp-go` client):

```go
import "github.com/open-telemetry/opamp-go/client"

c := client.NewWebSocket(nil)
err := c.Start(context.Background(), client.StartSettings{
    OpAMPServerURL: "ws://<magnify-host>:4320/v1/opamp",
})
```

### Docker Compose (local demo)

When running with `docker compose`, agents on the same Docker network reach the OpAMP server at `ws://otel-magnify:4320/v1/opamp`:

```bash
docker run -d --name collector-demo --network otel-magnify_default \
  -v $(pwd)/agents/collector-prod-eu.yaml:/etc/otelcol-contrib/config.yaml \
  otel/opentelemetry-collector-contrib:0.98.0
```

Once connected, agents are grouped into workloads and appear automatically in the **Inventory** page. See [Workload identity](https://magnify-labs.github.io/otel-magnify/users/connecting-agents/#workload-identity) for how the grouping works.

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/auth/login` | No | Login, returns JWT |
| `GET` | `/api/workloads` | Yes | List all workloads |
| `GET` | `/api/workloads/:id` | Yes | Get workload details |
| `GET` | `/api/workloads/:id/instances` | Yes | Live OpAMP-connected pods for the workload |
| `GET` | `/api/workloads/:id/topology` | Yes | Live instances plus workload topology/heterogeneity summary |
| `GET` | `/api/workloads/:id/events` | Yes | Append-only pod-lifecycle log (Activity tab) |
| `GET` | `/api/workloads/:id/configs` | Yes | Workload config push history |
| `POST` | `/api/workloads/:id/config` | Yes | Push config to workload |
| `GET` | `/api/configs` | Yes | List all configs |
| `POST` | `/api/configs` | Yes | Create a config |
| `GET` | `/api/configs/:id` | Yes | Get config by ID |
| `GET` | `/api/alerts` | Yes | List active alerts |
| `POST` | `/api/alerts/:id/resolve` | Yes | Resolve an alert |
| `GET` | `/ws?token=xxx` | Yes | Real-time WebSocket |
| `GET` | `/healthz` | No | Health check |

> Legacy `/api/agents/*` paths still resolve — they reply with HTTP `307 Temporary Redirect` to the matching `/api/workloads/*` endpoint for backwards compatibility.

## Project Structure

```text
cmd/server/         # Entrypoint
internal/
├── api/            # REST handlers, WebSocket hub, static serving
├── alerts/         # Alert engine (30s evaluation loop)
├── auth/           # JWT generation, validation, middleware
├── config/         # Env-based configuration
├── opamp/          # OpAMP server (agent registry, config push)
└── store/          # Database layer + SQL migrations
pkg/models/         # Shared data types

frontend/
├── src/
│   ├── api/        # REST + WebSocket clients
│   ├── components/ # Layout, workloads/*, config/*
│   ├── pages/      # Dashboard, Workloads (inventory), WorkloadDetail, Configs, Alerts, Login
│   └── store/      # Zustand state management

helm/otel-magnify/  # Kubernetes Helm chart
go.mod              # Go module root (github.com/magnify-labs/otel-magnify)
```

## License

Copyright 2026 Valentin Momboeuf. Licensed under the [Apache License, Version 2.0](LICENSE).

Contributions are accepted under the [Developer Certificate of Origin](https://developercertificate.org) — see [CONTRIBUTING.md](CONTRIBUTING.md).
