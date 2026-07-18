package server

import (
	"context"
	"io/fs"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/alerts"
	"github.com/magnify-labs/otel-magnify/internal/api"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/workloads"
	"github.com/magnify-labs/otel-magnify/pkg/capabilities"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// Server composes the otel-magnify subsystems.
type Server struct {
	cfg                  Config
	store                ext.Store
	auth                 ext.AuthProvider
	notifiers            []ext.AlertNotifier
	auditLogger          ext.AuditLogger
	reportSigner         ext.ReportSigner
	staticFS             fs.FS
	routerHooks          []func(chi.Router)
	protectedRouterHooks []func(chi.Router)
	authMethods          []ext.AuthMethod
	authMethodProvider   func() []ext.AuthMethod
	capabilities         capabilities.Registry
	licenseChecker       ext.LicenseChecker
}

// New creates a Server with the given store, auth provider, and options.
func New(cfg Config, store ext.Store, auth ext.AuthProvider, opts ...Option) *Server {
	s := &Server{
		cfg:          cfg,
		store:        store,
		auth:         auth,
		auditLogger:  ext.NopAuditLogger{},
		reportSigner: ext.NopReportSigner{},
		authMethods: []ext.AuthMethod{
			{
				ID:          "password",
				Type:        "password",
				DisplayName: "Email + password",
				LoginURL:    "/api/auth/login",
			},
		},
	}
	for _, opt := range opts {
		opt(s)
	}

	// Deduplicate authMethods by ID; keep the first occurrence so the
	// built-in "password" default cannot be accidentally overridden.
	seen := make(map[string]struct{}, len(s.authMethods))
	dedup := s.authMethods[:0]
	for _, m := range s.authMethods {
		if _, exists := seen[m.ID]; exists {
			log.Printf("WithAuthMethod: duplicate id %q dropped (first registration wins)", m.ID)
			continue
		}
		seen[m.ID] = struct{}{}
		dedup = append(dedup, m)
	}
	s.authMethods = dedup

	if s.cfg.ListenAddr == "" {
		s.cfg.ListenAddr = ":8080"
	}
	if s.cfg.OpAMPAddr == "" {
		s.cfg.OpAMPAddr = ":4320"
	}
	return s
}

// Run starts all subsystems and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// WebSocket hub
	hub := api.NewHub()
	go hub.Run()

	// OpAMP server. Grace and retention come from env-driven server Config;
	// zero values fall back to opamp's internal defaults (2-min grace,
	// 30-day retention).
	opampSrv := opamp.New(s.store, hub, opamp.Options{
		DisconnectGrace:   s.cfg.WorkloadDisconnectGrace,
		RetentionDuration: s.cfg.WorkloadRetention,
		SharedSecret:      s.cfg.OpAMPSharedSecret,
	})
	opampHandler, connCtx, err := opampSrv.Attach()
	if err != nil {
		return err
	}

	opampMux := http.NewServeMux()
	opampMux.HandleFunc("/v1/opamp", opampHandler)

	opampListener, err := net.Listen("tcp", s.cfg.OpAMPAddr)
	if err != nil {
		return err
	}
	opampHTTP := &http.Server{
		Handler:           opampMux,
		ConnContext:       connCtx,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("OpAMP server listening on %s", opampListener.Addr())
		if err := opampHTTP.Serve(opampListener); err != nil && err != http.ErrServerClosed {
			log.Printf("OpAMP server: %v", err)
		}
	}()

	// Alert engine
	alertEngine := alerts.New(s.store, hub, 5*time.Minute, s.cfg.MinAgentVersion, s.notifiers...)
	go alertEngine.Start(ctx, 30*time.Second)
	log.Println("Alert engine started (30s interval)")

	// Workload janitor: archives expired workloads and purges old events.
	j := workloads.New(s.store, workloads.Options{
		Interval:       s.cfg.WorkloadJanitorInterval,
		EventRetention: s.cfg.WorkloadEventRetention,
	})
	go j.Start(ctx)
	log.Printf("Workload janitor started (interval=%s, event retention=%s)",
		s.cfg.WorkloadJanitorInterval, s.cfg.WorkloadEventRetention)

	// REST API router
	router := api.NewRouter(s.store, s.auth, hub, opampSrv, s.auditLogger, s.cfg.CORSOrigins, s.staticFS, s.currentAuthMethods, s.cfg.WorkloadRetention, s.capabilities, s.licenseChecker, s.protectedRouterHooks, s.reportSigner)

	// Apply router hooks (enterprise can add RBAC middleware, extra routes, etc.)
	if len(s.routerHooks) > 0 {
		if chiRouter, ok := router.(chi.Router); ok {
			for _, hook := range s.routerHooks {
				hook(chiRouter)
			}
		}
	}

	apiListener, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	apiHTTP := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("API server listening on %s", apiListener.Addr())
		if err := apiHTTP.Serve(apiListener); err != nil && err != http.ErrServerClosed {
			log.Printf("API server: %v", err)
		}
	}()

	// Block until context cancellation
	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := apiHTTP.Shutdown(shutdownCtx); err != nil {
		log.Printf("API server shutdown: %v", err)
	}
	if err := opampHTTP.Shutdown(shutdownCtx); err != nil {
		log.Printf("OpAMP HTTP shutdown: %v", err)
	}
	if err := opampSrv.Stop(shutdownCtx); err != nil {
		log.Printf("OpAMP server stop: %v", err)
	}
	hub.Stop()
	log.Println("Shutdown complete")

	return nil
}

// currentAuthMethods returns the effective auth method list. If a provider
// was registered via WithAuthMethodProvider and it returns a non-nil
// slice, that slice wins. Otherwise the static list (password + any
// WithAuthMethod entries) is returned.
func (s *Server) currentAuthMethods() []ext.AuthMethod {
	if s.authMethodProvider != nil {
		if m := s.authMethodProvider(); m != nil {
			return m
		}
	}
	return s.authMethods
}

// Handler builds the HTTP handler served by the public API listener,
// without starting the WebSocket hub or the OpAMP server. Routes that
// depend on those (e.g. /ws, /api/agents/{id}/config) cannot be
// exercised through this handler — it is intended only for httptest
// assertions on stateless endpoints such as /api/auth/methods. Do not
// call Handler() and Run() on the same Server instance.
func (s *Server) Handler() http.Handler {
	hub := api.NewHub()
	opampSrv := opamp.New(s.store, hub, opamp.Options{
		DisconnectGrace:   s.cfg.WorkloadDisconnectGrace,
		RetentionDuration: s.cfg.WorkloadRetention,
		SharedSecret:      s.cfg.OpAMPSharedSecret,
	})
	return api.NewRouter(s.store, s.auth, hub, opampSrv, s.auditLogger, s.cfg.CORSOrigins, s.staticFS, s.currentAuthMethods, s.cfg.WorkloadRetention, s.capabilities, s.licenseChecker, s.protectedRouterHooks, s.reportSigner)
}
