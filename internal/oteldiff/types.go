// Package oteldiff computes an OpenTelemetry-aware, redacted semantic diff
// between two Collector YAML configurations.
//
//nolint:revive // This file intentionally exports JSON DTOs for API/frontend contracts.
package oteldiff

import "github.com/magnify-labs/otel-magnify/pkg/models"

const SchemaVersion = "otel-config-diff.v1"
const BlastRadiusSchemaVersion = "otel-config-blast-radius.v1"
const MaskedValue = "••••masked••••"

type Risk string

const (
	RiskNone   Risk = "none"
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

type ChangeKind string

const (
	ChangeAdded     ChangeKind = "added"
	ChangeRemoved   ChangeKind = "removed"
	ChangeModified  ChangeKind = "modified"
	ChangeUnchanged ChangeKind = "unchanged"
)

type ConfigDiff struct {
	SchemaVersion string                 `json:"schema_version"`
	Valid         bool                   `json:"valid"`
	Summary       Summary                `json:"summary"`
	RiskScore     models.ConfigRiskScore `json:"risk_score"`
	BlastRadius   BlastRadius            `json:"blast_radius"`
	HumanSummary  []HumanSummaryItem     `json:"human_summary"`
	Components    []ComponentDiff        `json:"components"`
	Pipelines     []PipelineDiff         `json:"pipelines"`
	Endpoints     []EndpointDiff         `json:"endpoints"`
	Security      []SecurityDiff         `json:"security"`
	RiskItems     []RiskItem             `json:"risk_items"`
	Diagnostics   []Diagnostic           `json:"diagnostics"`
	Normalized    Normalized             `json:"normalized"`
}

type Summary struct {
	OverallRisk Risk   `json:"overall_risk"`
	Counts      Counts `json:"counts"`
	Headline    string `json:"headline"`
}

// HumanSummaryItem is a display-ready, redaction-safe summary line derived from
// the semantic component, pipeline, and field diffs. It intentionally carries
// labels and paths only, never before/after values.
type HumanSummaryItem struct {
	Text        string     `json:"text"`
	Category    string     `json:"category"`
	Kind        ChangeKind `json:"kind"`
	Risk        Risk       `json:"risk"`
	ComponentID string     `json:"component_id,omitempty"`
	PipelineKey string     `json:"pipeline_key,omitempty"`
	Signal      string     `json:"signal,omitempty"`
	Path        string     `json:"path,omitempty"`
}

type Counts struct {
	ComponentsAdded    int `json:"components_added"`
	ComponentsRemoved  int `json:"components_removed"`
	ComponentsModified int `json:"components_modified"`
	PipelinesAdded     int `json:"pipelines_added"`
	PipelinesRemoved   int `json:"pipelines_removed"`
	PipelinesModified  int `json:"pipelines_modified"`
	EndpointsAdded     int `json:"endpoints_added"`
	EndpointsRemoved   int `json:"endpoints_removed"`
	EndpointsModified  int `json:"endpoints_modified"`
	HighRisk           int `json:"high_risk"`
	MediumRisk         int `json:"medium_risk"`
	LowRisk            int `json:"low_risk"`
}

type BlastRadius struct {
	SchemaVersion      string                 `json:"schema_version"`
	AffectedSignals    []string               `json:"affected_signals"`
	TouchedExporters   []string               `json:"touched_exporters"`
	ImpactedServices   []BlastRadiusService   `json:"impacted_services"`
	ImpactedClusters   []string               `json:"impacted_clusters"`
	CriticalCollectors []BlastRadiusCollector `json:"critical_collectors"`
}

type BlastRadiusService struct {
	ServiceName string `json:"service_name"`
	WorkloadID  string `json:"workload_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Type        string `json:"type,omitempty"`
	Status      string `json:"status,omitempty"`
}

type BlastRadiusCollector struct {
	WorkloadID  string   `json:"workload_id"`
	DisplayName string   `json:"display_name,omitempty"`
	Status      string   `json:"status,omitempty"`
	Reasons     []string `json:"reasons"`
}

type BlastRadiusContext struct {
	Workload   BlastRadiusWorkload   `json:"workload,omitempty"`
	FleetPeers []BlastRadiusWorkload `json:"fleet_peers,omitempty"`
}

type BlastRadiusWorkload struct {
	ID              string            `json:"id,omitempty"`
	DisplayName     string            `json:"display_name,omitempty"`
	Type            string            `json:"type,omitempty"`
	Status          string            `json:"status,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	FingerprintKeys map[string]string `json:"fingerprint_keys,omitempty"`
}

type ComponentRef struct {
	Category string `json:"category"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path"`
}

type ComponentDiff struct {
	ID                string        `json:"id"`
	Kind              ChangeKind    `json:"kind"`
	Component         ComponentRef  `json:"component"`
	Risk              Risk          `json:"risk"`
	Title             string        `json:"title"`
	Before            any           `json:"before,omitempty"`
	After             any           `json:"after,omitempty"`
	ChangedFields     []FieldChange `json:"changed_fields"`
	ImpactedPipelines []string      `json:"impacted_pipelines"`
	Rules             []string      `json:"rules"`
}

type PipelineDiff struct {
	ID                  string              `json:"id"`
	Kind                ChangeKind          `json:"kind"`
	PipelineKey         string              `json:"pipeline_key"`
	Signal              string              `json:"signal"`
	Risk                Risk                `json:"risk"`
	Before              *PipelineShape      `json:"before,omitempty"`
	After               *PipelineShape      `json:"after,omitempty"`
	ComponentRefChanges []PipelineRefChange `json:"component_ref_changes"`
	Rules               []string            `json:"rules"`
}

type PipelineShape struct {
	Receivers  []string `json:"receivers"`
	Processors []string `json:"processors"`
	Exporters  []string `json:"exporters"`
}

type PipelineRefChange struct {
	Section     string     `json:"section"`
	ComponentID string     `json:"component_id"`
	Kind        ChangeKind `json:"kind"`
	FromIndex   *int       `json:"from_index,omitempty"`
	ToIndex     *int       `json:"to_index,omitempty"`
	Risk        Risk       `json:"risk"`
	Reason      string     `json:"reason,omitempty"`
}

type EndpointDiff struct {
	ID           string         `json:"id"`
	Kind         ChangeKind     `json:"kind"`
	Component    ComponentRef   `json:"component"`
	EndpointKind string         `json:"endpoint_kind"`
	FieldPath    string         `json:"field_path"`
	Before       *EndpointValue `json:"before,omitempty"`
	After        *EndpointValue `json:"after,omitempty"`
	Risk         Risk           `json:"risk"`
	Rules        []string       `json:"rules"`
}

type EndpointValue struct {
	Raw        string `json:"raw"`
	Scheme     string `json:"scheme,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Path       string `json:"path,omitempty"`
	Normalized string `json:"normalized"`
	Insecure   bool   `json:"insecure,omitempty"`
	TLSEnabled bool   `json:"tls_enabled,omitempty"`
}

type SecurityDiff struct {
	ID        string       `json:"id"`
	Kind      ChangeKind   `json:"kind"`
	Component ComponentRef `json:"component,omitempty"`
	Path      string       `json:"path"`
	Field     string       `json:"field"`
	Before    any          `json:"before,omitempty"`
	After     any          `json:"after,omitempty"`
	Risk      Risk         `json:"risk"`
	Rules     []string     `json:"rules"`
	Message   string       `json:"message"`
}

type RiskItem struct {
	ID                string   `json:"id"`
	Risk              Risk     `json:"risk"`
	Category          string   `json:"category"`
	Rule              string   `json:"rule"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	AffectedPaths     []string `json:"affected_paths"`
	AffectedPipelines []string `json:"affected_pipelines"`
}

type FieldChange struct {
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
	Risk   Risk   `json:"risk"`
}

type Diagnostic struct {
	Side     string `json:"side"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Severity string `json:"severity"`
}

type Normalized struct {
	BaseHash             string `json:"base_hash"`
	TargetHash           string `json:"target_hash"`
	BaseComponentCount   int    `json:"base_component_count"`
	TargetComponentCount int    `json:"target_component_count"`
	BasePipelineCount    int    `json:"base_pipeline_count"`
	TargetPipelineCount  int    `json:"target_pipeline_count"`
}

type graph struct {
	root       map[string]any
	components map[string]map[string]component
	pipelines  map[string]PipelineShape
}

type component struct {
	ref ComponentRef
	cfg any
}

type engine struct {
	diff     ConfigDiff
	base     graph
	target   graph
	seenRisk map[string]bool
}

var componentCategories = []string{"receivers", "processors", "exporters", "connectors", "extensions"}
