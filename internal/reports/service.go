package reports

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// ErrInvalidRequest marks validation failures for report export requests.
var ErrInvalidRequest = errors.New("invalid report request")

// ServiceOptions configures report evidence generation.
type ServiceOptions struct {
	Now    func() time.Time
	Signer ext.ReportSigner
}

// Service builds deterministic report evidence packs from the store.
type Service struct {
	store  ext.Store
	now    func() time.Time
	signer ext.ReportSigner
}

// NewService creates a report evidence service over the provided store.
func NewService(store ext.Store, opts ServiceOptions) *Service {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	signer := opts.Signer
	if signer == nil {
		signer = ext.NopReportSigner{}
	}
	return &Service{store: store, now: now, signer: signer}
}

// NormalizeRequest applies defaults and validates report export request scope.
func NormalizeRequest(req models.ReportExportRequest) (models.ReportExportRequest, error) {
	if req.SchemaVersion == "" {
		req.SchemaVersion = models.ReportExportRequestSchemaVersion
	}
	if req.ReportType == "" {
		req.ReportType = models.ReportTypeEvidencePack
	}
	if req.Redaction == "" {
		req.Redaction = models.ReportRedactionStrict
	}
	if req.SchemaVersion != models.ReportExportRequestSchemaVersion {
		return req, fmt.Errorf("%w: unsupported schema_version", ErrInvalidRequest)
	}
	if req.ReportType != models.ReportTypeEvidencePack {
		return req, fmt.Errorf("%w: unsupported report_type", ErrInvalidRequest)
	}
	if req.Redaction != models.ReportRedactionStrict {
		return req, fmt.Errorf("%w: unsupported redaction mode", ErrInvalidRequest)
	}
	count := 0
	if len(req.Scope.WorkloadIDs) > 0 {
		count++
	}
	if strings.TrimSpace(req.Scope.GroupID) != "" {
		count++
	}
	if len(req.Scope.Selector) > 0 {
		count++
	}
	if count != 1 {
		return req, fmt.Errorf("%w: invalid scope: exactly one of workload_ids, group_id, selector is required", ErrInvalidRequest)
	}
	if req.Scope.Since != nil && req.Scope.Until != nil && req.Scope.Since.After(*req.Scope.Until) {
		return req, fmt.Errorf("%w: invalid time range", ErrInvalidRequest)
	}
	if req.Include == (models.ReportIncludeOptions{}) {
		req.Include = models.ReportIncludeOptions{WorkloadSummary: true, ConfigHistory: true, CurrentConfig: true, ConfigPlan: true, DriftFindings: true, VersionIntelligence: true, Alerts: true, WorkloadEvents: true, RollbackReadiness: true, AuditVerification: true}
	}
	return req, nil
}

// BuildEvidencePack aggregates current store state into a deterministic evidence pack.
func (s *Service) BuildEvidencePack(ctx context.Context, req models.ReportExportRequest) (models.EvidencePack, error) {
	req, err := NormalizeRequest(req)
	if err != nil {
		return models.EvidencePack{}, err
	}
	workloads, err := s.resolveWorkloads(req.Scope)
	if err != nil {
		return models.EvidencePack{}, err
	}
	ids := make([]string, 0, len(workloads))
	for _, wl := range workloads {
		ids = append(ids, wl.ID)
	}
	pack := models.EvidencePack{SchemaVersion: models.EvidencePackSchemaVersion, GeneratedAt: s.now().UTC(), Scope: models.ReportScopeResolved{WorkloadIDs: ids, GroupID: req.Scope.GroupID, Selector: req.Scope.Selector, Since: req.Scope.Since, Until: req.Scope.Until}, Sections: []models.EvidenceSection{}}
	if req.Include.WorkloadSummary {
		pack.Sections = append(pack.Sections, s.workloadSection(workloads))
	}
	if req.Include.ConfigHistory {
		pack.Sections = append(pack.Sections, s.configHistorySection(workloads))
	}
	if req.Include.CurrentConfig {
		pack.Sections = append(pack.Sections, s.currentConfigSection(workloads))
	}
	if req.Include.ConfigPlan {
		pack.Warnings = append(pack.Warnings, models.ReportWarning{Code: "config_plan_not_persisted", Message: "Config application plans are request-time previews; no persisted candidate plan is available for this evidence pack."})
	}
	if req.Include.DriftFindings {
		pack.Sections = append(pack.Sections, s.driftSection(workloads))
	}
	if req.Include.VersionIntelligence {
		pack.Sections = append(pack.Sections, s.versionSection(workloads))
	}
	if req.Include.Alerts {
		pack.Sections = append(pack.Sections, s.alertSection())
	}
	if req.Include.WorkloadEvents {
		pack.Sections = append(pack.Sections, s.eventsSection(workloads, req.Scope.Since))
	}
	if req.Include.RollbackReadiness {
		pack.Sections = append(pack.Sections, s.rollbackReadinessSection(workloads))
	}
	if req.Include.AuditVerification {
		pack.Sections = append(pack.Sections, s.auditVerificationSection())
	}
	sort.Slice(pack.Sections, func(i, j int) bool { return pack.Sections[i].Order < pack.Sections[j].Order })
	pack.InputsHash = hashCanonical(map[string]any{"request": req, "workload_ids": ids, "sections": pack.Sections})
	canonical, reportHash, err := canonicalReportPayload(pack)
	if err != nil {
		return models.EvidencePack{}, err
	}
	pack.ReportHash = reportHash
	if s.signer != nil {
		sig, err := s.signer.SignReport(ctx, pack.ReportHash, canonical)
		if err != nil {
			return models.EvidencePack{}, err
		}
		if sig.PayloadHash == "" {
			sig.PayloadHash = pack.ReportHash
		}
		pack.Signatures = []models.ReportSignature{sig}
	}
	return pack, nil
}

