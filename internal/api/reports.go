package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/internal/reports"
	"github.com/magnify-labs/otel-magnify/internal/version"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (a *API) reportService() *reports.Service {
	return reports.NewService(a.db, reports.ServiceOptions{Signer: a.reportSigner})
}

func (a *API) handlePreviewEvidencePack(w http.ResponseWriter, r *http.Request) {
	req, ok := a.decodeReportRequest(w, r)
	if !ok {
		return
	}
	pack, err := a.reportService().BuildEvidencePack(r.Context(), req)
	if err != nil {
		a.respondReportBuildError(w, err)
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "report.preview", "report", pack.InputsHash, fmt.Sprintf("report_type=%s workloads=%d", req.ReportType, len(pack.Scope.WorkloadIDs))); err != nil {
		respondAuditUnavailable(w, sideEffectNone)
		return
	}
	respondJSON(w, http.StatusOK, pack)
}

func (a *API) handleExportEvidencePack(w http.ResponseWriter, r *http.Request) {
	format := models.ReportExportFormat(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = models.ReportExportMarkdown
	}
	if format == "md" {
		format = models.ReportExportMarkdown
	}
	if format != models.ReportExportMarkdown && format != models.ReportExportCSV && format != models.ReportExportPDF {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported export format", "code": "unsupported_export_format"})
		return
	}
	req, ok := a.decodeReportRequest(w, r)
	if !ok {
		return
	}
	pack, err := a.reportService().BuildEvidencePack(r.Context(), req)
	if err != nil {
		a.respondReportBuildError(w, err)
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "report.export", "report", pack.InputsHash, fmt.Sprintf("format=%s report_type=%s workloads=%d", format, req.ReportType, len(pack.Scope.WorkloadIDs))); err != nil {
		respondAuditUnavailable(w, sideEffectNone)
		return
	}
	var body []byte
	contentType := ""
	suffix := ""
	switch format {
	case models.ReportExportMarkdown:
		body, err = reports.RenderMarkdown(pack)
		contentType = "text/markdown; charset=utf-8"
		suffix = ".md"
	case models.ReportExportCSV:
		body, err = reports.RenderCSV(pack)
		contentType = "text/csv; charset=utf-8"
		suffix = ".csv"
	case models.ReportExportPDF:
		body, err = reports.RenderPDFMinimal(pack)
		contentType = "application/pdf"
		suffix = ".pdf"
	}
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to render report"})
		return
	}
	prefix := pack.InputsHash
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"evidence-pack-%s%s\"", prefix, suffix))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (a *API) decodeReportRequest(w http.ResponseWriter, r *http.Request) (models.ReportExportRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	//nolint:errcheck // request body is fully consumed by json.Decoder; close errors are not actionable here
	defer r.Body.Close()
	var req models.ReportExportRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body", "code": "invalid_json"})
		return req, false
	}
	return req, true
}

func (a *API) respondReportBuildError(w http.ResponseWriter, err error) {
	msg := err.Error()
	if errors.Is(err, reports.ErrInvalidRequest) || strings.Contains(msg, "invalid scope") || strings.Contains(msg, "unsupported") || strings.Contains(msg, "invalid time range") {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if strings.Contains(msg, "workload not found") {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "workload not found"})
		return
	}
	respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to build evidence pack"})
}

const evidenceReportAuditAction = "report.config_safety.export"

type evidenceReportSigner interface {
	SignEvidenceReport(models.EvidenceReport) (models.EvidenceReportSignature, error)
}

type unsignedDigestEvidenceSigner struct{}

func (unsignedDigestEvidenceSigner) SignEvidenceReport(report models.EvidenceReport) (models.EvidenceReportSignature, error) {
	report.Signature = models.EvidenceReportSignature{}
	payload, err := json.Marshal(report)
	if err != nil {
		return models.EvidenceReportSignature{}, err
	}
	sum := sha256.Sum256(payload)
	return models.EvidenceReportSignature{
		Algorithm:           "sha256-unsigned-digest-v1",
		PayloadDigestSHA256: hex.EncodeToString(sum[:]),
		VerificationHint:    "Verify by recomputing SHA-256 over the canonical JSON report payload with the signature field cleared. Enterprise builds may replace this unsigned digest with a detached signature over the same payload.",
	}, nil
}

