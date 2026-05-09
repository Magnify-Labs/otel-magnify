# Changelog

All notable changes to this project are documented here.
## Unreleased

### Documentation
- Document audit log emission surface


### Features
- Add Emit helper for community handlers
- Wire emission for workload lifecycle
- Wire emission for auth (login + password change)
- Wire emission for config templates


## v0.5.0 — 2026-05-08

### Bug Fixes
- Silence false-positive security warnings in SSO Playwright tests (#51)
- Make Renovate config valid (#62)
- Gate SSO admin redirect behind features.isLoading (#72)
- Move YamlEditor onChange ref assignment into useEffect (#73)
- Reject backslash bypass in legacy agents redirect (#85)


### CI/CD
- Bump actions-security to Phase 2 (enforce) (#68)
- Ignore Go minor bumps for sdkagent image (#43)


### Documentation
- Update alert engine description in README


### Features
- Drop root + add HEALTHCHECK (#65)
- Migrate to react-router-dom v7 (Data API) (#66)
- Harden chart with pod security context and resource limits (#70)
- Add explicit config revision labels, comparison, and rollback (#86)


### Refactoring
- Remove setState-in-effect, re-enable rule as error (#50)


## v0.4.0 — 2026-04-30

### Documentation
- Update changelog for v0.4.0


### Features
- Expose WithProtectedRouterHook for auth-protected extensions (#49)


### Testing
- Real-backend Playwright for SSO admin lifecycle (#48)


## v0.3.0 — 2026-04-30

### CI/CD
- Foundations — lefthook, golangci, gitleaks, dependabot, security-pr, codeql (#19)
- Public posture — Scorecard, CODEOWNERS, scheduled scan, badges, markdownlint+lychee (#40)
- Supply chain — multi-arch ghcr.io, cosign keyless, SBOM, license check (#41)
- Add actions-security workflow (zizmor + actionlint + pinact) (#44)


### Features
- Apply a saved config from dropdown in collector detail (#45)
- SSO v2 admin UI + capabilities endpoint (#47)


### Refactoring
- Classify agents by attributes instead of service.name alone (#46)


## v0.2.1 — 2026-04-24

### Bug Fixes
- Request full state on unknown-instance heartbeat (#17)


### Documentation
- Update changelog for v0.2.1


### Features
- Extension hooks for SSO (Detach/Replace groups, WithAuthMethodProvider, bootstrap.PreRun) (#18)


## v0.2.0 — 2026-04-23

### Documentation
- Dedupe v0.2.0 changelog and note post-merge fixes


### Features
- RBAC groups + profile page (v0.2.0) (#16)


## v0.1.1 — 2026-04-23

### Documentation
- Update changelog for v0.1.1


### Refactoring
- Move Go module from backend/ to repo root (#15)


## v0.1.0 — 2026-04-22

### Bug Fixes
- Address critical review findings (CORS, embed, auth, DoS, healthz, seed)
- Pass seed admin env vars through docker-compose
- Use spec.ingressClassName instead of deprecated annotation
- Remove unused total variable
- Also invalidate agent query after config push
- Improve cache invalidation and axios error handling in AgentConfigSection
- YamlEditor now reacts to readOnly and value changes
- Preserve agent state across heartbeats
- Drop obsolete @codemirror/basic-setup
- Accept 'applying' remote config status and open /ws to query-token auth
- Invalidate TanStack caches on WS events for live updates
- Set document title to otel-magnify
- Exclude SDK agents from Inventory control filter
- Restore all compile-time interface satisfaction checks
- Remove superpowers plans/specs leaked into repo (belong in Obsidian)
- Hydrate agent state in disconnect broadcast
- Gate push-config on OpAMP capability, not agent type
- Trust empty auth methods response and pin fetch in E2E
- Toggle SQLite foreign_keys around migrations and add FK check to 00011


### CI/CD
- Add git-cliff config and initial changelog
- Build and deploy MkDocs site to GitHub Pages
- Add release script with rolling BSL Change Date injection


### Documentation
- Add OpAMP management platform design spec
- Translate design spec to English
- Add implementation plan for otel-magnify
- Write comprehensive README
- Add CLAUDE.md project instructions
- Replace Apache 2.0 with BSL 1.1
- Add pre-1.0 status banner and BSL 1.1 badge
- Add security policy and vulnerability reporting
- Add contributing guide with DCO
- Add public roadmap (Now/Next/Later)
- Add spec for agent configuration display feature
- Add implementation plan for agent configuration display
- Document release workflow in CLAUDE.md
- Translate all repo content to English
- Add users section (install, config, agents, push, alerts, troubleshooting)
- Add developers section with architecture and OpAMP flow diagrams
- Add API section (REST, WebSocket, auth, OpAMP)
- Add reference section (env vars, glossary, changelog)
- Add landing page with audience cards and asset placeholders
- Align with post-merge reality of config-push-validation
- Add badge and link to published documentation site
- Add agent connection guide and sample configs
- Amend BSL Additional Use Grant (no agent cap, prepare rolling Change Date)
- Update README license section (no agent cap, rolling Change Date)
- Document OpAMP Supervisor setup and ship sdkagent Dockerfile
- Bump collector-contrib reference to 0.150.1
- Note accepts_remote_config is persisted and gated
- Document agentCapabilities predicate invariants
- Add pricing and licensing design spec
- Add extensibility and module overlay implementation plan
- Clarify Handler() test-only contract and Run() exclusivity
- Document workload identity model and K8s resource attributes
- Add design handoff with React prototype for UI rebuild
- Update changelog for v0.1.0


### Features
- Initialize Go backend module with config
- Add shared data models
- Add database layer with goose migrations
- Add agent store CRUD operations
- Add config and agent_config store operations
- Add alert and user store operations
- Add OpAMP server with agent registration and config push
- Add JWT auth with middleware
- Add WebSocket hub for real-time frontend updates
- Add REST API router with agent handlers
- Implement config, alert, and auth REST handlers
- Add alert engine with agent_down rule evaluation
- Wire server entrypoint with all backend components
- Scaffold React frontend with Vite
- Add API client, WebSocket client, and Zustand store
- Add layout, dashboard page, and routing
- Redesign frontend with industrial terminal aesthetic
- Add Dockerfile and docker-compose for deployment
- Add Helm chart for Kubernetes deployment
- Redesign frontend with Signal Deck aesthetic
- Add config_drift and version_outdated alert rules
- Add webhook notifications for alerts
- Add route guards for authenticated pages
- Add agent status chart to Dashboard
- Add Postgres service profile to docker-compose
- Improve agent type detection (collector vs SDK)
- Add Collectors/SDK stat cards, remove pie chart
- Remove total agents stat card from dashboard
- Rename Agents to Inventory, clickable stat cards with type filter
- Add sample OTel Collector configs for OpAMP demo
- Add AgentConfigSection component for config display and push
- Display agent configuration in detail page
- Add sdkagent tool for simulating OpAMP SDK agents
- Capture effective config reported by connected agents
- Add push status columns on agent_configs and agents
- Add RemoteConfigStatus and push-history fields
- Extend agent_configs with error_message, pushed_by, JOIN on content
- Capture RemoteConfigStatus and auto-rollback on failure
- Persist push in agent_configs, return config_hash, enrich history
- Broadcast agent_config_status and auto_rollback_applied events
- Wire RemoteConfigStatus and auto_rollback events into store
- Capture AvailableComponents reported by agents
- Light config validator + POST /agents/{id}/config/validate
- Validate button with inline error list, gates Push
- Signal Deck YAML theme + PushStatusBanner component
- ConfigDiffView via @codemirror/merge
- AgentConfigSection state machine + history table
- Persist accepts_remote_config on agents
- Capture AcceptsRemoteConfig capability from OpAMP
- Reject config push when agent does not accept remote config
- Add accepts_remote_config type, helpers, and supervised styles
- Show supervised pill on Inventory agent cards
- Show Control cell (Supervised/Read-only) on agent detail
- Hide edit affordances on read-only collectors and add note
- Add control filter (supervised/read-only) on Inventory
- Add Supervised stat card on Dashboard
- Add extension interfaces for module overlay
- Add composable server builder with functional options
- Add UpdateUser for role refresh via SSO callbacks
- Add GET /api/auth/methods with default password entry
- Add WithAuthMethod option with ID deduplication
- Fetch auth methods and render SSO login buttons
- Add Workload and WorkloadEvent types
- Add 00011 rename agents to workloads migration
- Add 00012 workload_events migration
- Add workload events CRUD
- Add workload fingerprint with K8s/host/uid strategies
- Add in-memory instance registry
- Add grace-period controller for workload status
- Add retention janitor goroutine
- Wire workload janitor and expose retention env vars
- Broadcast workload update and event over WS
- Expose workloads endpoints with legacy 307 redirects
- Add Instances and Activity sub-tabs on workload detail
- Migrate tokens and add i18n foundation
- Rebuild sidebar chrome from design handoff
- Redesign with push activity and fleet health panels
- Rebuild workload cards and filter bar
- Expose embed as pkg/frontend
- Expose server lifecycle as pkg/bootstrap


### Refactoring
- Use reverse map for OpAMP connection tracking
- Implement ext.AuthProvider, return UserInfo instead of Claims
- Accept ext.Store and ext.AuthProvider interfaces
- Accept OpAMPStore interface instead of concrete *store.DB
- Accept AlertStore interface and ext.AlertNotifier slice
- Use pkg/server builder for composable startup
- Route onConnectionClose through broadcastDisconnect
- Extract docs base URL into VITE_DOCS_BASE_URL
- Shrink onConnectionClose critical section to map ops
- Rename Go module to github.com/magnify-labs/otel-magnify
- Return sql.ErrNoRows from UpdateUser when no match
- Replace agents with workloads CRUD layer
- Propagate workload rename through store + ext interface
- Route messages through workload registry with events and grace
- Evaluate rules against workloads (rule id workload_down supersedes agent_down)
- Swap Agent types for Workload/Instance/WorkloadEvent
- Rename Agent pages/components/store to Workload and update WS dispatcher
- Wire cmd/server to pkg/frontend
- Reduce cmd/server to a bootstrap.Run wrapper


### Testing
- Playwright coverage for edit, validate, push, diff, history, theme
- E2e coverage for supervised/read-only collector UX
- Cover DB-error fallback in disconnect broadcast
- Integration coverage for onConnectionClose broadcast
- Add Playwright coverage for workload inventory, instances, activity
- Migrate legacy agent specs to workload endpoints



