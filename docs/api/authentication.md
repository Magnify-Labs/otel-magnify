# Authentication

otel-magnify's community server uses email/password login backed by HS256 JWTs. Enterprise or edition binaries can advertise additional login methods through server options, but the bearer-token contract stays the same for protected REST APIs.

## Login flow

1. Discover available login methods:

   ```http
   GET /api/auth/methods
   ```

   Community response:

   ```json
   {
     "methods": [
       {
         "id": "password",
         "type": "password",
         "display_name": "Email + password",
         "login_url": "/api/auth/login"
       }
     ]
   }
   ```

2. Log in with email and password:

   ```http
   POST /api/auth/login
   Content-Type: application/json
   ```

   ```json
   { "email": "<bootstrap-email>", "password": "<bootstrap-password>" }
   ```

3. Store the returned token client-side for API calls:

   ```json
   { "token": "eyJhbGciOi..." }
   ```

`POST /api/auth/login` returns `400` for malformed or incomplete JSON and `401` for invalid credentials. If the audit sink is unavailable, it can return `503` with `side_effect_status: "none"`; retrying login is safe because no business mutation was persisted.

## Bearer tokens

Protected REST endpoints require:

```http
Authorization: Bearer <jwt>
```

Tokens are signed with `JWT_SECRET` using HS256 and expire after 24 hours. There is no refresh-token endpoint in the community server today; clients should redirect to login when a protected request returns `401`.

Token claims include:

- `user_id`
- `email`
- `groups`
- standard `iat` and `exp` registered claims

Legacy tokens containing a single `role` claim are still accepted for the token lifetime and translated to the matching group name.

## RBAC model

Authorization is group/permission based. The seeded system groups are:

| Group | Intended access |
|-------|-----------------|
| `viewer` | Read-only inventory, configs, alerts, and user profile access. |
| `editor` | Viewer access plus config/template operations where permitted. |
| `administrator` | Full operational access, including archive/delete style operations and settings-level permissions. |

Handlers enforce concrete permissions through `internal/perm`, not raw group-name string checks. When adding a handler, protect it with the narrowest permission that matches the side effect.

## WebSocket authentication

The browser WebSocket hub is authenticated with the `om_session` HttpOnly session cookie set by `/api/auth/login`, so the SPA does not need to persist or place the JWT in the WebSocket URL.

The legacy query-token form remains available for non-browser or older clients:

```text
/ws?token=<jwt>
```

Browsers cannot set custom `Authorization` headers during WebSocket handshakes, so cookie auth plus the legacy query fallback is intentional. Security caveats:

- Do not log full WebSocket URLs that contain a real token.
- Prefer TLS in production so cookies and query strings are encrypted in transit.
- Treat `401` from the WebSocket handshake as a signal to re-login.

## Feature discovery is not auth

`GET /api/v1/capabilities` is the canonical public capability-discovery endpoint. `GET /api/features` remains a legacy boolean compatibility endpoint. Capability discovery is not authorization: protected APIs still enforce authentication, RBAC, and server-side gates.

Community advertises only `config_safety.approvals` and `config_safety.policy_preview` in this release. For binaries that declare capabilities, `WithCapabilities` is preferred for typed declarations; `WithFeatures` remains supported for legacy edition overlays.

## Seeded admin bootstrap

On first start, operators can generate or enter a bootstrap credential without
putting a reusable example password in configuration:

```bash
export SEED_ADMIN_EMAIL="admin@example.invalid"
read -r -s -p "Initial admin password (minimum 12 characters): " SEED_ADMIN_PASSWORD
echo
export SEED_ADMIN_PASSWORD
```

The bootstrapper requires both variables and creates the administrator only
when the users table is empty. Reusing the same existing administrator email
is idempotent and never resets its password; other conflicts fail startup.
Remove both variables after the first successful login.
