# Installation

otel-magnify ships as a single binary that embeds the frontend. Three deployment paths are supported: Docker Compose, Kubernetes with Helm, and a native binary.

## Prerequisites

- Go version compatible with `go.mod` when building from source.
- Node.js/npm only when rebuilding the frontend locally. The production binary embeds pre-built frontend assets.
- PostgreSQL 16 or later. `DB_DSN` is required by the binary.
- A strong `JWT_SECRET`. Startup fails when it is missing, too short, or still set to the production placeholder.

## Docker Compose

The simplest local or single-host path is Docker Compose:

```bash
JWT_SECRET="$(openssl rand -hex 32)" \
  POSTGRES_PASSWORD="$(openssl rand -hex 24)" docker compose up --build
```

The API and embedded frontend are served at `http://localhost:8080`. The OpAMP endpoint listens on `:4320` and is available to containers on the Compose network at `ws://otel-magnify:4320/v1/opamp`.

Compose defaults:

- `DB_DSN=postgres://magnify:***@postgres:5432/magnify?sslmode=disable`
- `CORS_ORIGINS=http://localhost:8080`
- PostgreSQL data persisted in the `pg-data` Docker volume
- `OPAMP_SHARED_SECRET` empty unless you set it in the shell environment

Do not use sample password values from docs in a shared environment.

## Kubernetes (Helm)

Install from the in-repo chart:

```bash
helm install magnify helm/otel-magnify/ \
  --set jwtSecret="$(openssl rand -hex 32)" \
  --set database.dsn="postgres://user:***@host:5432/magnify?sslmode=require" \
  --set opampSharedSecret="REPLACE_WITH_RANDOM_OPAMP_SHARED_SECRET"
```

The chart creates:

- one `Deployment`
- one `Service` exposing named ports `api` and `opamp`
- one release `Secret` containing the supplied application secrets; it contains `db-dsn` only when Helm creates the database credential from `database.dsn`
- no release `db-dsn` credential when `database.existingSecret` is set; the `Deployment` reads the operator-managed Secret and `database.existingSecretKey`
- an optional `Ingress` for the API/frontend only

Important values:

| Value | Default | Notes |
|-------|---------|-------|
| `replicaCount` | `1` | PostgreSQL supports single- and multi-replica deployments. |
| `image.repository` | `ghcr.io/magnify-labs/otel-magnify` | Container image repository. |
| `image.tag` | chart app version | Override to pin a release/image digest. |
| `service.type` | `ClusterIP` | Exposes both API and OpAMP service ports inside the cluster. |
| `service.apiPort` | `8080` | Service port for API, frontend, health, and browser WebSocket hub. |
| `service.opampPort` | `4320` | Service port for OpAMP clients. |
| `ingress.enabled` | `false` | Ingress routes to the API port only. Expose OpAMP separately if agents connect from outside the cluster. |
| `database.existingSecret` | empty | Operator-managed Secret containing the PostgreSQL DSN; preferred when a secret manager creates it. |
| `database.existingSecretKey` | `db-dsn` | Key holding the PostgreSQL DSN in `database.existingSecret`. |
| `database.dsn` | empty | Explicit DSN used to create the release Secret when `database.existingSecret` is empty. |
| `database.maxOpenConns` | `40` | Maximum PostgreSQL connections held open. |
| `database.maxIdleConns` | `10` | Maximum idle PostgreSQL connections retained. |
| `database.connMaxLifetimeSeconds` | `1800` | Maximum lifetime for a pooled connection. |
| `config.corsOrigins` | empty | Passed to `CORS_ORIGINS`. Set this to your external UI origin when using ingress. |
| `jwtSecret` | empty | Stored in the generated Kubernetes Secret as `jwt-secret`; must be set to a strong random value. |
| `opampSharedSecret` | empty | Stored in the generated Kubernetes Secret as `opamp-shared-secret`; leave empty only for trusted local/internal OpAMP networks. |
| `automountServiceAccountToken` | `false` | The binary does not call the Kubernetes API. |
| `podSecurityContext` / `containerSecurityContext` | hardened non-root defaults | Keep these defaults unless your runtime requires a documented exception. |

### Helm security caveats

- Passing secrets with `--set` can expose them in shell history. Prefer a local values file, your secret manager, or a pre-created Secret workflow for shared clusters.
- When `database.dsn` is set without `database.existingSecret`, Helm creates the release Secret with the `db-dsn` credential. When `database.existingSecret` is set, Helm does not render a release `db-dsn`; protect the operator-managed Secret and namespace read access accordingly.
- The default ingress exposes only the API/frontend. OpAMP is a separate service port and should be exposed deliberately, with network policy, an internal load balancer, and `OPAMP_SHARED_SECRET` when possible.
- `readOnlyRootFilesystem` is enabled. The application uses `/tmp` only for temporary files; database state belongs to PostgreSQL.
- `automountServiceAccountToken=false` should stay disabled unless an extension binary actually needs Kubernetes API access.

### Helm database secret examples

Let Helm create the release Secret only when you explicitly provide `database.dsn`:

```yaml
database:
  dsn: postgres://magnify:***@postgres.example:5432/magnify?sslmode=require
```

For an operator-managed Secret, create the Secret through your secret manager and reference its name and key:

```yaml
database:
  existingSecret: magnify-postgres
  existingSecretKey: url
```

## Native binary

Build from source:

```bash
go build -o otel-magnify ./cmd/server/
DB_DSN="postgres://magnify:***@postgres.example:5432/magnify?sslmode=require" \
  JWT_SECRET="$(openssl rand -hex 32)" ./otel-magnify
```

For local development with an initial admin:

```bash
DB_DSN="postgres://magnify:***@postgres.example:5432/magnify?sslmode=require" \
  SEED_ADMIN_EMAIL=admin@local SEED_ADMIN_PASSWORD=change-me-on-first-login \
  JWT_SECRET=dev-secret-at-least-32-bytes-long ./otel-magnify
```

## Seed an admin user on first start

If `SEED_ADMIN_EMAIL` and `SEED_ADMIN_PASSWORD` are set, startup creates the user when it does not already exist and attaches it to the `administrator` system group.

Use this as an initial bootstrap mechanism only. After first login, rotate the password through the application or your operational process and remove the seed variables from the runtime environment.

## Post-install smoke checks

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/api/features
curl -fsS http://localhost:8080/api/auth/methods
```

Expected unauthenticated responses:

- `/healthz` returns `ok`.
- `/api/features` returns `{ "features": {} }` in the community binary unless an edition binary registers feature flags.
- `/api/auth/methods` lists the password login method by default.

## Production checklist

Before exposing otel-magnify beyond a developer machine:

- Generate a strong `JWT_SECRET`; do not reuse docs/examples.
- Configure PostgreSQL backups, recovery, and connection limits for the expected workload.
- Set `CORS_ORIGINS` to the exact browser origin(s) that should access the API.
- Serve the API/frontend and WebSocket hub over TLS.
- Treat legacy WebSocket URLs containing `?token=` as sensitive; browser clients should normally use the `om_session` HttpOnly cookie on `/ws`.
- Restrict OpAMP exposure to trusted agents/networks and configure `OPAMP_SHARED_SECRET` when OpAMP crosses a shared or exposed boundary.
- Review any Collector YAML before sharing it publicly; exporter configs often contain credentials.
