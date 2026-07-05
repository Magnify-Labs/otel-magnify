# Installation

otel-magnify ships as a single binary that embeds the frontend. Three supported deployment paths:

## Docker Compose

The simplest way to run otel-magnify locally or on a single host.

```bash
JWT_SECRET=$(openssl rand -base64 32) docker compose up --build
```

The API and embedded frontend are served at `http://localhost:8080`. The OpAMP endpoint listens on `:4320`.

This local command leaves `OPAMP_SHARED_SECRET` unset, so demo collectors can connect without OpAMP bearer authentication. If `:4320` is reachable outside your trusted local machine or Docker network, set `OPAMP_SHARED_SECRET` and pass the same value to each OpAMP client.

To enable PostgreSQL persistence:

```bash
DB_DRIVER=pgx \
  DB_DSN="postgres://magnify:magnify@postgres:5432/magnify?sslmode=disable" \
  docker compose --profile postgres up
```

## Kubernetes (Helm)

```bash
helm install magnify helm/otel-magnify/ \
  --set jwtSecret=your-secret \
  --set opampSharedSecret=replace-with-a-random-opamp-token \
  --set config.dbDSN="postgres://user:***@host:5432/magnify?sslmode=require"
```

See the chart values under `helm/otel-magnify/values.yaml` for ingress, resources, and persistence options.

## Native binary

Build from source:

```bash
go build -o otel-magnify ./cmd/server/
JWT_SECRET=$(openssl rand -base64 32) ./otel-magnify
```

## Seed an admin user on first start

```bash
SEED_ADMIN_EMAIL=admin@local \
  SEED_ADMIN_PASSWORD=changeme \
  JWT_SECRET=local-dev-secret-at-least-32-chars!! \
  ./otel-magnify
```

## Prerequisites

- Go 1.22+ (for building from source)
- Node.js 20+ (for frontend development only — the production binary embeds the pre-built frontend)
- A database: SQLite ships by default; PostgreSQL is recommended for multi-instance deployments.
