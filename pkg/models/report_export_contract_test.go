package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReportExportContractJSONShape(t *testing.T) {
	generatedAt := time.Date(2026, 7, 2, 20, 0, 0, 0, time.UTC)
	signedAt := generatedAt.Add(time.Minute)
	observedAt := generatedAt.Add(-time.Hour)

	req := ReportExportRequest{
		SchemaVersion: ReportExportRequestSchemaVersion,
		ReportType:    EvidencePackReportType,
		Scope: ReportScope{
			WorkloadIDs: []string{"collector-prod"},
			Since:       &observedAt,
			Until:       &generatedAt,
		},
		Include: ReportIncludeOptions{
			WorkloadSummary:     true,
			ConfigHistory:       true,
			CurrentConfig:       true,
			Alerts:              true,
			AuditVerification:   true,
			SignedAuditMetadata: true,
		},
		Redaction: ReportRedactionStrict,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if got := decoded["schema_version"]; got != "report_export_request.v1" {
		t.Fatalf("schema_version = %v", got)
	}
	if got := decoded["report_type"]; got != "evidence_pack" {
		t.Fatalf("report_type = %v", got)
	}
	include := decoded["include"].(map[string]any)
	for _, field := range []string{"workload_summary", "config_history", "current_config", "alerts", "audit_verification", "signed_audit_metadata"} {
		if include[field] != true {
			t.Fatalf("include.%s = %v", field, include[field])
		}
	}

	pack := EvidencePack{
		SchemaVersion: EvidencePackSchemaVersion,
		GeneratedAt:   generatedAt,
		InputsHash:    "inputs-hash",
		ReportHash:    "report-hash",
		Scope: ReportScopeResolved{
			WorkloadIDs:    []string{"collector-prod"},
			WorkloadCount:  1,
			RequestedScope: req.Scope,
		},
		Sections: []EvidenceSection{{
			ID:    "audit_verification",
			Title: "Audit verification",
			Order: 90,
			Items: []EvidenceItem{{
				ID:          "audit/head",
				Resource:    "audit",
				ResourceID:  "head",
				ObservedAt:  &observedAt,
				Severity:    "info",
				Summary:     "audit chain verified",
				Facts:       map[string]any{"head_hash": "abc123", "verified": true},
				ContentHash: "content-hash",
				Redacted:    true,
			}},
			CSVTable: &EvidenceTable{
				Columns: ReportExportCSVColumns,
				Rows:    [][]string{{"audit_verification", "audit/head", "audit", "head", observedAt.Format(time.RFC3339), "info", "audit chain verified", "verified", "true", "content-hash", "true"}},
			},
		}},
		Signatures: []ReportSignature{{
			Scheme:       ReportSignatureSchemeNone,
			SignedAt:     signedAt,
			PayloadHash:  "report-hash",
			SignatureB64: "",
			Verifier:     ReportSignatureVerifierCommunityNone,
		}},
		SignedAudit: &SignedAuditReportMetadata{
			Status:           "verified",
			Verifier:         "enterprise-audit-chain",
			VerifiedFrom:     &observedAt,
			VerifiedUntil:    &generatedAt,
			HeadHash:         "audit-head",
			CheckedAt:        signedAt,
			FirstBadSequence: 0,
		},
		Warnings: []ReportWarning{{Code: "pdf_minimal_renderer", Message: "PDF renderer is text-only"}},
	}

	body, err := json.Marshal(pack)
	if err != nil {
		t.Fatalf("marshal pack: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal pack: %v", err)
	}
	if out["schema_version"] != "evidence_pack.v1" {
		t.Fatalf("schema_version = %v", out["schema_version"])
	}
	if out["signed_audit"] == nil {
		t.Fatalf("signed_audit metadata missing from response JSON: %s", body)
	}
	if out["signatures"] == nil {
		t.Fatalf("signatures missing from response JSON: %s", body)
	}
}

func TestReportExportContentTypesAndErrorsContract(t *testing.T) {
	contentTypes := ReportExportContentTypes()
	cases := map[ReportExportFormat]string{
		ReportExportMarkdown: "text/markdown; charset=utf-8",
		ReportExportCSV:      "text/csv; charset=utf-8",
		ReportExportPDF:      "application/pdf",
	}
	for format, want := range cases {
		if got := contentTypes[format]; got != want {
			t.Fatalf("content type for %s = %q, want %q", format, got, want)
		}
	}

	if got := ReportExportErrorResponses[ReportExportErrorPDFUnavailable].FallbackFormat; got != ReportExportMarkdown {
		t.Fatalf("pdf unavailable fallback = %q", got)
	}
	if ReportExportErrorResponses[ReportExportErrorInvalidScope].HTTPStatus != 400 {
		t.Fatalf("invalid scope HTTP status = %d", ReportExportErrorResponses[ReportExportErrorInvalidScope].HTTPStatus)
	}
	if ReportExportErrorResponses[ReportExportErrorBodyTooLarge].HTTPStatus != 413 {
		t.Fatalf("body too large HTTP status = %d", ReportExportErrorResponses[ReportExportErrorBodyTooLarge].HTTPStatus)
	}
	if ReportExportErrorResponses[ReportExportErrorAuditUnavailable].HTTPStatus != 503 {
		t.Fatalf("audit unavailable HTTP status = %d", ReportExportErrorResponses[ReportExportErrorAuditUnavailable].HTTPStatus)
	}
}
