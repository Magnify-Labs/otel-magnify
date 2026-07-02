package models

import "time"

const (
	// ReportExportRequestSchemaVersion is the v1 request schema for report exports.
	ReportExportRequestSchemaVersion = "report_export_request.v1"
	// ReportExportRequestSchemaVersionV1 is an explicit alias for the v1 request schema.
	ReportExportRequestSchemaVersionV1 = ReportExportRequestSchemaVersion

	// EvidencePackSchemaVersion is the v1 response schema for evidence packs.
	EvidencePackSchemaVersion = "evidence_pack.v1"
	// EvidencePackSchemaVersionV1 is an explicit alias for the v1 evidence pack schema.
	EvidencePackSchemaVersionV1 = EvidencePackSchemaVersion

	// EvidencePackReportType is the report_type value for evidence pack exports.
	EvidencePackReportType = "evidence_pack"
	// ReportTypeEvidencePack is an explicit alias for evidence pack report_type.
	ReportTypeEvidencePack = EvidencePackReportType
)

// ReportExportFormat identifies the wire format rendered by the export endpoint.
type ReportExportFormat string

// Report export formats supported by the v1 contract.
const (
	ReportExportMarkdown ReportExportFormat = "markdown"

	// ReportExportCSV renders the evidence pack as a deterministic flat CSV table.
	ReportExportCSV ReportExportFormat = "csv"
	// ReportExportPDF renders the evidence pack as a deterministic PDF artifact.
	ReportExportPDF ReportExportFormat = "pdf"
)

// ReportRedactionMode controls how sensitive report evidence is redacted before rendering/signing.
type ReportRedactionMode string

// Report redaction modes supported by the v1 contract.
const (
	ReportRedactionStrict ReportRedactionMode = "strict"

	// ReportRedactionNone is reserved for future privileged unredacted exports.
	ReportRedactionNone ReportRedactionMode = "none"
)

// ReportExportRequest is the JSON body accepted by report preview/export endpoints.
type ReportExportRequest struct {
	SchemaVersion string               `json:"schema_version"`
	ReportType    string               `json:"report_type"`
	Scope         ReportScope          `json:"scope"`
	Include       ReportIncludeOptions `json:"include"`
	Redaction     ReportRedactionMode  `json:"redaction"`
}

// ReportScope describes the caller-requested report scope.
type ReportScope struct {
	WorkloadIDs []string          `json:"workload_ids,omitempty"`
	GroupID     string            `json:"group_id,omitempty"`
	Selector    map[string]string `json:"selector,omitempty"`
	Since       *time.Time        `json:"since,omitempty"`
	Until       *time.Time        `json:"until,omitempty"`
}

// ReportIncludeOptions selects the report sections included in an evidence pack.
type ReportIncludeOptions struct {
	WorkloadSummary     bool `json:"workload_summary"`
	ConfigHistory       bool `json:"config_history"`
	CurrentConfig       bool `json:"current_config"`
	ConfigPlan          bool `json:"config_plan"`
	DriftFindings       bool `json:"drift_findings"`
	VersionIntelligence bool `json:"version_intelligence"`
	Alerts              bool `json:"alerts"`
	WorkloadEvents      bool `json:"workload_events"`
	RollbackReadiness   bool `json:"rollback_readiness"`
	AuditVerification   bool `json:"audit_verification"`
	SignedAuditMetadata bool `json:"signed_audit_metadata"`
}

// ReportScopeResolved records the normalized resources represented in the report.
type ReportScopeResolved struct {
	WorkloadIDs    []string          `json:"workload_ids,omitempty"`
	WorkloadCount  int               `json:"workload_count,omitempty"`
	GroupID        string            `json:"group_id,omitempty"`
	Selector       map[string]string `json:"selector,omitempty"`
	Since          *time.Time        `json:"since,omitempty"`
	Until          *time.Time        `json:"until,omitempty"`
	RequestedScope ReportScope       `json:"requested_scope,omitempty"`
}

// EvidencePack is the canonical JSON response body used for preview and signing.
type EvidencePack struct {
	SchemaVersion string                     `json:"schema_version"`
	GeneratedAt   time.Time                  `json:"generated_at"`
	InputsHash    string                     `json:"inputs_hash"`
	ReportHash    string                     `json:"report_hash"`
	Scope         ReportScopeResolved        `json:"scope"`
	Sections      []EvidenceSection          `json:"sections"`
	Signatures    []ReportSignature          `json:"signatures,omitempty"`
	SignedAudit   *SignedAuditReportMetadata `json:"signed_audit,omitempty"`
	Warnings      []ReportWarning            `json:"warnings,omitempty"`
}

