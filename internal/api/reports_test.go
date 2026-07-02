package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestReportsPreviewJSON_OK(t *testing.T) {
	db, router, _, _ := newAuditTestAPI(t)
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
