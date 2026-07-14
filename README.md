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
- **Governed remote config** — request, approve, and push YAML configs to compatible workloads via OpAMP; new instances inherit the active config on connect
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
│                      │  │    PostgreSQL       │  │  │
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
| Frontend | React 19, TypeScript, Vite, Zustand, TanStack Query, CodeMirror 6 |
| Database | PostgreSQL 16+ |
| Auth | JWT (HS256), bcrypt |
| Deployment | Docker, Docker Compose, Helm |

## Quick Start

### Prerequisites

- Docker with Docker Compose v2
- `curl`, `jq`, and `openssl` for the automated activation smoke test

### Docker Compose activation

From a source checkout, create fresh credentials without storing them in a
file or copying a sample password:

```bash
export JWT_SECRET="$(openssl rand -hex 32)"
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export SEED_ADMIN_EMAIL="admin@example.invalid"
read -r -s -p "Initial admin password (minimum 12 characters): " SEED_ADMIN_PASSWORD
echo
export SEED_ADMIN_PASSWORD

docker compose --profile activation up --detach --build
```

Open `http://localhost:8080`, sign in, select `otelcol-activation-demo`, edit
the configuration, then use **Validate for this collector**, **Generate safety
plan**, **Request approval**, **Approve request**, and **Push approved config**.
The `activation-agent` service is a local OpAMP protocol simulator; it is not
an OpenTelemetry Collector and must not be used as one in production.

After the first successful login, remove the bootstrap credential from the
application container environment:

```bash
unset SEED_ADMIN_EMAIL SEED_ADMIN_PASSWORD
docker compose --profile activation up --detach --force-recreate otel-magnify
```

The complete cold path is automated and fails if it exceeds 15 minutes:

```bash
./scripts/activation-smoke.sh
```

The script generates ephemeral credentials, starts a fresh PostgreSQL volume,
creates and logs in as the first admin, discovers a workload, performs a
governed config push, verifies the OpAMP `applied` status, and cleans up.

### Development

```bash
# Backend
export JWT_SECRET="$(openssl rand -hex 32)"
DB_DSN="${DB_DSN:?set DB_DSN through your local secret workflow}" \
  go run ./cmd/server/

# Frontend (separate terminal)
cd frontend
npm install
npm run dev
```

The API runs on `:8080`, OpAMP on `:4320`, frontend dev server on `:5173` (proxied to backend).

### 5,000 collector load test

The local-only OpAMP benchmark requires an explicit confirmation and test-only
configuration values. It creates an isolated Compose project and never removes
Docker volumes:

```bash
export JWT_SECRET="$(openssl rand -hex 32)"
export OPAMP_SHARED_SECRET="$(openssl rand -hex 32)"
LOAD_TEST_CONFIRM=5000 \
  DB_DSN='required-but-ignored' \
  ./scripts/load-test-5000.sh
```

`DB_DSN` is required as an intent guard, but its value is intentionally ignored
and replaced with the fixed PostgreSQL DSN inside the isolated Compose project.

See [load testing](docs/operations/load-testing.md) for capacity prerequisites,
timing controls, output artifacts, and acceptance criteria.

### Kubernetes (Helm)

```bash
read -r -p "Released image version (without v prefix): " otel_magnify_version
helm install magnify helm/otel-magnify/ \
  --set image.tag="${otel_magnify_version:?pin a released image version}" \
  --set database.existingSecret=magnify-postgres \
  --set auth.existingSecret=magnify-auth \
  --set auth.seedAdmin.enabled=true \
  --set auth.seedAdmin.existingSecret=magnify-bootstrap
```

Create those Secrets through a secret manager or the [documented bootstrap
workflow](docs/users/installation.md#kubernetes-helm). Keep the durable JWT key
separate from the removable first-admin bootstrap Secret.

## Configuration

All configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_DSN` | *(required)* | PostgreSQL connection string |
| `DB_MAX_OPEN_CONNS` | `40` | Maximum PostgreSQL connections held open |
| `DB_MAX_IDLE_CONNS` | `10` | Maximum idle PostgreSQL connections retained |
| `DB_CONN_MAX_LIFETIME_SECONDS` | `1800` | Maximum lifetime for a pooled connection |
| `LISTEN_ADDR` | `:8080` | API server listen address |
| `OPAMP_ADDR` | `:4320` | OpAMP server listen address |
| `JWT_SECRET` | *(required)* | Secret key for JWT signing |
| `CORS_ORIGINS` | `http://localhost:5173` | Comma-separated allowed origins |
| `SEED_ADMIN_EMAIL` | *(optional)* | Bootstrap the first admin on an empty database; must be set with `SEED_ADMIN_PASSWORD` |
| `SEED_ADMIN_PASSWORD` | *(optional)* | Bootstrap password, minimum 12 characters; remove after first login |

## Connecting Agents

otel-magnify manages agents via the [OpAMP](https://opentelemetry.io/docs/specs/opamp/) protocol. Each agent must be configured to connect to the OpAMP WebSocket endpoint exposed on port `4320`.

The Collector's built-in OpAMP extension can report state but does not apply
remote configuration. Use the OpAMP Supervisor for a real remotely managed
Collector. For the local activation path, use the Compose `activation` profile
described above. See [Connecting agents](docs/users/connecting-agents.md) for
the supported distinction and workload identity rules.

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/auth/login` | No | Login, returns JWT |
| `GET` | `/api/workloads` | Yes | List all workloads |
| `GET` | `/api/workloads/:id` | Yes | Get workload details |
| `GET` | `/api/workloads/:id/instances` | Yes | Live OpAMP-connected pods for the workload |
| `GET` | `/api/workloads/:id/events` | Yes | Append-only pod-lifecycle log (Activity tab) |
| `GET` | `/api/workloads/:id/configs` | Yes | Workload config push history |
| `POST` | `/api/workloads/:id/config/approvals` | Yes | Request approval for a config draft |
| `POST` | `/api/workloads/:id/config/approvals/:approval_id/approve` | Yes | Approve a pending config draft |
| `POST` | `/api/workloads/:id/config/approvals/:approval_id/push` | Yes | Push an approved config to the workload |
| `GET` | `/api/configs` | Yes | List all configs |
| `POST` | `/api/configs` | Yes | Create a config |
| `GET` | `/api/configs/:id` | Yes | Get config by ID |
| `GET` | `/api/alerts` | Yes | List active alerts |
| `POST` | `/api/alerts/:id/resolve` | Yes | Resolve an alert |
| `GET` | `/ws` | Yes | Real-time WebSocket; browser sessions authenticate with the HttpOnly session cookie (`?token=` remains as a legacy compatibility fallback) |
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
