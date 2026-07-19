# Backend

This page is the contributor quick-start for the Go backend. For the high-level system diagram, see [Architecture](architecture.md). For endpoint contracts, see [REST API](../api/rest.md).

## Local development setup

The Go module lives at the repository root:

```bash
go version
TEST_POSTGRES_DSN="${TEST_POSTGRES_DSN:?set a disposable PostgreSQL DSN}" go test ./...
```

Use the Go toolchain declared in `go.mod`; do not rely on older minimum versions from stale screenshots or blog posts.

The production binary embeds a pre-built frontend, but backend-only development can run without rebuilding frontend assets:

```bash
export JWT_SECRET="$(openssl rand -hex 32)"
export SEED_ADMIN_EMAIL="admin@example.invalid"
read -r -s -p "Initial admin password (minimum 12 characters): " SEED_ADMIN_PASSWORD
echo
export SEED_ADMIN_PASSWORD
DB_DSN="${DB_DSN:?set DB_DSN through your local secret workflow}" go run ./cmd/server/
```

Default listeners:

- API, health check, embedded SPA, and browser WebSocket hub: `http://localhost:8080`.
- OpAMP WebSocket server: `ws://localhost:4320/v1/opamp`.
- Browser WebSocket hub: `ws://localhost:8080/ws` using the `om_session` HttpOnly cookie; `/ws?token=<jwt>` remains a legacy fallback.

For frontend development, run Vite separately from `frontend/`; the default backend CORS origin allows `http://localhost:5173`.

## Package map

| Package | Responsibility |
|---------|----------------|
| `cmd/server/` | Process entrypoint. Loads env config, opens the store, runs migrations, wires auth/server/bootstrap. |
| `internal/config/` | Environment-variable parsing and defaults. |
| `pkg/bootstrap/` | Runtime bootstrap that can be reused by alternate binaries; enforces required `JWT_SECRET` and seeds the first admin when requested. |
| `pkg/server/` | Composes store, auth, router hooks, alert notifier(s), audit logger, feature flags, static assets, OpAMP server, alert engine, and workload janitor. |
| `internal/api/` | chi router, REST handlers, browser WebSocket hub, auth/permission middleware adapters, and SPA static serving. |
| `internal/auth/` | HS256 JWT minting/validation and bearer-token middleware. Tokens expire after 24 hours. |
| `internal/perm/` | RBAC permission matrix for system groups (`viewer`, `editor`, `administrator`). |
| `internal/opamp/` | OpAMP server lifecycle, workload identity, available component tracking, config fan-out, and status aggregation. |
| `internal/alerts/` | Alert engine and notifier fan-out. The engine ticks every 30 seconds in `pkg/server`. |
| `internal/workloads/` | Workload janitor that archives expired disconnected workloads and trims old events. |
| `internal/store/` | PostgreSQL persistence plus goose migrations. |
| `pkg/models/` | Shared domain structs serialized by the API and persisted by the store. |
| `pkg/ext/` | Extension interfaces used by community and enterprise binaries: auth methods, audit logger, notifier, store abstractions. |

## Runtime composition

`pkg/bootstrap.Run` is the normal startup path:

1. Load `internal/config.Config` from environment variables.
2. Fail closed when `JWT_SECRET` is unset, the placeholder is used, or the secret is too short.
3. Require `DB_DSN`, open the PostgreSQL store, and run migrations.
4. Optionally create the first administrator, atomically and only on an empty users table, from `SEED_ADMIN_EMAIL` and `SEED_ADMIN_PASSWORD`.
5. Construct `pkg/server.Server` with any extension options supplied by the binary.
6. Start the API listener, OpAMP listener, alert engine, workload janitor, and WebSocket hub.

`pkg/server.Server` exposes extension points through options such as:

- `WithNotifier` for alert delivery integrations.
- `WithAuditLogger` for persistent audit sinks.
- `WithRouterHook` and `WithProtectedRouterHook` for extra routes/middleware.
- `WithAuthMethod` and `WithAuthMethodProvider` for additional login methods.
- `WithCapabilities` for typed capability declarations exposed by `GET /api/v1/capabilities`.
- `WithFeatures` for legacy edition overlays exposed by `GET /api/features`.
- `WithLicenseChecker` for edition/entitlement checks on gated endpoints.

## Capability discovery

`GET /api/v1/capabilities` is the canonical public capability-discovery endpoint. `GET /api/features` remains a legacy boolean compatibility endpoint. `WithCapabilities` is preferred for typed declarations; `WithFeatures` remains supported for legacy edition overlays.

```go
import (
    "github.com/magnify-labs/otel-magnify/pkg/capabilities"
    "github.com/magnify-labs/otel-magnify/pkg/server"
)

func capabilityOption() (server.Option, error) {
	registry, err := capabilities.New([]capabilities.Capability{
		{ID: "config_safety.approvals", State: capabilities.StateEnabled},
	})
	if err != nil {
		return nil, err
	}
	return server.WithCapabilities(registry), nil
}
```

The versioned document has three states: `enabled`, `disabled`, and `read_only`. An `enabled` capability must not include `reason_code`; every `disabled` or `read_only` capability requires one. Valid reason codes are `not_enabled`, `prerequisite_unavailable`, and `read_only_mode`.

Community advertises only `config_safety.approvals` and `config_safety.policy_preview` in this release. The legacy endpoint projects the same registry into booleans for older edition overlays.

Capability discovery is not authorization. Protected APIs still enforce authentication, RBAC, and server-side gates. `WithLicenseChecker` is consulted by server-side gates and does not add capabilities to either public discovery response.

## Database notes

- PostgreSQL is the only supported database. `DB_DSN` is required and must be a PostgreSQL connection string.
- Migrations are managed with `pressly/goose` and run automatically on startup.
- Store tests use isolated PostgreSQL schemas through `TEST_POSTGRES_DSN`.

## Security caveats for contributors

- Never add a default production JWT secret. `JWT_SECRET` is required at startup and should be generated by the operator.
- Avoid logging request bodies that may contain Collector YAML, DSNs, passwords, bearer tokens, webhook URLs, or exporter credentials.
- Browser WebSocket auth uses the `om_session` HttpOnly cookie. Avoid copying legacy `/ws?token=...` URLs with real tokens into logs, docs, or screenshots.
- OpAMP bearer auth is controlled by `OPAMP_SHARED_SECRET`; leave it empty only for local or trusted-network demos.
- Feature flags are discovery metadata, not authorization. Keep RBAC checks on protected handlers even if the UI hides a feature.
- Mutating handlers that emit audit records may return `503` with `side_effect_status`; callers must reconcile according to that field rather than blindly retrying every mutation.