func (a *API) handleConfigSafetyReport(w http.ResponseWriter, r *http.Request) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}
	scope, err := configSafetyReportScope(r.URL.Query())
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	report, err := a.buildConfigSafetyEvidenceReport(r.URL.Query().Get("recommended_version"), scope, time.Now().UTC(), unsignedDigestEvidenceSigner{})
	if err != nil {
		if errors.Is(err, reports.ErrInvalidRequest) {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to build config safety report")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, evidenceReportAuditAction, "report", report.ReportID, format); err != nil {
		respondAuditUnavailable(w, sideEffectNone)
		return
	}

	filename := fmt.Sprintf("config-safety-evidence-%s.%s", report.ReportID[:12], reportExtension(format))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	switch format {
	case "json":
		respondJSON(w, http.StatusOK, report)
	case "markdown", "md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(renderEvidenceReportMarkdown(report)))
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(renderEvidenceReportCSV(report))
	case "pdf":
		w.Header().Set("Content-Type", "application/pdf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(renderEvidenceReportPDF(report))
	default:
		respondError(w, http.StatusBadRequest, "unsupported report format")
	}
}

func reportExtension(format string) string {
	switch format {
	case "markdown", "md":
		return "md"
	default:
		return format
	}
}

func configSafetyReportScope(q url.Values) (models.ReportScope, error) {
	scope := models.ReportScope{GroupID: strings.TrimSpace(q.Get("group_id"))}
	for _, raw := range append(q["workload_id"], q["workload_ids"]...) {
		for _, id := range strings.Split(raw, ",") {
			if id = strings.TrimSpace(id); id != "" {
				scope.WorkloadIDs = append(scope.WorkloadIDs, id)
			}
		}
	}
	for key, values := range q {
		name, ok := strings.CutPrefix(key, "selector.")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[len(values)-1])
		if value == "" {
			continue
		}
		if scope.Selector == nil {
			scope.Selector = map[string]string{}
		}
		scope.Selector[name] = value
	}
	normalized, err := reports.NormalizeRequest(models.ReportExportRequest{ReportType: models.ReportTypeEvidencePack, Scope: scope})
	if err != nil {
		return models.ReportScope{}, err
	}
	return normalized.Scope, nil
}

func (a *API) buildConfigSafetyEvidenceReport(recommended string, scope models.ReportScope, now time.Time, signer evidenceReportSigner) (models.EvidenceReport, error) {
	workloads, err := a.configSafetyReportWorkloads(scope)
	if err != nil {
		return models.EvidenceReport{}, err
	}
	alerts, err := a.db.ListAlerts(false)
	if err != nil {
		return models.EvidenceReport{}, err
	}
	alertRules := unresolvedAlertRulesByWorkload(alerts)

	report := models.EvidenceReport{
		SchemaVersion:      models.EvidenceReportSchemaVersion,
		GeneratedAt:        now,
		RecommendedVersion: strings.TrimSpace(recommended),
		ConfigChanges:      []models.EvidenceConfigChange{},
		ValidationFailures: []models.EvidenceValidationFailure{},
		Rollbacks:          []models.EvidenceRollback{},
		Drift: models.ConfigDriftDashboard{
			GeneratedAt: now,
			Items:       []models.ConfigDriftItem{},
		},
		OutdatedCollectors: []models.FleetCollectorVersionFinding{},
	}

	recommendedComparable := false
	if report.RecommendedVersion != "" {
		_, recommendedComparable = version.Compare(report.RecommendedVersion, report.RecommendedVersion)
	}

	sort.Slice(workloads, func(i, j int) bool { return workloads[i].ID < workloads[j].ID })
	for _, wl := range workloads {
		if wl.Type != "collector" {
			continue
		}
		a.hydrateCurrentConfigPush(&wl)
		history, err := a.db.GetWorkloadConfigHistory(wl.ID)
		if err != nil {
			return models.EvidenceReport{}, err
		}
		history = sanitizedConfigHistory(history)
		report.ConfigChanges = append(report.ConfigChanges, evidenceConfigChanges(wl, history)...)
		report.ValidationFailures = append(report.ValidationFailures, evidenceValidationFailures(wl, history)...)
		report.Rollbacks = append(report.Rollbacks, evidenceRollbacks(wl, history)...)

		drift := a.buildConfigDriftItem(wl, alertRules[wl.ID], now)
		report.Drift.Items = append(report.Drift.Items, drift)
		summarizeConfigDriftItem(&report.Drift.Summary, drift)

		if recommendedComparable {
			if cmp, ok := version.Compare(wl.Version, report.RecommendedVersion); ok && cmp < 0 {
				report.OutdatedCollectors = append(report.OutdatedCollectors, models.FleetCollectorVersionFinding{WorkloadID: wl.ID, DisplayName: wl.DisplayName, Group: workloadGroup(wl), Version: wl.Version, RecommendedVersion: report.RecommendedVersion})
			}
		}
	}

	sortEvidenceReport(&report)
	report.Summary = models.EvidenceReportSummary{
		ConfigChanges:      len(report.ConfigChanges),
		ValidationFailures: len(report.ValidationFailures),
		Rollbacks:          len(report.Rollbacks),
		DriftedCollectors:  report.Drift.Summary.DriftedCollectors,
		OutdatedCollectors: len(report.OutdatedCollectors),
		AuditEvents:        1,
	}
	report.AuditTrail = []models.EvidenceAuditTrailEntry{{Action: evidenceReportAuditAction, Resource: "report", Detail: "export generated", At: now}}
	report.ReportID = evidenceReportID(report)
	report.AuditTrail[0].ResourceID = report.ReportID
	sig, err := signer.SignEvidenceReport(report)
	if err != nil {
		return models.EvidenceReport{}, err
	}
	report.Signature = sig
	return report, nil
}