func (s *Service) resolveWorkloads(scope models.ReportScope) ([]models.Workload, error) {
	var out []models.Workload
	if len(scope.WorkloadIDs) > 0 {
		seen := map[string]bool{}
		for _, id := range scope.WorkloadIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			wl, err := s.store.GetWorkload(id)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, fmt.Errorf("workload not found")
				}
				return nil, err
			}
			if wl.ArchivedAt == nil {
				out = append(out, wl)
			}
		}
	} else {
		items, err := s.store.ListWorkloads(false)
		if err != nil {
			return nil, err
		}
		for _, wl := range items {
			if matchesScope(wl, scope) {
				out = append(out, wl)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].DisplayName), strings.ToLower(out[j].DisplayName)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func matchesScope(wl models.Workload, scope models.ReportScope) bool {
	if scope.GroupID != "" && wl.Labels["group"] != scope.GroupID {
		return false
	}
	for k, v := range scope.Selector {
		if wl.Labels[k] != v && wl.FingerprintKeys[k] != v {
			return false
		}
	}
	return true
}

func (s *Service) workloadSection(workloads []models.Workload) models.EvidenceSection {
	sec := models.EvidenceSection{ID: "workloads", Title: "Workloads", Order: 10}
	for _, wl := range workloads {
		at := wl.LastSeenAt.UTC()
		facts := map[string]any{"display_name": wl.DisplayName, "type": wl.Type, "version": wl.Version, "status": wl.Status, "accepts_remote_config": wl.AcceptsRemoteConfig, "active_config_hash": wl.ActiveConfigHash, "labels": trustedLabels(wl.Labels)}
		sec.Items = append(sec.Items, item("workload", wl.ID, "workload", wl.ID, &at, "", fmt.Sprintf("%s (%s) is %s", wl.DisplayName, wl.Type, wl.Status), facts, "", true))
	}
	return sec
}

func (s *Service) configHistorySection(workloads []models.Workload) models.EvidenceSection {
	sec := models.EvidenceSection{ID: "config_history", Title: "Config history", Order: 20}
	for _, wl := range workloads {
		h, err := s.store.GetWorkloadConfigHistory(wl.ID)
		if err != nil {
			continue
		}
		sort.Slice(h, func(i, j int) bool {
			if !h[i].AppliedAt.Equal(h[j].AppliedAt) {
				return h[i].AppliedAt.After(h[j].AppliedAt)
			}
			if h[i].ConfigID != h[j].ConfigID {
				return h[i].ConfigID < h[j].ConfigID
			}
			return h[i].PushID < h[j].PushID
		})
		for _, c := range h {
			at := c.AppliedAt.UTC()
			facts := map[string]any{"workload_id": wl.ID, "config_id": c.ConfigID, "status": c.Status, "pushed_by": redactValue(c.PushedBy), "label": labelValue(c.Label), "target_count": c.TargetCount, "applied_count": c.AppliedCount, "failed_count": c.FailedCount, "content_available": c.Content != "" || c.ContentAvailable, "error_message": RedactString(c.ErrorMessage)}
			sec.Items = append(sec.Items, item("config", wl.ID+":"+c.ConfigID+":"+c.PushID, "config", c.ConfigID, &at, c.Status, "config "+c.ConfigID+" "+c.Status, facts, contentHash(c.Content), true))
		}
	}
	return sec
}
func (s *Service) currentConfigSection(workloads []models.Workload) models.EvidenceSection {
	sec := models.EvidenceSection{ID: "current_config", Title: "Current config", Order: 25}
	for _, wl := range workloads {
		cfg, err := s.store.GetLatestWorkloadConfig(wl.ID)
		if err != nil || cfg == nil {
			continue
		}
		at := cfg.AppliedAt.UTC()
		facts := map[string]any{
			"workload_id":           wl.ID,
			"config_id":             cfg.ConfigID,
			"config_hash":           cfg.ConfigHash,
			"status":                cfg.Status,
			"target_count":          cfg.TargetCount,
			"applied_count":         cfg.AppliedCount,
			"failed_count":          cfg.FailedCount,
			"pending_count":         cfg.PendingCount,
			"timed_out_count":       cfg.TimedOutCount,
			"no_status_count":       cfg.NoStatusCount,
			"content_available":     cfg.Content != "" || cfg.ContentAvailable,
			"error_message":         RedactString(cfg.ErrorMessage),
			"rollback_of_push_id":   cfg.RollbackOfPushID,
			"instance_status_count": len(cfg.InstanceStatuses),
		}
		sec.Items = append(sec.Items, item("current_config", wl.ID+":"+cfg.ConfigID+":"+cfg.PushID, "config", cfg.ConfigID, &at, cfg.Status, "current config "+cfg.ConfigID+" "+cfg.Status, facts, contentHash(cfg.Content), true))
	}
	return sec
}

func (s *Service) driftSection(workloads []models.Workload) models.EvidenceSection {
	sec := models.EvidenceSection{ID: "drift", Title: "Drift state", Order: 30}
	for _, wl := range workloads {
		facts := map[string]any{"expected_config_hash": wl.ActiveConfigHash, "effective_config_hash": "", "drift_status": "unknown"}
		if wl.RemoteConfigStatus != nil {
			facts["effective_config_hash"] = wl.RemoteConfigStatus.ConfigHash
			if wl.ActiveConfigHash != "" && wl.RemoteConfigStatus.ConfigHash != "" && wl.ActiveConfigHash != wl.RemoteConfigStatus.ConfigHash {
				facts["drift_status"] = "drifted"
			} else {
				facts["drift_status"] = "in_sync"
			}
		}
		sec.Items = append(sec.Items, item("drift", wl.ID, "workload", wl.ID, nil, "", fmt.Sprintf("drift state for %s", wl.DisplayName), facts, "", true))
	}
	return sec
}
func (s *Service) versionSection(workloads []models.Workload) models.EvidenceSection {
	sec := models.EvidenceSection{ID: "version_intelligence", Title: "Version intelligence", Order: 40}
	for _, wl := range workloads {
		severity := ""
		if strings.TrimSpace(wl.Version) == "" {
			severity = "warning"
		}
		sec.Items = append(sec.Items, item("version", wl.ID, "workload", wl.ID, nil, severity, "collector version "+nonempty(wl.Version, "unknown"), map[string]any{"version": wl.Version, "type": wl.Type}, "", true))
	}
	return sec
}
func (s *Service) alertSection() models.EvidenceSection {
	sec := models.EvidenceSection{ID: "alerts", Title: "Alerts", Order: 50}
	alerts, err := s.store.ListAlerts(false)
	if err != nil {
		return sec
	}
	sort.Slice(alerts, func(i, j int) bool {
		if !alerts[i].FiredAt.Equal(alerts[j].FiredAt) {
			return alerts[i].FiredAt.After(alerts[j].FiredAt)
		}
		return alerts[i].ID < alerts[j].ID
	})
	for _, a := range alerts {
		at := a.FiredAt.UTC()
		sec.Items = append(sec.Items, item("alert", a.ID, "alert", a.ID, &at, a.Severity, RedactString(a.Message), map[string]any{"workload_id": a.WorkloadID, "rule": a.Rule, "resolved": a.ResolvedAt != nil}, "", true))
	}
	return sec
}
func (s *Service) eventsSection(workloads []models.Workload, since *time.Time) models.EvidenceSection {
	sec := models.EvidenceSection{ID: "workload_events", Title: "Workload events", Order: 60}
	for _, wl := range workloads {
		var st time.Time
		if since != nil {
			st = *since
		}
		events, err := s.store.ListWorkloadEvents(wl.ID, 500, st)
		if err != nil {
			continue
		}
		sort.Slice(events, func(i, j int) bool {
			if !events[i].OccurredAt.Equal(events[j].OccurredAt) {
				return events[i].OccurredAt.After(events[j].OccurredAt)
			}
			return events[i].ID < events[j].ID
		})
		for _, e := range events {
			at := e.OccurredAt.UTC()
			sec.Items = append(sec.Items, item("event", fmt.Sprintf("%d", e.ID), "event", fmt.Sprintf("%d", e.ID), &at, "", e.EventType, map[string]any{"workload_id": e.WorkloadID, "instance_uid": e.InstanceUID, "pod_name": e.PodName, "version": e.Version, "prev_version": e.PrevVersion}, "", true))
		}
	}
	return sec
}

func (s *Service) rollbackReadinessSection(workloads []models.Workload) models.EvidenceSection {
	sec := models.EvidenceSection{ID: "rollback_readiness", Title: "Rollback readiness", Order: 70}
	for _, wl := range workloads {
		currentHash := wl.ActiveConfigHash
		if currentHash == "" {
			if cfg, err := s.store.GetLatestWorkloadConfig(wl.ID); err == nil && cfg != nil {
				currentHash = cfg.ConfigID
			}
		}
		target, err := s.store.GetRollbackTarget(wl.ID, currentHash)
		ready := err == nil && target != nil
		facts := map[string]any{"workload_id": wl.ID, "ready": ready, "current_config_hash": currentHash}
		severity := "warning"
		summary := "no rollback target available for " + wl.DisplayName
		resourceID := wl.ID
		var observed *time.Time
		if ready {
			severity = ""
			resourceID = target.Config.ConfigID
			at := target.Config.AppliedAt.UTC()
			observed = &at
			facts["target_kind"] = target.Kind
			facts["target_config_id"] = target.Config.ConfigID
			facts["target_config_hash"] = target.Config.ConfigHash
			facts["content_available"] = target.Config.Content != "" || target.Config.ContentAvailable
			summary = "rollback target " + target.Config.ConfigID + " (" + target.Kind + ")"
		}
		sec.Items = append(sec.Items, item("rollback", wl.ID, "config", resourceID, observed, severity, summary, facts, "", true))
	}
	return sec
}

func (s *Service) auditVerificationSection() models.EvidenceSection {
	return models.EvidenceSection{ID: "audit_verification", Title: "Audit verification", Order: 80, Items: []models.EvidenceItem{
		item("audit_verification", "community-hook", "audit", "community-hook", nil, "info", "audit event export is available through the enterprise verifier hook; community store does not persist queryable audit events", map[string]any{"verifier_hook": "ext.ReportSigner.VerifyReport", "community_audit_events_persisted": false}, "", true),
	}}
}

func item(prefix, id, resource, rid string, at *time.Time, severity, summary string, facts map[string]any, ch string, redacted bool) models.EvidenceItem {
	if facts == nil {
		facts = map[string]any{}
	}
	return models.EvidenceItem{ID: prefix + ":" + id, Resource: resource, ResourceID: rid, ObservedAt: at, Severity: severity, Summary: RedactString(summary), Facts: redactFacts(facts), ContentHash: ch, Redacted: redacted}
}
func trustedLabels(labels models.Labels) map[string]string {
	out := map[string]string{}
	for k, v := range labels {
		if strings.HasPrefix(k, models.TrustedSelectorLabelPrefix) {
			out[k] = RedactString(v)
		}
	}
	return out
}
func labelValue(p *string) string {
	if p == nil {
		return ""
	}
	return RedactString(*p)
}
func nonempty(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
func contentHash(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
func hashCanonical(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func canonicalReportPayload(pack models.EvidencePack) ([]byte, string, error) {
	clone := pack
	clone.ReportHash = ""
	clone.Signatures = nil
	b, err := json.Marshal(clone)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}
