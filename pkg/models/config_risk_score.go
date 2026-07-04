package models

// ConfigRiskScore is the concise pre-push risk contract consumed by the UI.
// Severity is normalized to one of none, low, medium, high.
type ConfigRiskScore struct {
	Severity       string   `json:"severity"`
	Reasons        []string `json:"reasons"`
	AppliesToCount int      `json:"applies_to_count"`
}
