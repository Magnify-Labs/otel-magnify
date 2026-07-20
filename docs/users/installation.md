# Installation

otel-magnify ships as a single binary that embeds the frontend. Three deployment paths are supported: Docker Compose, Kubernetes with Helm, and a native binary.

## Prerequisites

- Go version compatible with `go.mod` when building from source.
- Node.js/npm only when rebuilding the frontend locally. The production binary embeds pre-built frontend assets.
- PostgreSQL 18.x. `DB_DSN` is required by the binary.
- A strong `JWT_SECRET`. Startup fails when it is missing, too short, or still set to the production placeholder.

## PostgreSQL 18 support boundary

PostgreSQL 18.x is the only supported server version. An existing v0.7.1
database on PostgreSQL 16 must be dumped or migrated into a distinct
PostgreSQL 18 instance and validated with the same v0.7.1 artifact before the
application is upgraded. Never reuse a PostgreSQL 16 physical data directory
with PostgreSQL 18. Follow the [PostgreSQL lifecycle
runbook](../operations/postgresql-lifecycle.md) for backup, upgrade, and
rollback.

## Docker Compose

The supported cold-start path from a source checkout is Docker Compose. It
includes an opt-in activation workload that can acknowledge remote config:

```bash
export JWT_SECRET="$(openssl rand -hex 32)"
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export SEED_ADMIN_EMAIL="admin@example.invalid"
read -r -s -p "Initial admin password (minimum 12 characters): " SEED_ADMIN_PASSWORD
echo
export SEED_ADMIN_PASSWORD

docker compose --profile activation up --detach --build
```

The API and embedded frontend are served at `http://localhost:8080`. The OpAMP endpoint listens on `:4320` and is available to containers on the Compose network at `ws://otel-magnify:4320/v1/opamp`.

Sign in, open the `otelcol-activation-demo` workload, edit and validate the
config, generate its safety plan, request approval, approve it, and push it.
The local activation agent reports the final OpAMP status as `applied`. It is a
protocol simulator, not a Collector runtime.

After the first successful login, recreate the application without its
bootstrap credential:

```bash
unset SEED_ADMIN_EMAIL SEED_ADMIN_PASSWORD
docker compose --profile activation up --detach --force-recreate otel-magnify
```

To verify the whole path automatically on a clean PostgreSQL volume:

```bash
./scripts/activation-smoke.sh
```

The smoke test generates ephemeral credentials, requires the Community
approval and policy-preview features, and fails unless `/readyz` returns
exactly `ready`, the database reports PostgreSQL major 18, and first-admin
login, workload discovery, approval, push, and the `applied` acknowledgement
complete within 15 minutes. It removes its isolated containers, network,
volume, and credential files on exit.

Compose defaults:

- `DB_DSN=postgres://magnify:***@postgres:5432/magnify?sslmode=disable`
- `CORS_ORIGINS=http://localhost:8080`
- PostgreSQL data persisted in a fresh `pg18-data` Docker volume, mounted at
  `/var/lib/postgresql` with `PGDATA=/var/lib/postgresql/18/docker`
- `OPAMP_SHARED_SECRET` empty unless you set it in the shell environment

Do not use sample password values from docs in a shared environment. Do not
run `docker compose down --volumes` against an older project until its database
has been migrated, validated on PostgreSQL 18, and backed up.

### Published GHCR image

Once a release containing this activation path is published and the package is
public, the same smoke test can verify an anonymous image pull before starting:

```bash
read -r -p "Released image version (without v prefix): " released_version
OTEL_MAGNIFY_IMAGE="ghcr.io/magnify-labs/otel-magnify:${released_version}" \
  ./scripts/activation-smoke.sh
```

The script uses an empty temporary Docker configuration for the pull, so a
developer's cached GitHub credentials cannot produce a false positive.

If GHCR returns `denied`, an organization owner must perform this external
GitHub action; the repository cannot change package visibility from code:

1. Open **Magnify-Labs → Settings → Packages**.
2. Under **Package Creation**, enable **Public**. If this setting is locked, an
   organization or Enterprise owner must change the higher-level policy.
3. Open **Magnify-Labs → Packages → otel-magnify → Package settings**.
4. In **Danger Zone**, select **Change visibility**, choose **Public**, and type
   the package name to confirm.
5. Optionally disable public package creation again at the organization level;
   the existing `otel-magnify` package remains public.

