# Backend

> **Status:** Stub — to be expanded.

## Planned content

- Walkthrough of each `internal/` package: `api`, `alerts`, `auth`, `config`, `opamp`, `store`.
- How `cmd/server/main.go` wires everything together, including `embed.FS` for the frontend.
- Migration strategy with `pressly/goose` — why it was chosen over `golang-migrate`.
- The OpAMP server's use of `Attach()` on the chi mux.
- `pkg/models` as the shared type boundary between packages.
- Local dev loop: `JWT_SECRET=local-dev-secret-at-least-32-chars!! go run ./cmd/server/`.
