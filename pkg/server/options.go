// Package server composes the API router, OpAMP server, alert engine, and janitor into a single runnable Server, with functional options for EE overlays.
package server

import (
	"io/fs"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// Config holds the server's listen addresses and feature flags.
type Config struct {
	ListenAddr      string // default ":8080"
	OpAMPAddr       string // default ":4320"
	CORSOrigins     string
	MinAgentVersion string

	// Workload lifecycle tuning. Zero values let the downstream subsystems
	// apply their own defaults (2-minute grace, 30-day retention, 5-minute
	// janitor tick).
	WorkloadRetention       time.Duration
	WorkloadDisconnectGrace time.Duration
	WorkloadJanitorInterval time.Duration
	WorkloadEventRetention  time.Duration
}

// Option configures optional features on the Server.
type Option func(*Server)

// WithNotifier adds an AlertNotifier (webhook, email, Slack, etc.).
func WithNotifier(n ext.AlertNotifier) Option {
	return func(s *Server) {
		s.notifiers = append(s.notifiers, n)
	}
}

// WithAuditLogger sets the audit logger. Default is ext.NopAuditLogger.
func WithAuditLogger(l ext.AuditLogger) Option {
	return func(s *Server) {
		s.auditLogger = l
	}
}

// WithReportSigner wires an enterprise report signer/verifier into evidence-pack exports.
func WithReportSigner(signer ext.ReportSigner) Option {
	return func(s *Server) {
		if signer != nil {
			s.reportSigner = signer
		}
	}
}

// WithStaticFS sets the embedded frontend assets for SPA serving.
func WithStaticFS(fsys fs.FS) Option {
	return func(s *Server) {
		s.staticFS = fsys
	}
}

// WithRouterHook adds a function that can modify the chi router before
// the server starts. Use this to add middleware (RBAC, audit) or extra routes.
//
// Routes added by this hook are attached to the outer router and DO NOT
// pass through the Bearer-token auth middleware — use it only for
// genuinely public endpoints (SSO ACS callbacks, login, public metadata).
// For routes that must run with an authenticated UserInfo in context,
// use WithProtectedRouterHook instead.
func WithRouterHook(fn func(chi.Router)) Option {
	return func(s *Server) {
		s.routerHooks = append(s.routerHooks, fn)
	}
}

// WithProtectedRouterHook adds a function that registers routes inside
// the community auth-middleware-protected group. Routes mounted this
// way require a valid Bearer token: the request is rejected with 401
// before the hook's handlers run, and downstream RBAC middleware
// (perm.RequireGroup, custom checks) can rely on ext.UserInfoFromContext
// returning a non-nil value.
//
// This is the correct option for enterprise admin endpoints
// (e.g. /api/admin/sso/*), which need the community auth boundary applied
// before any feature-level permission check.
func WithProtectedRouterHook(fn func(chi.Router)) Option {
	return func(s *Server) {
		s.protectedRouterHooks = append(s.protectedRouterHooks, fn)
	}
}

// WithAuthMethod registers an additional login method exposed on
// GET /api/auth/methods. The "password" method is always registered by
// default. Duplicate IDs are ignored with a warning log; the first
// registration wins (so the built-in default cannot be accidentally
// overridden by an enterprise option).
func WithAuthMethod(m ext.AuthMethod) Option {
	return func(s *Server) {
		s.authMethods = append(s.authMethods, m)
	}
}

// WithAuthMethodProvider registers a callback consulted on every
// GET /api/auth/methods request. When set, the provider's return value
// fully replaces the static list built from the default "password"
// method plus any WithAuthMethod(...) entries. The fallback to the
// static list applies when no provider is registered or the provider
// itself returns nil.
//
// Enterprise binaries use this to serve a DB-backed, runtime-mutable
// list of SSO providers without restarting.
//
// The callback pointer is registered once at server construction and
// never swapped; only its return value is consulted on each request.
// Thread-safety of the callback body is the caller's responsibility:
// the returned slice is JSON-encoded synchronously on the request
// goroutine, so the callback must return a slice that will not be
// mutated after return (typically, return a fresh copy or an immutable
// snapshot).
func WithAuthMethodProvider(fn func() []ext.AuthMethod) Option {
	return func(s *Server) {
		s.authMethodProvider = fn
	}
}

// WithFeatures registers a static map of feature flags exposed on
// GET /api/features. Edition binaries use it to advertise capabilities
// (e.g. "sso.admin") that the frontend uses to conditionally render
// pages and menu items.
//
// The map is fixed at construction; there is no dynamic provider.
// Features are build-time decisions, not runtime mutable state — to
// toggle a feature, restart the binary with a different option.
//
// Default (no option set): an empty map. The endpoint always returns
// 200 with {"features": {}} rather than 404, so the frontend can
// distinguish "feature off" from "endpoint missing".
func WithFeatures(features map[string]bool) Option {
	return func(s *Server) {
		s.features = features
	}
}
