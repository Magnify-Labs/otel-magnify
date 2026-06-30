package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/perm"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// OpAMPPusher is the subset of opamp.Server the HTTP layer uses.
// Declared here so handlers can be tested with a fake.
type OpAMPPusher interface {
	PushConfig(ctx context.Context, workloadID string, yamlContent []byte, targetInstanceUID string) error
	Instances(workloadID string) []opamp.Instance
}

// API holds the HTTP handler dependencies (store, auth, WS hub, OpAMP pusher, feature flags) shared across all routes.
type API struct {
	db                ext.Store
	auth              ext.AuthProvider
	hub               *Hub
	opamp             OpAMPPusher
	audit             ext.AuditLogger
	authMethods       func() []ext.AuthMethod
	workloadRetention time.Duration
	features          map[string]bool
}

// NewRouter constructs the chi-based HTTP handler that wires together public
// routes (health, auth, features), protected REST endpoints, the WebSocket hub,
// and the embedded SPA catch-all. It is the single composition root shared by
// the production listener and httptest-based assertions.
//
// protectedHooks are functions invoked inside the Bearer-token auth group;
// each hook receives a chi.Router whose requests have already been
// authenticated and decorated with ext.UserInfoFromContext. Pass nil
// when no extra protected routes are needed (the standard community case).
//
// auditLogger may be nil — handlers fall back to ext.NopAuditLogger so the
// community binary works without an audit sink configured. EE wires its
// sink via pkg/server.WithAuditLogger.
func NewRouter(db ext.Store, a ext.AuthProvider, hub *Hub, opampSrv OpAMPPusher, auditLogger ext.AuditLogger, corsOrigins string, staticFS fs.FS, authMethods func() []ext.AuthMethod, workloadRetention time.Duration, features map[string]bool, protectedHooks []func(chi.Router)) http.Handler {
	if auditLogger == nil {
		auditLogger = ext.NopAuditLogger{}
	}
	api := &API{db: db, auth: a, hub: hub, opamp: opampSrv, audit: auditLogger, authMethods: authMethods, workloadRetention: workloadRetention, features: features}
	if api.features == nil {
		api.features = map[string]bool{}
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// CORS middleware
	allowedOrigins := []string{"http://localhost:5173"}
	if corsOrigins != "" {
		allowedOrigins = strings.Split(corsOrigins, ",")
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check (public, no auth)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		//nolint:errcheck,gosec // status already committed; a Write failure here only signals a closed client connection
		w.Write([]byte("ok"))
	})

	// Public routes
	r.Post("/api/auth/login", api.handleLogin)
	r.Get("/api/auth/methods", api.handleListAuthMethods)
	r.Get("/api/features", api.handleListFeatures)

	// WebSocket validates its own token via ?token= query param
	// (browsers cannot set Authorization headers on WS handshakes, so it
	// cannot live behind the Bearer-token middleware).
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if _, err := a.ValidateToken(token); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		hub.HandleWS(w, r)
	})

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(a.Middleware)

		r.Get("/api/workloads", api.handleListWorkloads)
		r.Get("/api/workloads/{id}", api.handleGetWorkload)
		r.Get("/api/workloads/{id}/instances", api.handleListWorkloadInstances)
		r.Get("/api/workloads/{id}/events", api.handleListWorkloadEvents)
		r.Get("/api/workloads/{id}/events/stats", api.handleWorkloadEventsStats)
		r.With(api.RequirePerm(perm.PushConfig)).Post("/api/workloads/{id}/config", api.handlePushWorkloadConfig)
		r.With(api.RequirePerm(perm.ValidateConfig)).Post("/api/workloads/{id}/config/validate", api.handleValidateWorkloadConfig)
		r.Get("/api/workloads/{id}/configs", api.handleGetWorkloadConfigHistory)
		r.Get("/api/workloads/{id}/configs/{hash}", api.handleGetWorkloadConfigByHash)
		r.Get("/api/workloads/{id}/known-good", api.handleGetWorkloadKnownGood)
		r.With(api.RequirePerm(perm.PushConfig)).Post("/api/workloads/{id}/configs/{hash}/label", api.handleSetWorkloadConfigLabel)
		r.With(api.RequirePerm(perm.PushConfig)).Post("/api/workloads/{id}/configs/{hash}/known-good", api.handleMarkWorkloadConfigKnownGood)
		r.With(api.RequirePerm(perm.PushConfig)).Delete("/api/workloads/{id}/known-good", api.handleClearWorkloadKnownGood)
		r.With(api.RequirePerm(perm.PushConfig)).Post("/api/workloads/{id}/rollback", api.handleRollbackWorkloadDefault)
		r.With(api.RequirePerm(perm.PushConfig)).Post("/api/workloads/{id}/configs/{hash}/rollback", api.handleRollbackWorkloadConfig)
		r.With(api.RequirePerm(perm.ArchiveWorkload)).Post("/api/workloads/{id}/archive", api.handleArchiveWorkload)
		r.With(api.RequirePerm(perm.DeleteWorkload)).Delete("/api/workloads/{id}", api.handleDeleteWorkload)

		// Legacy /api/agents/... redirects (remove at next minor release).
		r.Get("/api/agents", redirectAgentsToWorkloads)
		r.Get("/api/agents/{id}", redirectAgentsToWorkloads)
		r.Get("/api/agents/{id}/configs", redirectAgentsToWorkloads)
		r.Post("/api/agents/{id}/config", redirectAgentsToWorkloads)
		r.Post("/api/agents/{id}/config/validate", redirectAgentsToWorkloads)

		r.Get("/api/configs", api.handleListConfigs)
		r.With(api.RequirePerm(perm.CreateConfigTpl)).Post("/api/configs", api.handleCreateConfig)
		r.Get("/api/configs/{id}", api.handleGetConfig)

		r.Get("/api/alerts", api.handleListAlerts)
		r.With(api.RequirePerm(perm.ResolveAlert)).Post("/api/alerts/{id}/resolve", api.handleResolveAlert)

		r.Get("/api/pushes/activity", api.handleListPushActivity)

		r.Get("/api/me", api.handleGetMe)
		r.Put("/api/me/password", api.handlePutPassword)
		r.Put("/api/me/preferences", api.handlePutPreferences)

		// Protected hooks run inside this group, so they inherit the
		// auth middleware. Enterprise binaries register admin endpoints
		// here (e.g. /api/admin/sso/*) so RBAC middleware can rely on a
		// populated UserInfo context.
		for _, hook := range protectedHooks {
			hook(r)
		}
	})

	// Serve embedded frontend assets as catch-all (SPA fallback)
	if staticFS != nil {
		r.NotFound(ServeStatic(staticFS))
	}

	return r
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("respondJSON: encode error: %v", err)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}