// EvidenceSection is a stable ordered group of evidence items.
type EvidenceSection struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Order    int            `json:"order"`
	Items    []EvidenceItem `json:"items"`
	CSVTable *EvidenceTable `json:"csv_table,omitempty"`
}

// EvidenceItem is one reportable resource/fact in an evidence section.
type EvidenceItem struct {
	ID          string         `json:"id"`
	Resource    string         `json:"resource"`
	ResourceID  string         `json:"resource_id"`
	ObservedAt  *time.Time     `json:"observed_at,omitempty"`
	Severity    string         `json:"severity,omitempty"`
	Summary     string         `json:"summary"`
	Facts       map[string]any `json:"facts"`
	ContentHash string         `json:"content_hash,omitempty"`
	Redacted    bool           `json:"redacted"`
}

// EvidenceTable is the fixed CSV representation attached to sections that render tabular evidence.
type EvidenceTable struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// ReportExportCSVColumns is the deterministic v1 flat CSV header.
var ReportExportCSVColumns = []string{
	"section_id",
	"item_id",
	"resource",
	"resource_id",
	"observed_at",
	"severity",
	"summary",
	"key",
	"value",
	"content_hash",
	"redacted",
}

// EvidencePackCSVColumnsV1 is an explicit alias for the v1 CSV columns.
var EvidencePackCSVColumnsV1 = ReportExportCSVColumns

// ReportSignatureScheme identifies how the final redacted report payload was signed.
type ReportSignatureScheme string

// Report signature schemes supported by the v1 contract.
const (
	ReportSignatureSchemeNone ReportSignatureScheme = "none"

	// ReportSignatureSchemeSHA256HMAC identifies enterprise HMAC-signed reports.
	ReportSignatureSchemeSHA256HMAC ReportSignatureScheme = "sha256-hmac"
	// ReportSignatureSchemeEd25519 identifies enterprise Ed25519-signed reports.
	ReportSignatureSchemeEd25519 ReportSignatureScheme = "ed25519"

	// ReportSignatureVerifierCommunityNone is the verifier label for community unsigned reports.
	ReportSignatureVerifierCommunityNone = "community-none"
)

// ReportSignature records signed report metadata returned with an evidence pack.
type ReportSignature struct {
	Scheme       ReportSignatureScheme `json:"scheme"`
	KeyID        string                `json:"key_id,omitempty"`
	SignedAt     time.Time             `json:"signed_at"`
	PayloadHash  string                `json:"payload_hash"`
	SignatureB64 string                `json:"signature_b64,omitempty"`
	Verifier     string                `json:"verifier,omitempty"`
}

// ReportVerification records the result of verifying a signed report payload.
type ReportVerification struct {
	Valid       bool                  `json:"valid"`
	Scheme      ReportSignatureScheme `json:"scheme"`
	KeyID       string                `json:"key_id,omitempty"`
	PayloadHash string                `json:"payload_hash"`
	Verifier    string                `json:"verifier"`
	CheckedAt   time.Time             `json:"checked_at"`
	Error       string                `json:"error,omitempty"`
}

// SignedAuditReportMetadata summarizes audit-chain verification included in a signed evidence report.
type SignedAuditReportMetadata struct {
	Status           string     `json:"status"`
	Verifier         string     `json:"verifier"`
	VerifiedFrom     *time.Time `json:"verified_from,omitempty"`
	VerifiedUntil    *time.Time `json:"verified_until,omitempty"`
	HeadHash         string     `json:"head_hash,omitempty"`
	CheckedAt        time.Time  `json:"checked_at"`
	FirstBadSequence int64      `json:"first_bad_sequence,omitempty"`
}

