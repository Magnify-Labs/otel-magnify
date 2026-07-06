// Package alerts evaluates rules against workload state and dispatches alert notifications.
package alerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/version"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// Broadcaster pushes real-time updates to connected frontends.
type Broadcaster interface {
	BroadcastAlertUpdate(alert models.Alert)
}

// AlertStore is the subset of ext.Store used by the alert engine.
type AlertStore interface {
	ListWorkloads(includeArchived bool) ([]models.Workload, error)
	GetUnresolvedAlertByWorkloadAndRule(workloadID, rule string) (*models.Alert, error)
	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	GetLatestPendingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
}

type pagedWorkloadStore interface {
	ListWorkloadsPage(includeArchived bool, afterID string, limit int) ([]models.Workload, error)
}

const defaultWorkloadPageSize = 500

// Engine periodically evaluates alert rules (workload_down, config_drift, version_outdated) and emits notifications via the configured AlertNotifiers.
type Engine struct {
	db          AlertStore
	hub         Broadcaster
	downTimeout time.Duration
	minVersion  string
	notifiers   []ext.AlertNotifier
	pageSize    int
}

// New constructs an Engine with the given store, broadcaster, down-timeout, minimum version threshold, and zero or more notifiers.
func New(db AlertStore, hub Broadcaster, downTimeout time.Duration, minVersion string, notifiers ...ext.AlertNotifier) *Engine {
	return &Engine{
		db:          db,
		hub:         hub,
		downTimeout: downTimeout,
		minVersion:  minVersion,
		notifiers:   notifiers,
		pageSize:    defaultWorkloadPageSize,
	}
}

// Start runs the evaluation loop on the given interval until ctx is cancelled.
func (e *Engine) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.Evaluate()
		case <-ctx.Done():
			return
		}
	}
}

// Evaluate runs all alert rules once across every non-archived workload.
func (e *Engine) Evaluate() {
	if pager, ok := e.db.(pagedWorkloadStore); ok {
		e.evaluatePages(pager)
		return
	}

	workloads, err := e.db.ListWorkloads(false)
	if err != nil {
		log.Printf("alert engine: list workloads: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, w := range workloads {
		e.evaluateWorkloadDown(w, now)
		e.evaluateConfigDrift(w, now)
		e.evaluateVersionOutdated(w, now)
	}
}

func (e *Engine) evaluatePages(pager pagedWorkloadStore) {
	now := time.Now().UTC()
	afterID := ""
	pageSize := e.pageSize
	if pageSize <= 0 {
		pageSize = defaultWorkloadPageSize
	}

	for {
		workloads, err := pager.ListWorkloadsPage(false, afterID, pageSize)
		if err != nil {
			log.Printf("alert engine: list workload page after %q: %v", afterID, err)
			return
		}
		if len(workloads) == 0 {
			return
		}

		for _, w := range workloads {
			e.evaluateWorkloadDown(w, now)
			e.evaluateConfigDrift(w, now)
			e.evaluateVersionOutdated(w, now)
		}

		afterID = workloads[len(workloads)-1].ID
		if len(workloads) < pageSize {
			return
		}
	}
}

func (e *Engine) evaluateWorkloadDown(w models.Workload, now time.Time) {
	isDown := now.Sub(w.LastSeenAt) > e.downTimeout

	existing, err := e.db.GetUnresolvedAlertByWorkloadAndRule(w.ID, "workload_down")
	if err != nil {
		log.Printf("alert engine: check existing alert for %s: %v", w.ID, err)
		return
	}

	if isDown && existing == nil {
		alert := models.Alert{
			ID:         generateID(),
			WorkloadID: w.ID,
			Rule:       "workload_down",
			Severity:   "critical",
			Message:    fmt.Sprintf("Workload %s not seen for %s", w.ID, e.downTimeout),
			FiredAt:    now,
		}
		if err := e.db.CreateAlert(alert); err != nil {
			log.Printf("alert engine: create alert: %v", err)
			return
		}
		if e.hub != nil {
			e.hub.BroadcastAlertUpdate(alert)
		}
		for _, n := range e.notifiers {
			go n.Send(alert)
		}
	}

	if !isDown && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve alert: %v", err)
		}
	}
}

func (e *Engine) evaluateConfigDrift(w models.Workload, now time.Time) {
	pending, err := e.db.GetLatestPendingWorkloadConfig(w.ID)
	if err != nil {
		log.Printf("alert engine: check config drift for %s: %v", w.ID, err)
		return
	}

	// Workload has not applied the config we pushed within the timeout window.
	isDrifted := pending != nil && now.Sub(pending.AppliedAt) > e.downTimeout

	existing, _ := e.db.GetUnresolvedAlertByWorkloadAndRule(w.ID, "config_drift")

	if isDrifted && existing == nil {
		alert := models.Alert{
			ID:         generateID(),
			WorkloadID: w.ID,
			Rule:       "config_drift",
			Severity:   "warning",
			Message:    fmt.Sprintf("Workload %s has not applied config %s after %s", w.ID, pending.ConfigID[:12], e.downTimeout),
			FiredAt:    now,
		}
		if err := e.db.CreateAlert(alert); err != nil {
			log.Printf("alert engine: create config_drift alert: %v", err)
			return
		}
		if e.hub != nil {
			e.hub.BroadcastAlertUpdate(alert)
		}
		for _, n := range e.notifiers {
			go n.Send(alert)
		}
	}

	if !isDrifted && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve config_drift alert: %v", err)
		}
	}
}

func (e *Engine) evaluateVersionOutdated(w models.Workload, now time.Time) {
	// Skip if the minimum version constraint is not configured or the workload
	// has not yet reported its version.
	if e.minVersion == "" || w.Version == "" {
		return
	}

	cmp, ok := version.Compare(w.Version, e.minVersion)
	if !ok {
		return
	}
	isOutdated := cmp < 0

	existing, _ := e.db.GetUnresolvedAlertByWorkloadAndRule(w.ID, "version_outdated")

	if isOutdated && existing == nil {
		alert := models.Alert{
			ID:         generateID(),
			WorkloadID: w.ID,
			Rule:       "version_outdated",
			Severity:   "warning",
			Message:    fmt.Sprintf("Workload %s version %s is below minimum %s", w.ID, w.Version, e.minVersion),
			FiredAt:    now,
		}
		if err := e.db.CreateAlert(alert); err != nil {
			log.Printf("alert engine: create version_outdated alert: %v", err)
			return
		}
		if e.hub != nil {
			e.hub.BroadcastAlertUpdate(alert)
		}
		for _, n := range e.notifiers {
			go n.Send(alert)
		}
	}

	if !isOutdated && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve version_outdated alert: %v", err)
		}
	}
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