GitHub documents this under [Configuring a package's access control and
visibility](https://docs.github.com/en/packages/learn-github-packages/configuring-a-packages-access-control-and-visibility).
Changing a private package to public cannot be undone. Verify the result with
an empty Docker configuration and a released tag, not with `latest`:

```bash
read -r -p "Released image version (without v prefix): " released_version
anonymous_config="$(mktemp -d)"
DOCKER_CONFIG="${anonymous_config}" \
  docker pull "ghcr.io/magnify-labs/otel-magnify:${released_version}"
rm -rf "${anonymous_config}"
```

## Kubernetes (Helm)

Create three operator-managed Secrets: one for the PostgreSQL DSN, one for the
durable JWT signing key, and a separate Secret for the removable first-admin
bootstrap credential. Prefer External Secrets or your platform secret manager
in a shared cluster. The following local example avoids putting secret values
in Helm values, shell history, or process arguments:

```bash
set -euo pipefail

namespace="otel-magnify"
secret_directory="$(mktemp -d)"
chmod 700 "${secret_directory}"
trap 'rm -rf "${secret_directory}"' EXIT

kubectl create namespace "${namespace}" --dry-run=client -o yaml | kubectl apply -f -
read -r -s -p "PostgreSQL DSN: " database_dsn
echo
read -r -s -p "Initial admin password (minimum 12 characters): " seed_admin_password
echo
read -r -p "Released image version (without v prefix): " released_version
printf '%s' "${database_dsn}" >"${secret_directory}/db-dsn"
printf '%s' "$(openssl rand -hex 32)" >"${secret_directory}/jwt-secret"
printf '%s' 'admin@example.invalid' >"${secret_directory}/seed-admin-email"
printf '%s' "${seed_admin_password}" >"${secret_directory}/seed-admin-password"
unset database_dsn seed_admin_password

kubectl --namespace "${namespace}" create secret generic magnify-postgres \
  --from-file="${secret_directory}/db-dsn" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl --namespace "${namespace}" create secret generic magnify-auth \
  --from-file="${secret_directory}/jwt-secret" \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl --namespace "${namespace}" create secret generic magnify-bootstrap \
  --from-file="${secret_directory}/seed-admin-email" \
  --from-file="${secret_directory}/seed-admin-password" \
  --dry-run=client -o yaml | kubectl apply -f -

helm install magnify helm/otel-magnify/ \
  --namespace "${namespace}" \
  --set image.tag="${released_version:?pin a released image version}" \
  --set database.existingSecret=magnify-postgres \
  --set auth.existingSecret=magnify-auth \
  --set auth.seedAdmin.enabled=true \
  --set auth.seedAdmin.existingSecret=magnify-bootstrap
```

After the first successful login, disable the seed environment references and
delete only the bootstrap Secret. Keep `magnify-auth`: changing or losing the
JWT signing key invalidates every active session.

```bash
helm upgrade magnify helm/otel-magnify/ \
  --namespace otel-magnify \
  --reuse-values \
  --set auth.seedAdmin.enabled=false
kubectl --namespace otel-magnify delete secret magnify-bootstrap
```

The chart creates:

- one `Deployment`
- one `Service` exposing named ports `api` and `opamp`
- an optional release `Secret` only when a legacy inline value is supplied
- no release `db-dsn` credential when `database.existingSecret` is set; the `Deployment` reads the operator-managed Secret and `database.existingSecretKey`
- no release JWT credential when `auth.existingSecret` is set; the `Deployment` reads `auth.jwtSecretKey`
- optional seed-admin references from a separate Secret only while `auth.seedAdmin.enabled=true`
- an optional `Ingress` for the API/frontend only

Important values:

| Value | Default | Notes |
|-------|---------|-------|
| `replicaCount` | `1` | Must remain `1` while the OpAMP registry and live connections are process-local. |
| `deployment.secretRevision` | empty | Change after rotating an operator-managed Secret to restart the pod. |
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
| `database.connMaxIdleTimeSeconds` | `300` | Maximum idle time for a pooled connection. |
| `database.connMaxLifetimeSeconds` | `1800` | Maximum lifetime for a pooled connection. |
| `config.corsOrigins` | empty | Passed to `CORS_ORIGINS`. Set this to your external UI origin when using ingress. |
| `auth.existingSecret` | empty | Operator-managed Secret containing the durable JWT signing key; required for the supported install path. |
| `auth.jwtSecretKey` | `jwt-secret` | Key holding the JWT signing key in `auth.existingSecret`. |
| `auth.seedAdmin.enabled` | `false` | Inject the bootstrap email and password from a separate Secret. Disable after first login. |
| `auth.seedAdmin.existingSecret` | empty | Secret containing the seed email and password keys; required when bootstrap is enabled. |
| `jwtSecret` | empty | Legacy inline fallback that renders a release Secret; avoid for new installations. |
| `opampSharedSecret` | empty | Stored in the generated Kubernetes Secret as `opamp-shared-secret`; leave empty only for trusted local/internal OpAMP networks. |
| `automountServiceAccountToken` | `false` | The binary does not call the Kubernetes API. |
| `podSecurityContext` / `containerSecurityContext` | hardened non-root defaults | Keep these defaults unless your runtime requires a documented exception. |

### Deployment availability and probes

The chart deliberately supports one replica only while the OpAMP registry and
live collector connections are process-local. It rejects any `replicaCount`
other than `1`. The Deployment uses the `Recreate` strategy, so an upgrade has
brief downtime and connected collectors must reconnect after the replacement
pod starts.

The startup and liveness probes call `/healthz`, which checks that the process
is alive. The readiness probe calls `/readyz`, which depends on PostgreSQL and
keeps the pod out of Service endpoints while the database is unavailable.

For an operator-managed external Secret, first apply the updated Secret and
then change `deployment.secretRevision` in the Helm release. This explicit pod
template change triggers a restart; the chart does not read external Secret
contents during rendering. For example:

```bash
kubectl --namespace otel-magnify apply -f magnify-secrets.yaml
helm upgrade magnify helm/otel-magnify/ \
  --namespace otel-magnify \
  --reuse-values \
  --set-string deployment.secretRevision="rev-2"
```

`helm upgrade --atomic` can roll back Kubernetes release objects after a failed
upgrade, but it never restores PostgreSQL schemas or data. Back up PostgreSQL
and verify recovery independently before an application upgrade.

### Helm security caveats

- Passing secrets with `--set` can expose them in shell history and Helm release state. Use pre-created Secrets for the database, JWT key, and first-admin credential.
- When `database.dsn` is set without `database.existingSecret`, Helm creates the release Secret with the `db-dsn` credential. When `database.existingSecret` is set, Helm does not render a release `db-dsn`; protect the operator-managed Secret and namespace read access accordingly.
- Store the JWT signing key separately from the seed-admin credential. The former is durable; the latter should be deleted immediately after bootstrap.
- The default ingress exposes only the API/frontend. OpAMP is a separate service port and should be exposed deliberately, with network policy, an internal load balancer, and `OPAMP_SHARED_SECRET` when possible.
- `readOnlyRootFilesystem` is enabled. The application uses `/tmp` only for temporary files; database state belongs to PostgreSQL.
- `automountServiceAccountToken=false` should stay disabled unless an extension binary actually needs Kubernetes API access.

### Helm secret references

Reference Secrets created by your secret manager without copying their content
into a Helm values file:

```yaml
database:
  existingSecret: magnify-postgres
  existingSecretKey: db-dsn
auth:
  existingSecret: magnify-auth
  jwtSecretKey: jwt-secret
  seedAdmin:
    enabled: true
    existingSecret: magnify-bootstrap
    emailKey: seed-admin-email
    passwordKey: seed-admin-password
```

## Native binary

Build from source:

```bash
go build -o otel-magnify ./cmd/server/
DB_DSN="${DB_DSN:?set DB_DSN through your secret workflow}" \
  JWT_SECRET="$(openssl rand -hex 32)" ./otel-magnify
```

For local development with an initial admin:

```bash
export JWT_SECRET="$(openssl rand -hex 32)"
export SEED_ADMIN_EMAIL="admin@example.invalid"
read -r -s -p "Initial admin password (minimum 12 characters): " SEED_ADMIN_PASSWORD
echo
export SEED_ADMIN_PASSWORD
DB_DSN="${DB_DSN:?set DB_DSN through your secret workflow}" ./otel-magnify
```

## Seed an admin user on first start

Both `SEED_ADMIN_EMAIL` and `SEED_ADMIN_PASSWORD` must be set, and the password
must contain at least 12 characters. Bootstrap succeeds only when the users
table is empty. A restart with the same already-administrator email is
idempotent and does not reset its password. Any other existing user or a
same-email non-admin makes startup fail closed.

Use this as an initial bootstrap mechanism only. After first login, rotate the password through the application or your operational process and remove the seed variables from the runtime environment.

## Post-install smoke checks

```bash
curl -fsS http://localhost:8080/healthz
curl -fsS http://localhost:8080/readyz
curl -fsS http://localhost:8080/api/v1/capabilities
curl -fsS http://localhost:8080/api/auth/methods
```

Expected unauthenticated responses:

- `/healthz` returns `ok`.
- `/readyz` returns `ready` when PostgreSQL is reachable.
- `/api/v1/capabilities` is the canonical public capability-discovery endpoint and returns only the Community `config_safety.approvals` and `config_safety.policy_preview` capabilities in this release.
- `/api/auth/methods` lists the password login method by default.

`GET /api/features` remains a legacy boolean compatibility endpoint; it is not the smoke-check path for new integrations. Capability discovery is not authorization: protected APIs still enforce authentication, RBAC, and server-side gates. For edition binary maintainers, `WithCapabilities` is preferred for typed declarations; `WithFeatures` remains supported for legacy edition overlays.

## Production checklist

Before exposing otel-magnify beyond a developer machine:

- Generate a strong `JWT_SECRET`; do not reuse docs/examples.
- Use PostgreSQL 18.x with `sslmode=verify-full`, a trusted root certificate,
  and hostname verification.
- Configure and rehearse PostgreSQL backups and recovery, and budget connection
  limits for the expected workload. Community makes no PITR, RPO, or RTO
  promise; follow the [lifecycle runbook](../operations/postgresql-lifecycle.md).
- Set `CORS_ORIGINS` to the exact browser origin(s) that should access the API.
- Serve the API/frontend and WebSocket hub over TLS.
- Treat legacy WebSocket URLs containing `?token=` as sensitive; browser clients should normally use the `om_session` HttpOnly cookie on `/ws`.
- Restrict OpAMP exposure to trusted agents/networks and configure `OPAMP_SHARED_SECRET` when OpAMP crosses a shared or exposed boundary.
- Review any Collector YAML before sharing it publicly; exporter configs often contain credentials.