// ReportWarning is a deterministic non-fatal report generation warning.
type ReportWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	// ReportExportErrorInvalidJSONBody indicates the request body could not be decoded as JSON.
	ReportExportErrorInvalidJSONBody = "invalid_json_body"
	// ReportExportErrorUnsupportedReportType indicates report_type is not supported by this contract version.
	ReportExportErrorUnsupportedReportType = "unsupported_report_type"
	// ReportExportErrorUnsupportedFormat indicates the requested export format is not supported.
	ReportExportErrorUnsupportedFormat = "unsupported_export_format"
	// ReportExportErrorInvalidScope indicates the report scope is malformed or ambiguous.
	ReportExportErrorInvalidScope = "invalid_scope"
	// ReportExportErrorInvalidTimeRange indicates since/until bounds are invalid.
	ReportExportErrorInvalidTimeRange = "invalid_time_range"
	// ReportExportErrorUnsupportedRedaction indicates the requested redaction mode is unavailable.
	ReportExportErrorUnsupportedRedaction = "unsupported_redaction_mode"
	// ReportExportErrorWorkloadNotFound indicates an explicit workload scope could not be resolved.
	ReportExportErrorWorkloadNotFound = "workload_not_found"
	// ReportExportErrorScopeEmpty indicates the requested scope resolved to no reportable resources.
	ReportExportErrorScopeEmpty = "report_scope_empty"
	// ReportExportErrorBodyTooLarge indicates the JSON request exceeded the configured size limit.
	ReportExportErrorBodyTooLarge = "request_body_too_large"
	// ReportExportErrorBuildFailed indicates the evidence pack could not be built.
	ReportExportErrorBuildFailed = "report_build_failed"
	// ReportExportErrorPDFUnavailable indicates PDF rendering is unavailable and Markdown should be used.
	ReportExportErrorPDFUnavailable = "pdf_unavailable"
	// ReportExportErrorAuditUnavailable indicates audit verification or emission failed before streaming bytes.
	ReportExportErrorAuditUnavailable = "audit_unavailable"
	// ReportExportErrorSigningUnavailable indicates the configured report signer could not sign the payload.
	ReportExportErrorSigningUnavailable = "report_signing_unavailable"
	// ReportExportErrorCodePDFUnavailable is a compatibility alias for PDF fallback errors.
	ReportExportErrorCodePDFUnavailable = ReportExportErrorPDFUnavailable
)

// ReportExportErrorResponse is the JSON body shape used by report export errors.
type ReportExportErrorResponse struct {
	Error          string             `json:"error"`
	Code           string             `json:"code"`
	FallbackFormat ReportExportFormat `json:"fallback_format,omitempty"`
	HTTPStatus     int                `json:"-"`
}

// ReportExportErrorResponses documents the stable report-export error contract.
var ReportExportErrorResponses = map[string]ReportExportErrorResponse{
	ReportExportErrorInvalidJSONBody:       {Error: "invalid JSON body", Code: ReportExportErrorInvalidJSONBody, HTTPStatus: 400},
	ReportExportErrorUnsupportedReportType: {Error: "unsupported report_type", Code: ReportExportErrorUnsupportedReportType, HTTPStatus: 400},
	ReportExportErrorUnsupportedFormat:     {Error: "unsupported export format", Code: ReportExportErrorUnsupportedFormat, HTTPStatus: 400},
	ReportExportErrorInvalidScope:          {Error: "invalid scope", Code: ReportExportErrorInvalidScope, HTTPStatus: 400},
	ReportExportErrorInvalidTimeRange:      {Error: "invalid time range", Code: ReportExportErrorInvalidTimeRange, HTTPStatus: 400},
	ReportExportErrorUnsupportedRedaction:  {Error: "unsupported redaction mode", Code: ReportExportErrorUnsupportedRedaction, HTTPStatus: 400},
	ReportExportErrorWorkloadNotFound:      {Error: "workload not found", Code: ReportExportErrorWorkloadNotFound, HTTPStatus: 404},
	ReportExportErrorScopeEmpty:            {Error: "report scope empty", Code: ReportExportErrorScopeEmpty, HTTPStatus: 409},
	ReportExportErrorBodyTooLarge:          {Error: "request body too large", Code: ReportExportErrorBodyTooLarge, HTTPStatus: 413},
	ReportExportErrorBuildFailed:           {Error: "failed to build evidence pack", Code: ReportExportErrorBuildFailed, HTTPStatus: 500},
	ReportExportErrorPDFUnavailable:        {Error: "pdf export unavailable", Code: ReportExportErrorPDFUnavailable, FallbackFormat: ReportExportMarkdown, HTTPStatus: 501},
	ReportExportErrorAuditUnavailable:      {Error: "audit unavailable", Code: ReportExportErrorAuditUnavailable, HTTPStatus: 503},
	ReportExportErrorSigningUnavailable:    {Error: "report signing unavailable", Code: ReportExportErrorSigningUnavailable, HTTPStatus: 503},
}

// ReportExportContentTypes returns the HTTP content type for each supported export format.
func ReportExportContentTypes() map[ReportExportFormat]string {
	return map[ReportExportFormat]string{
		ReportExportMarkdown: "text/markdown; charset=utf-8",
		ReportExportCSV:      "text/csv; charset=utf-8",
		ReportExportPDF:      "application/pdf",
	}
}
