// Package bootstrap wires together the otel-magnify server subsystems
// (config, store, auth, alerts, server) into a single entry point usable
// by any edition binary. Community and enterprise binaries both call
// Run and customise behaviour through Options.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/crypto/bcrypt"

	"github.com/magnify-labs/otel-magnify/internal/alerts"
	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/config"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/frontend"
	"github.com/magnify-labs/otel-magnify/pkg/models"
	"github.com/magnify-labs/otel-magnify/pkg/server"
)

// Options lets callers extend the default community behaviour.
type Options struct {
	// ExtraServerOptions are appended to the default server options
	// (notifier, static FS) when constructing server.Server. Edition
	// binaries use this to register auth methods, router hooks, or
	// audit loggers without reimplementing the full bootstrap flow.
	ExtraServerOptions []server.Option

	// StaticFS overrides the embedded frontend. Zero value means the
	// default pkg/frontend embed is used. To serve no static assets at
	// all, pass a non-nil empty FS (e.g. fstest.MapFS{}) — leaving this
	// field zero installs the community default.
	StaticFS fs.FS

	// PreRun is called after migrations and seedAdmin, before the
	// server is constructed. Edition binaries use it to run edition-
	// scoped migrations, build dynamic state (e.g. a provider
	// registry), and return additional server options. Returned
	// options are appended to ExtraServerOptions. Returning an error
	// aborts Run and propagates the error to the caller.
	//
	// The callback receives both the opened Store and the constructed
	// AuthProvider so callers can mint tokens or query state without
	// re-initialising those subsystems.
	PreRun func(store ext.Store, auth ext.AuthProvider) ([]server.Option, error)
}

// Run loads configuration from the environment, opens the database,
// applies migrations, seeds the admin user if requested, builds a
// Server with the community defaults (plus any ExtraServerOptions),
// and blocks until ctx is cancelled or a SIGINT/SIGTERM is received.
// It returns an error if any step of the bootstrap fails.
//
// Callers that manage their own signal handling can cancel ctx
// directly; Run installs its own SIGINT/SIGTERM handler on top.
func Run(ctx context.Context, opts Options) error {
	cfg := config.Load()
	if err := validateJWTSecret(cfg.JWTSecret); err != nil {
		return err
	}

	db, err := store.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		return err
	}
	//nolint:errcheck // deferred until process exit; close error not actionable here
	defer db.Close()

	if err := db.Migrate(); err != nil {
		return err
	}
	log.Println("Database migrations applied")

	seedAdmin(db)

	a := auth.New(cfg.JWTSecret)

	var preRunOpts []server.Option
	if opts.PreRun != nil {
		var err error
		preRunOpts, err = opts.PreRun(db, a)
		if err != nil {
			return fmt.Errorf("pre-run: %w", err)
		}
	}

	serverOpts := []server.Option{}

	if wh := alerts.NewWebhookNotifier(cfg.WebhookURL); wh != nil {
		serverOpts = append(serverOpts, server.WithNotifier(wh))
	}

	staticFS := opts.StaticFS
	if staticFS == nil {
		staticFS = frontend.FS()
	}
	serverOpts = append(serverOpts, server.WithStaticFS(staticFS))

	serverOpts = append(serverOpts, opts.ExtraServerOptions...)
	serverOpts = append(serverOpts, preRunOpts...)

	srv := server.New(server.Config{
		ListenAddr:              cfg.ListenAddr,
		OpAMPAddr:               cfg.OpAMPAddr,
		OpAMPSharedSecret:       cfg.OpAMPSharedSecret,
		CORSOrigins:             cfg.CORSOrigins,
		MinAgentVersion:         cfg.MinAgentVersion,
		WorkloadRetention:       cfg.WorkloadRetention,
		WorkloadDisconnectGrace: cfg.WorkloadDisconnectGrace,
		WorkloadJanitorInterval: cfg.WorkloadJanitorInterval,
		WorkloadEventRetention:  cfg.WorkloadEventRetention,
	}, db, a, serverOpts...)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		select {
		case <-sig:
			cancel()
		case <-runCtx.Done():
		}
	}()

	return srv.Run(runCtx)
}

func validateJWTSecret(secret string) error {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return errors.New("JWT_SECRET environment variable is required")
	}
	if trimmed == "change-me-in-production" {
		return errors.New("JWT_SECRET must not use the placeholder value")
	}
	if len(trimmed) < 32 {
		return errors.New("JWT_SECRET must be at least 32 characters")
	}
	return nil
}

// seedAdmin creates an administrator user on startup when SEED_ADMIN_EMAIL
// and SEED_ADMIN_PASSWORD are set. The user is attached to the system
// `administrator` group — membership is what grants privileges now.
func seedAdmin(db ext.Store) {
	email := os.Getenv("SEED_ADMIN_EMAIL")
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return
	}

	if _, err := db.GetUserByEmail(email); err == nil {
		//nolint:gosec // SEED_ADMIN_EMAIL is operator-supplied at deploy time, not user input
		log.Printf("Seed admin: user %s already exists, skipping", email)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Printf("Seed admin: failed to hash password: %v", err)
		return
	}

	user := models.User{
		ID:           "admin-seed-001",
		Email:        email,
		PasswordHash: string(hash),
	}
	if err := db.CreateUser(user); err != nil {
		log.Printf("Seed admin: failed to create user: %v", err)
		return
	}
	if err := db.AttachUserToGroupByName(user.ID, "administrator"); err != nil {
		log.Printf("Seed admin: failed to attach admin group: %v", err)
		return
	}
	//nolint:gosec // SEED_ADMIN_EMAIL is operator-supplied at deploy time, not user input
	log.Printf("Seed admin: created user %s in group administrator", email)
}
