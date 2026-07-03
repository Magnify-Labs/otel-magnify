package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func seedReportAPIFixture(t *testing.T, db interface{ UpsertWorkload(models.Workload) error }) {
	t.Helper()
	at := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := db.UpsertWorkload(models.Workload{ID: "w1", DisplayName: "collector-a", Type: "collector", Status: "connected", Version: "0.99.0", LastSeenAt: at, Labels: models.Labels{}, FingerprintKeys: models.FingerprintKeys{}, AcceptsRemoteConfig: true}); err != nil {
		t.Fatal(err)
	}
}

func TestReportsExportMarkdown_OKAuditedAndRedacted(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedReportAPIFixture(t, db)

	body := `{"report_type":"evidence_pack","scope":{"workload_ids":["w1"]}}`
	req := authedRequestForGroups(t, http.MethodPost, "/api/reports/evidence-pack/export?format=markdown", body, []string{"editor"})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("content-type=%q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "evidence-pack-") || !strings.HasSuffix(cd, ".md\"") {
		t.Fatalf("content-disposition=%q", cd)
	}
	if !strings.Contains(rec.Body.String(), "# Evidence Pack") || strings.Contains(rec.Body.String(), "SECRET_TOKEN") {
		t.Fatalf("unexpected markdown body: %s", rec.Body.String())
	}
	if got := findEvent(audit.snapshot(), "report.export"); got == nil || got.Resource != "report" || got.ResourceID == "" {
		t.Fatalf("missing report.export audit: %+v", audit.snapshot())
	}
}

func TestReportsPreviewJSON_OKAudited(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedReportAPIFixture(t, db)

	body := `{"report_type":"evidence_pack","scope":{"workload_ids":["w1"]}}`
	req := authedRequestForGroups(t, http.MethodPost, "/api/reports/evidence-pack", body, []string{"administrator"})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pack models.EvidencePack
	if err := json.NewDecoder(rec.Body).Decode(&pack); err != nil {
		t.Fatal(err)
	}
	if pack.SchemaVersion != models.EvidencePackSchemaVersion || len(pack.Sections) == 0 || len(pack.Signatures) != 1 {
		t.Fatalf("unexpected pack: %+v", pack)
	}
	if got := findEvent(audit.snapshot(), "report.preview"); got == nil || got.Resource != "report" || got.ResourceID == "" {
		t.Fatalf("missing report.preview audit: %+v", audit.snapshot())
	}
}

func TestReportsExportCSVAndPDF_OK(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
	seedReportAPIFixture(t, db)
	for _, tc := range []struct{ format, contentType, suffix string }{{"csv", "text/csv", ".csv\""}, {"pdf", "application/pdf", ".pdf\""}} {
		req := authedRequestForGroups(t, http.MethodPost, "/api/reports/evidence-pack/export?format="+tc.format, `{"report_type":"evidence_pack","scope":{"workload_ids":["w1"]}}`, []string{"administrator"})
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", tc.format, rec.Code, rec.Body.String())
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), tc.contentType) || !strings.HasSuffix(rec.Header().Get("Content-Disposition"), tc.suffix) {
			t.Fatalf("%s headers: ct=%q cd=%q", tc.format, rec.Header().Get("Content-Type"), rec.Header().Get("Content-Disposition"))
		}
	}
}

func TestReportsExportRejectsViewerAndInvalidScope(t *testing.T) {
	_, router, _, _ := newAuditTestAPI(t)
	req := authedRequestForGroups(t, http.MethodPost, "/api/reports/evidence-pack/export?format=markdown", `{"report_type":"evidence_pack","scope":{"workload_ids":["w1"]}}`, []string{"viewer"})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = authedRequestForGroups(t, http.MethodPost, "/api/reports/evidence-pack/export?format=markdown", `{"report_type":"evidence_pack","scope":{}}`, []string{"administrator"})
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid scope") {
		t.Fatalf("invalid scope status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReportsPreviewAuditFailureReturns503BeforeJSON(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedReportAPIFixture(t, db)
	audit.failWith(errors.New("audit unavailable"))
	req := authedRequestForGroups(t, http.MethodPost, "/api/reports/evidence-pack", `{"report_type":"evidence_pack","scope":{"workload_ids":["w1"]}}`, []string{"administrator"})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), models.EvidencePackSchemaVersion) || !strings.Contains(rec.Body.String(), "none") {
		t.Fatalf("audit failure response status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReportsExportAuditFailureReturns503BeforeBytes(t *testing.T) {
	db, router, _, audit := newAuditTestAPI(t)
	seedReportAPIFixture(t, db)
	audit.failWith(errors.New("audit unavailable"))
	req := authedRequestForGroups(t, http.MethodPost, "/api/reports/evidence-pack/export?format=markdown", `{"report_type":"evidence_pack","scope":{"workload_ids":["w1"]}}`, []string{"administrator"})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "# Evidence Pack") || !strings.Contains(rec.Body.String(), "none") {
		t.Fatalf("audit failure response status=%d body=%s", rec.Code, rec.Body.String())
	}
}

const reportConfigWithSecret = `receivers:
  otlp: {}
exporters:
  otlp:
    endpoint: https://tenant-a.internal:4317
    headers:
      authorization: Bearer SECRET_TOKEN
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlp]
`

