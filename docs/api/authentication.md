# Authentication

> **Status:** Stub — to be expanded.

## Planned content

- Login flow against `POST /api/auth/login`.
- JWT signing with HS256 and the `JWT_SECRET` env var.
- Token lifetime and refresh strategy (currently: re-login on expiry).
- Middleware in `internal/auth/` — how to use it in new handlers.
- RBAC: `admin` and `viewer` roles, where enforcement happens.
- WebSocket auth — browser clients use the `om_session` HttpOnly cookie on `/ws`; `?token=` remains a legacy fallback because browsers cannot set custom headers on the WS handshake.
