package models

import "time"

// EvidenceReportSchemaVersion is the current schema identifier for config-safety evidence exports.
const EvidenceReportSchemaVersion = "config_safety_evidence_report.v1"

// EvidenceReport is the stable, sanitized export payload for config-safety
// reporting. It contains evidence metadata and hashes only; raw config content
// and raw collector/agent error strings must not be serialized here.
type EvidenceReport struct {
	SchemaVersion      string                         `json:"schema_version"`
	ReportID           string                         `json:"report_id"`
	GeneratedAt        time.Time                      `json:"generated_at"`
	RecommendedVersion string                         `json:"recommended_version,omitempty"`
	Summary            EvidenceReportSummary          `json:"summary"`
	ConfigChanges      []EvidenceConfigChange         `json:"config_changes"`
	ValidationFailures []EvidenceValidationFailure    `json:"validation_failures"`
	Rollbacks          []EvidenceRollback             `json:"rollbacks"`
	Drift              ConfigDriftDashboard           `json:"drift"`
	OutdatedCollectors []FleetCollectorVersionFinding `json:"outdated_collectors"`
	AuditTrail         []EvidenceAuditTrailEntry      `json:"audit_trail"`
	Signature          EvidenceReportSignature        `json:"signature"`
}

// EvidenceReportSummary aggregates the evidence report section counts.
type EvidenceReportSummary struct {
	ConfigChanges      int `json:"config_changes"`
	ValidationFailures int `json:"validation_failures"`
	Rollbacks          int `json:"rollbacks"`
	DriftedCollectors  int `json:"drifted_collectors"`
	OutdatedCollectors int `json:"outdated_collectors"`
	AuditEvents        int `json:"audit_events"`
}

// EvidenceConfigChange describes one sanitized workload config revision transition.
type EvidenceConfigChange struct {
	WorkloadID       string    `json:"workload_id"`
	DisplayName      string    `json:"display_name,omitempty"`
	ConfigHash       string    `json:"config_hash"`
	PreviousHash     string    `json:"previous_hash,omitempty"`
	Status           string    `json:"status"`
	PushedBy         string    `json:"pushed_by,omitempty"`
	AppliedAt        time.Time `json:"applied_at"`
	ContentAvailable bool      `json:"content_available"`
	DiffSummary      string    `json:"diff_summary,omitempty"`
}

// EvidenceValidationFailure describes one sanitized config validation or apply failure.
type EvidenceValidationFailure struct {
	WorkloadID  string    `json:"workload_id"`
	DisplayName string    `json:"display_name,omitempty"`
	ConfigHash  string    `json:"config_hash"`
	Status      string    `json:"status"`
	Error       string    `json:"error"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// EvidenceRollback describes one rollback event included in an evidence report.
type EvidenceRollback struct {
	WorkloadID       string    `json:"workload_id"`
	DisplayName      string    `json:"display_name,omitempty"`
	ConfigHash       string    `json:"config_hash"`
	RollbackOfPushID string    `json:"rollback_of_push_id,omitempty"`
	Status           string    `json:"status"`
	OccurredAt       time.Time `json:"occurred_at"`
}

// EvidenceAuditTrailEntry records the audit event associated with report generation.
type EvidenceAuditTrailEntry struct {
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	At         time.Time `json:"at"`
}

// EvidenceReportSignature is the community-safe signing contract. Community
// builds populate an unsigned SHA-256 payload digest; enterprise builds can
// verify the same canonical payload and replace/augment the signature value
// without embedding private key material in the repository.
type EvidenceReportSignature struct {
	Algorithm           string `json:"algorithm"`
	PayloadDigestSHA256 string `json:"payload_digest_sha256"`
	KeyID               string `json:"key_id,omitempty"`
	Signature           string `json:"signature,omitempty"`
	VerificationHint    string `json:"verification_hint"`
}