func seedReportWorkload(t *testing.T, db ext.Store, fake *fakeOpAMPPusher) {
	t.Helper()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if err := db.CreateConfig(models.Config{ID: "cfg-old", Name: "collector-old", Content: "receivers: {}", CreatedAt: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatalf("CreateConfig old: %v", err)
	}
	if err := db.CreateConfig(models.Config{ID: "cfg-secret", Name: "collector-secret", Content: reportConfigWithSecret, CreatedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatalf("CreateConfig secret: %v", err)
	}
	wl := models.Workload{
		ID: "w-report", DisplayName: "collector-prod", Type: "collector", Version: "0.99.0", Status: "connected",
		LastSeenAt: now, Labels: models.Labels{"env": "prod", "group": "checkout"}, ActiveConfigHash: "cfg-secret", AcceptsRemoteConfig: true,
		AvailableComponents: &models.AvailableComponents{Hash: "components-v1", Components: map[string][]string{"receivers": {"otlp"}, "exporters": {"logging"}}},
	}
	if err := db.UpsertWorkload(wl); err != nil {
		t.Fatalf("UpsertWorkload: %v", err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w-report", ConfigID: "cfg-old", AppliedAt: now.Add(-90 * time.Minute), Status: models.PushStatusApplied, PushedBy: "ops@example.com"}); err != nil {
		t.Fatalf("Record old config: %v", err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w-report", ConfigID: "cfg-secret", AppliedAt: now.Add(-45 * time.Minute), Status: models.PushStatusFailed, ErrorMessage: "collector failed: exporters.otlp.headers.authorization=Bearer SECRET_TOKEN endpoint=https://tenant-a.internal:4317", PushedBy: "ops@example.com"}); err != nil {
		t.Fatalf("Record failed config: %v", err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "w-report", ConfigID: "cfg-old", AppliedAt: now.Add(-30 * time.Minute), Status: models.PushStatusRollbackApplied, RollbackOfPushID: "push-secret"}); err != nil {
		t.Fatalf("Record rollback config: %v", err)
	}
	if err := db.CreateAlert(models.Alert{ID: "alert-drift", WorkloadID: "w-report", Rule: "config_drift", Severity: "critical", Message: "collector drifted", FiredAt: now.Add(-20 * time.Minute)}); err != nil {
		t.Fatalf("Create drift alert: %v", err)
	}
	if err := db.CreateAlert(models.Alert{ID: "alert-version", WorkloadID: "w-report", Rule: "version_outdated", Severity: "warning", Message: "collector below recommended", FiredAt: now.Add(-10 * time.Minute)}); err != nil {
		t.Fatalf("Create version alert: %v", err)
	}
	fake.instances["w-report"] = []opamp.Instance{{InstanceUID: "i-1", EffectiveConfigHash: "cfg-live-drift"}}
}

func TestConfigSafetyReportJSONIncludesEvidenceAndUnsignedDigest(t *testing.T) {
	db, router, fake, recAudit := newAuditTestAPI(t)
	seedReportWorkload(t, db, fake)

	req := authedRequest(t, http.MethodGet, "/api/reports/config-safety?format=json&recommended_version=1.0.0")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"SECRET_TOKEN", "tenant-a.internal", "authorization=Bearer"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("report leaked sensitive value %q: %s", forbidden, body)
		}
	}
	var report models.EvidenceReport
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.SchemaVersion != "config_safety_evidence_report.v1" || report.ReportID == "" {
		t.Fatalf("identity fields not populated: %+v", report)
	}
	if report.Summary.ConfigChanges != 3 || report.Summary.ValidationFailures != 1 || report.Summary.Rollbacks != 1 || report.Summary.DriftedCollectors != 1 || report.Summary.OutdatedCollectors != 1 {
		t.Fatalf("summary = %+v", report.Summary)
	}
	if len(report.ConfigChanges) != 3 || len(report.ValidationFailures) != 1 || len(report.Rollbacks) != 1 || len(report.Drift.Items) != 1 || len(report.OutdatedCollectors) != 1 {
		t.Fatalf("evidence sections incomplete: %+v", report)
	}
	if report.Signature.Algorithm != "sha256-unsigned-digest-v1" || report.Signature.PayloadDigestSHA256 == "" || report.Signature.VerificationHint == "" {
		t.Fatalf("signature metadata incomplete: %+v", report.Signature)
	}
	if len(report.AuditTrail) == 0 || report.AuditTrail[0].Action != "report.config_safety.export" {
		t.Fatalf("audit trail not populated: %+v", report.AuditTrail)
	}
	events := recAudit.snapshot()
	if len(events) != 1 || events[0].Action != "report.config_safety.export" {
		t.Fatalf("audit events = %+v", events)
	}
}

func TestConfigSafetyReportExportsMarkdownCSVAndPDF(t *testing.T) {
	db, router, fake, _ := newAuditTestAPI(t)
	seedReportWorkload(t, db, fake)

	cases := []struct {
		format      string
		contentType string
		want        string
	}{
		{format: "markdown", contentType: "text/markdown", want: "# Config Safety Evidence Report"},
		{format: "csv", contentType: "text/csv", want: "section,workload_id,display_name"},
		{format: "pdf", contentType: "application/pdf", want: "%PDF-1.4"},
	}
	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			req := authedRequest(t, http.MethodGet, "/api/reports/config-safety?format="+tc.format+"&recommended_version=1.0.0")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); !strings.Contains(got, tc.contentType) {
				t.Fatalf("content-type = %q, want %q", got, tc.contentType)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body missing %q: %s", tc.want, rec.Body.String())
			}
		})
	}
}
