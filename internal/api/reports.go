package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/internal/reports"
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
