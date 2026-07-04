package models

// ConfigApplicationPlan is the dry-run contract returned before applying a
// collector configuration. The shape is fleet-capable even when v1 is invoked
// for a single workload.
type ConfigApplicationPlan struct {
	SchemaVersion string                        `json:"schema_version"`
	WorkloadID    string                        `json:"workload_id"`
	ConfigHash    string                        `json:"config_hash"`
	Summary       ConfigApplicationPlanSummary  `json:"summary"`
	RiskScore     ConfigRiskScore               `json:"risk_score"`
	Targets       []ConfigApplicationPlanTarget `json:"targets"`
	Policy        ConfigPolicyEvaluation        `json:"policy"`
	HardFailures  []string                      `json:"hard_failures"`
	CanPush       bool                          `json:"can_push"`
	ApplyAllowed  bool                          `json:"apply_allowed"`
	Export        ConfigApplicationPlanExport   `json:"export"`
}

// ConfigApplicationPlanSummary aggregates fleet-level safety counts for a plan.
type ConfigApplicationPlanSummary struct {
	TargetCount              int `json:"target_count"`
	CollectorTargetCount     int `json:"collector_target_count"`
	RemoteConfigCapableCount int `json:"remote_config_capable_count"`
	ReadOnlyCount            int `json:"read_only_count"`
	ValidationOKCount        int `json:"validation_ok_count"`
	ValidationFailedCount    int `json:"validation_failed_count"`
	ComponentsMissingCount   int `json:"components_missing_count"`
	HighRiskChangeCount      int `json:"high_risk_change_count"`
	ExcludedCount            int `json:"excluded_count"`
}

// ConfigApplicationPlanTarget describes one workload considered by a plan.
type ConfigApplicationPlanTarget struct {
	WorkloadID              string   `json:"workload_id"`
	DisplayName             string   `json:"display_name"`
	Type                    string   `json:"type"`
	AcceptsRemoteConfig     bool     `json:"accepts_remote_config"`
	ReadOnly                bool     `json:"read_only"`
	ValidationStatus        string   `json:"validation_status"`
	ValidationErrors        []string `json:"validation_errors,omitempty"`
	ComponentsMissingCount  int      `json:"components_missing_count"`
	HighRiskChangeCount     int      `json:"high_risk_change_count"`
	Excluded                bool     `json:"excluded"`
	ExclusionReasons        []string `json:"exclusion_reasons"`
	HardFailures            []string `json:"hard_failures"`
	ActiveConfigHash        string   `json:"active_config_hash,omitempty"`
	ActiveConfigUnavailable bool     `json:"active_config_unavailable"`
}

// ConfigApplicationPlanExport advertises deterministic plan export options.
type ConfigApplicationPlanExport struct {
	Supported        bool     `json:"supported"`
	Formats          []string `json:"formats"`
	JSONEndpoint     string   `json:"json_endpoint"`
	MarkdownEndpoint string   `json:"markdown_endpoint"`
	PersistedRollout string   `json:"persisted_rollout"`
}
