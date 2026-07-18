package models

const (
	// PolicyDecisionPass means the config satisfies policy.
	PolicyDecisionPass = "pass"
	// PolicyDecisionWarn means the config is allowed with warnings.
	PolicyDecisionWarn = "warn"
	// PolicyDecisionBlock means the config must not be pushed.
	PolicyDecisionBlock = "block"

	// PolicySeverityInfo is informational policy severity.
	PolicySeverityInfo = "info"
	// PolicySeverityWarning is warning policy severity.
	PolicySeverityWarning = "warning"
	// PolicySeverityCritical is blocking/critical policy severity.
	PolicySeverityCritical = "critical"
)

// ConfigPolicyTarget describes the backend-side scope used to evaluate config
// safety policy. Community rules use environment/scope; edition extensions can add
// tenant/team metadata without changing the community evaluator contract.
type ConfigPolicyTarget struct {
	Environment string `json:"environment,omitempty"`
	Scope       string `json:"scope,omitempty"`
	WorkloadID  string `json:"workload_id,omitempty"`
	TenantID    string `json:"tenant_id,omitempty"`
	TeamID      string `json:"team_id,omitempty"`
}

// ConfigPolicySettings represents server-provided policy settings while
// keeping community defaults immutable when no settings are supplied.
type ConfigPolicySettings struct {
	AllowedOTLPEndpoints       []string               `json:"allowed_otlp_endpoints,omitempty"`
	CriticalExporters          []string               `json:"critical_exporters,omitempty"`
	RequiredResourceAttributes []string               `json:"required_resource_attributes,omitempty"`
	Sampling                   SamplingPolicySettings `json:"sampling,omitempty"`
}

// SamplingPolicySettings bounds sampling percentages when sampling policy is enabled.
type SamplingPolicySettings struct {
	MinPercentage float64 `json:"min_percentage,omitempty"`
	MaxPercentage float64 `json:"max_percentage,omitempty"`
}

// ConfigPolicyFinding is one deterministic policy finding suitable for API
// clients, audit details, and future Enterprise tenant/team hooks.
type ConfigPolicyFinding struct {
	PolicyID    string   `json:"policy_id"`
	PolicyName  string   `json:"policy_name"`
	RuleID      string   `json:"rule_id"`
	RuleCode    string   `json:"rule_code"`
	Severity    string   `json:"severity"`
	Decision    string   `json:"decision"`
	TargetScope string   `json:"target_scope,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Path        string   `json:"path"`
	Paths       []string `json:"paths,omitempty"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation"`
	Packaging   string   `json:"packaging"`
	Tier        string   `json:"tier"`
}

// ConfigPolicyEvaluation is the API contract returned by preview/plan/push
// paths. It intentionally includes deterministic metadata rather than raw audit
// storage ids so Community can run without an audit sink.
type ConfigPolicyEvaluation struct {
	SchemaVersion string                `json:"schema_version"`
	Valid         bool                  `json:"valid"`
	Allowed       bool                  `json:"allowed"`
	Decision      string                `json:"decision"`
	Severity      string                `json:"severity"`
	Target        ConfigPolicyTarget    `json:"target"`
	Settings      ConfigPolicySettings  `json:"settings,omitempty"`
	Findings      []ConfigPolicyFinding `json:"findings"`
	Summary       ConfigPolicySummary   `json:"summary"`
	Audit         ConfigPolicyAuditMeta `json:"audit"`
}

// ConfigPolicySummary counts policy findings by decision.
type ConfigPolicySummary struct {
	PassCount  int `json:"pass_count"`
	WarnCount  int `json:"warn_count"`
	BlockCount int `json:"block_count"`
}

// ConfigPolicyAuditMeta describes whether a policy evaluation was persisted to audit storage.
type ConfigPolicyAuditMeta struct {
	Persisted bool   `json:"persisted"`
	Event     string `json:"event,omitempty"`
	Reason    string `json:"reason,omitempty"`
}
