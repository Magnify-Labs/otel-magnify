# Frontend

The frontend is a React 19 + TypeScript + Vite app under `frontend/`. It is built into `frontend/dist` and embedded by the Go server for the single-binary deployment path.

## Local development

```bash
cd frontend
npm ci
npm run dev
```

Vite listens on `http://localhost:5173` by default. Run the Go backend separately on `http://localhost:8080`; the default `CORS_ORIGINS` value allows the Vite origin for local development.

## Structure and conventions

| Area | Notes |
|------|-------|
| Routing | `frontend/src/main.tsx` uses `createBrowserRouter`; `/inventory` is the workload list route. |
| Server state | TanStack Query owns API cache and invalidation. Keep server data out of long-lived Zustand slices. |
| Client/session state | Zustand holds UI/session state. `startClientSession()` and `clearClientSessionState()` clear sensitive caches on identity changes. |
| API client | `frontend/src/api/client.ts` wraps REST calls. Keep endpoint shapes aligned with `docs/api/rest.md` and Go models. |
| WebSocket | `frontend/src/api/websocket.ts` connects to `/ws`; browser auth uses the `om_session` HttpOnly cookie. Do not add JWTs to WebSocket URLs except for legacy compatibility tests. |
| YAML editing | CodeMirror 6 powers config editing and diff views. |
| Styling | Use CSS classes and variables in `frontend/src/styles/global.css`; avoid inline styles. |
| i18n | `react-i18next` is initialized from `frontend/src/i18n/`; keep user-facing copy translatable. |

## Workload UI

The workload detail page is centered on a workload, not an individual pod. Current tabs and panels include config/history, live instances, activity, safety approvals, guided rollback, canary rollout, policy preview, migration assistance, drift/version intelligence, report evidence, and archive/delete actions where the server feature flags and RBAC permissions allow them.

Feature flags are discovery metadata from `GET /api/features`; they hide or show UI affordances only. Protected endpoints must still enforce RBAC and feature gates on the server.

## Verification

Use focused checks while developing and the full build before opening a PR:

```bash
cd frontend
npm ci
npm run lint
npm run build
npm run test:unit
npm run test:e2e
```

`npm run build` runs `tsc -b` before `vite build`, matching the CI frontend build gate. Use `npm run test:e2e:real` only when you intentionally want Playwright to exercise a real backend environment.