func (a *API) configSafetyReportWorkloads(scope models.ReportScope) ([]models.Workload, error) {
	workloads, err := a.db.ListWorkloads(false)
	if err != nil {
		return nil, err
	}
	if len(scope.WorkloadIDs) > 0 {
		wanted := map[string]bool{}
		for _, id := range scope.WorkloadIDs {
			wanted[id] = true
		}
		out := make([]models.Workload, 0, len(wanted))
		for _, wl := range workloads {
			if wanted[wl.ID] {
				out = append(out, wl)
				delete(wanted, wl.ID)
			}
		}
		if len(wanted) > 0 {
			return nil, fmt.Errorf("%w: workload not found", reports.ErrInvalidRequest)
		}
		return out, nil
	}
	out := []models.Workload{}
	for _, wl := range workloads {
		if configSafetyReportMatchesScope(wl, scope) {
			out = append(out, wl)
		}
	}
	return out, nil
}

func configSafetyReportMatchesScope(wl models.Workload, scope models.ReportScope) bool {
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

func sanitizedConfigHistory(history []models.WorkloadConfig) []models.WorkloadConfig {
	out := make([]models.WorkloadConfig, 0, len(history))
	for _, cfg := range history {
		cfg = cfg.SanitizedRemoteConfigErrors()
		cfg.Content = ""
		cfg.ContentAvailable = cfg.ConfigID != ""
		out = append(out, cfg)
	}
	return out
}

func evidenceConfigChanges(wl models.Workload, history []models.WorkloadConfig) []models.EvidenceConfigChange {
	out := make([]models.EvidenceConfigChange, 0, len(history))
	for i, cfg := range history {
		prev := ""
		if i+1 < len(history) {
			prev = history[i+1].ConfigID
		}
		diff := "initial config"
		if prev != "" && prev != cfg.ConfigID {
			diff = "config hash changed"
		} else if prev == cfg.ConfigID {
			diff = "config hash unchanged"
		}
		out = append(out, models.EvidenceConfigChange{WorkloadID: wl.ID, DisplayName: wl.DisplayName, ConfigHash: cfg.ConfigID, PreviousHash: prev, Status: cfg.Status, PushedBy: cfg.PushedBy, AppliedAt: cfg.AppliedAt, ContentAvailable: cfg.ContentAvailable, DiffSummary: diff})
	}
	return out
}

func evidenceValidationFailures(wl models.Workload, history []models.WorkloadConfig) []models.EvidenceValidationFailure {
	out := []models.EvidenceValidationFailure{}
	for _, cfg := range history {
		if cfg.Status != models.PushStatusFailed && cfg.FailedCount == 0 && cfg.ErrorMessage == "" {
			continue
		}
		out = append(out, models.EvidenceValidationFailure{WorkloadID: wl.ID, DisplayName: wl.DisplayName, ConfigHash: cfg.ConfigID, Status: cfg.Status, Error: models.SanitizeRemoteConfigErrorMessage(cfg.ErrorMessage), OccurredAt: cfg.AppliedAt})
	}
	return out
}

func evidenceRollbacks(wl models.Workload, history []models.WorkloadConfig) []models.EvidenceRollback {
	out := []models.EvidenceRollback{}
	for _, cfg := range history {
		if !strings.HasPrefix(cfg.Status, "rollback_") && cfg.RollbackOfPushID == "" {
			continue
		}
		out = append(out, models.EvidenceRollback{WorkloadID: wl.ID, DisplayName: wl.DisplayName, ConfigHash: cfg.ConfigID, RollbackOfPushID: cfg.RollbackOfPushID, Status: cfg.Status, OccurredAt: cfg.AppliedAt})
	}
	return out
}

func sortEvidenceReport(report *models.EvidenceReport) {
	sort.Slice(report.ConfigChanges, func(i, j int) bool {
		return evidenceTimeKey(report.ConfigChanges[i].WorkloadID, report.ConfigChanges[i].AppliedAt) < evidenceTimeKey(report.ConfigChanges[j].WorkloadID, report.ConfigChanges[j].AppliedAt)
	})
	sort.Slice(report.ValidationFailures, func(i, j int) bool {
		return evidenceTimeKey(report.ValidationFailures[i].WorkloadID, report.ValidationFailures[i].OccurredAt) < evidenceTimeKey(report.ValidationFailures[j].WorkloadID, report.ValidationFailures[j].OccurredAt)
	})
	sort.Slice(report.Rollbacks, func(i, j int) bool {
		return evidenceTimeKey(report.Rollbacks[i].WorkloadID, report.Rollbacks[i].OccurredAt) < evidenceTimeKey(report.Rollbacks[j].WorkloadID, report.Rollbacks[j].OccurredAt)
	})
	sort.Slice(report.Drift.Items, func(i, j int) bool { return report.Drift.Items[i].WorkloadID < report.Drift.Items[j].WorkloadID })
	sort.Slice(report.OutdatedCollectors, func(i, j int) bool {
		return report.OutdatedCollectors[i].WorkloadID < report.OutdatedCollectors[j].WorkloadID
	})
}

func evidenceTimeKey(workloadID string, t time.Time) string {
	return workloadID + "\x00" + t.UTC().Format(time.RFC3339Nano)
}

func evidenceReportID(report models.EvidenceReport) string {
	report.ReportID = ""
	report.Signature = models.EvidenceReportSignature{}
	b, _ := json.Marshal(report)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func renderEvidenceReportMarkdown(report models.EvidenceReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Config Safety Evidence Report\n\n")
	fmt.Fprintf(&b, "Report ID: `%s`\n\n", report.ReportID)
	fmt.Fprintf(&b, "Generated at: %s\n\n", report.GeneratedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "## Summary\n\n- Config changes: %d\n- Validation failures: %d\n- Rollbacks: %d\n- Drifted collectors: %d\n- Outdated collectors: %d\n\n", report.Summary.ConfigChanges, report.Summary.ValidationFailures, report.Summary.Rollbacks, report.Summary.DriftedCollectors, report.Summary.OutdatedCollectors)
	fmt.Fprintf(&b, "## Config changes\n\n")
	for _, c := range report.ConfigChanges {
		fmt.Fprintf(&b, "- `%s` %s -> %s (%s)\n", c.WorkloadID, c.PreviousHash, c.ConfigHash, c.Status)
	}
	fmt.Fprintf(&b, "\n## Signature\n\nAlgorithm: `%s`\n\nPayload digest: `%s`\n", report.Signature.Algorithm, report.Signature.PayloadDigestSHA256)
	return b.String()
}

func renderEvidenceReportCSV(report models.EvidenceReport) []byte {
	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)
	_ = w.Write([]string{"section", "workload_id", "display_name", "config_hash", "status", "detail", "occurred_at"})
	for _, c := range report.ConfigChanges {
		_ = w.Write(reports.NeutralizeCSVRecord([]string{"config_change", c.WorkloadID, c.DisplayName, c.ConfigHash, c.Status, c.DiffSummary, c.AppliedAt.UTC().Format(time.RFC3339)}))
	}
	for _, f := range report.ValidationFailures {
		_ = w.Write(reports.NeutralizeCSVRecord([]string{"validation_failure", f.WorkloadID, f.DisplayName, f.ConfigHash, f.Status, f.Error, f.OccurredAt.UTC().Format(time.RFC3339)}))
	}
	for _, r := range report.Rollbacks {
		_ = w.Write(reports.NeutralizeCSVRecord([]string{"rollback", r.WorkloadID, r.DisplayName, r.ConfigHash, r.Status, r.RollbackOfPushID, r.OccurredAt.UTC().Format(time.RFC3339)}))
	}
	for _, d := range report.Drift.Items {
		_ = w.Write(reports.NeutralizeCSVRecord([]string{"drift", d.WorkloadID, d.Collector, d.ExpectedConfigHash, d.DriftStatus, strings.Join(d.DriftReasons, ";"), report.GeneratedAt.UTC().Format(time.RFC3339)}))
	}
	for _, o := range report.OutdatedCollectors {
		_ = w.Write(reports.NeutralizeCSVRecord([]string{"outdated_collector", o.WorkloadID, o.DisplayName, "", "below_recommended", o.Version + " < " + o.RecommendedVersion, report.GeneratedAt.UTC().Format(time.RFC3339)}))
	}
	w.Flush()
	return buf.Bytes()
}

func renderEvidenceReportPDF(report models.EvidenceReport) []byte {
	text := strings.ReplaceAll(renderEvidenceReportMarkdown(report), "\n", "\\n")
	if len(text) > 1800 {
		text = text[:1800]
	}
	stream := fmt.Sprintf("BT /F1 10 Tf 50 780 Td (%s) Tj ET", escapePDFString(text))
	objects := []string{
		"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n",
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n",
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >> endobj\n",
		"4 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n",
		fmt.Sprintf("5 0 obj << /Length %d >> stream\n%s\nendstream endobj\n", len(stream), stream),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = b.Len()
		b.WriteString(obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

func escapePDFString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "(", `\(`)
	s = strings.ReplaceAll(s, ")", `\)`)
	return s
}
