// Package oteldiff computes an OpenTelemetry-aware, redacted semantic diff
// between two Collector YAML configurations.
//
//nolint:revive // This file intentionally exports JSON DTOs for API/frontend contracts.
package oteldiff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"

	"gopkg.in/yaml.v3"
)

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

func Compare(baseYAML, targetYAML []byte) ConfigDiff {
	return CompareWithContext(baseYAML, targetYAML, BlastRadiusContext{})
}

func CompareWithContext(baseYAML, targetYAML []byte, ctx BlastRadiusContext) ConfigDiff {
	d := emptyDiff(baseYAML, targetYAML)
	base, baseDiags := parseConfig("base", baseYAML)
	target, targetDiags := parseConfig("target", targetYAML)
	d.Diagnostics = append(d.Diagnostics, baseDiags...)
	d.Diagnostics = append(d.Diagnostics, targetDiags...)
	if len(baseDiags) > 0 || len(targetDiags) > 0 {
		d.Valid = false
		d.Summary = Summary{OverallRisk: RiskNone, Counts: Counts{}, Headline: "OTel impact summary unavailable: fix YAML parse errors"}
		return d
	}
	d.Valid = true
	d.Normalized.BaseComponentCount = countComponents(base.components)
	d.Normalized.TargetComponentCount = countComponents(target.components)
	d.Normalized.BasePipelineCount = len(base.pipelines)
	d.Normalized.TargetPipelineCount = len(target.pipelines)
	e := &engine{diff: d, base: base, target: target, seenRisk: map[string]bool{}}
	e.compareComponents()
	e.comparePipelines()
	e.compareEndpoints()
	e.compareSecurity()
	e.finish()
	e.diff.BlastRadius = buildBlastRadius(e.diff, ctx)
	return e.diff
}

func emptyDiff(baseYAML, targetYAML []byte) ConfigDiff {
	return ConfigDiff{
		SchemaVersion: SchemaVersion,
		Valid:         false,
		Summary:       Summary{OverallRisk: RiskNone, Counts: Counts{}, Headline: "No OTel semantic changes detected"},
		RiskScore:     models.ConfigRiskScore{Severity: string(RiskNone), Reasons: []string{}},
		BlastRadius:   emptyBlastRadius(),
		Components:    []ComponentDiff{}, Pipelines: []PipelineDiff{}, Endpoints: []EndpointDiff{}, Security: []SecurityDiff{}, RiskItems: []RiskItem{}, Diagnostics: []Diagnostic{},
		Normalized: Normalized{BaseHash: hashYAML(baseYAML), TargetHash: hashYAML(targetYAML)},
	}
}

func emptyBlastRadius() BlastRadius {
	return BlastRadius{SchemaVersion: BlastRadiusSchemaVersion, AffectedSignals: []string{}, TouchedExporters: []string{}, ImpactedServices: []BlastRadiusService{}, ImpactedClusters: []string{}, CriticalCollectors: []BlastRadiusCollector{}}
}

func hashYAML(b []byte) string { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }

func parseConfig(side string, b []byte) (graph, []Diagnostic) {
	var root map[string]any
	if err := yaml.Unmarshal(b, &root); err != nil {
		return graph{}, []Diagnostic{{Side: side, Code: "yaml_parse", Message: "invalid YAML: parse failed", Severity: "error"}}
	}
	if root == nil {
		root = map[string]any{}
	}
	g := graph{root: root, components: map[string]map[string]component{}, pipelines: map[string]PipelineShape{}}
	var diags []Diagnostic
	for _, cat := range componentCategories {
		g.components[cat] = map[string]component{}
		v, ok := root[cat]
		if !ok {
			continue
		}
		m, ok := asMap(v)
		if !ok {
			diags = append(diags, Diagnostic{Side: side, Code: "invalid_section", Path: cat, Message: fmt.Sprintf("%s section is not an object", cat), Severity: "warning"})
			continue
		}
		for _, id := range sortedKeys(m) {
			g.components[cat][id] = component{ref: componentRef(cat, id), cfg: normalizeValue(m[id])}
		}
	}
	if svc, ok := asMap(root["service"]); ok {
		if pmap, ok := asMap(svc["pipelines"]); ok {
			for _, name := range sortedKeys(pmap) {
				if pm, ok := asMap(pmap[name]); ok {
					g.pipelines[name] = PipelineShape{Receivers: toStringSlice(pm["receivers"]), Processors: toStringSlice(pm["processors"]), Exporters: toStringSlice(pm["exporters"])}
				}
			}
		}
	}
	return g, diags
}

func (e *engine) compareComponents() {
	for _, cat := range componentCategories {
		ids := unionComponentIDs(e.base.components[cat], e.target.components[cat])
		for _, id := range ids {
			b, bok := e.base.components[cat][id]
			t, tok := e.target.components[cat][id]
			cd := ComponentDiff{ID: cat + ":" + id, Component: componentRef(cat, id), ChangedFields: []FieldChange{}, ImpactedPipelines: impactedPipelines(e.base.pipelines, e.target.pipelines, cat, id), Rules: []string{}}
			switch {
			case !bok && tok:
				cd.Kind = ChangeAdded
				cd.After = redactValue(t.cfg, cat+"."+id)
				cd.Risk = RiskLow
				cd.Title = singular(cat) + " " + id + " added"
			case bok && !tok:
				cd.Kind = ChangeRemoved
				cd.Before = redactValue(b.cfg, cat+"."+id)
				cd.Risk = removedComponentRisk(cat, id)
				cd.Title = singular(cat) + " " + id + " removed"
				cd.Rules = removedComponentRules(cat, id, cd.ImpactedPipelines)
			default:
				if canonical(b.cfg) == canonical(t.cfg) {
					continue
				}
				cd.Kind = ChangeModified
				cd.Before = redactValue(b.cfg, cat+"."+id)
				cd.After = redactValue(t.cfg, cat+"."+id)
				cd.ChangedFields = changedFields(cat+"."+id, b.cfg, t.cfg)
				cd.Risk = componentModifiedRisk(cat, id, cd.ChangedFields)
				cd.Title = singular(cat) + " " + id + " modified"
				cd.Rules = componentModifiedRules(cat, id, cd.ChangedFields)
			}
			for _, rule := range cd.Rules {
				e.addRisk(rule+":"+cd.Component.Path, cd.Risk, riskCategory(rule), rule, titleForRule(rule), rule, []string{cd.Component.Path}, cd.ImpactedPipelines)
			}
			e.diff.Components = append(e.diff.Components, cd)
		}
	}
}

func (e *engine) comparePipelines() {
	keys := unionPipelineKeys(e.base.pipelines, e.target.pipelines)
	for _, key := range keys {
		b, bok := e.base.pipelines[key]
		t, tok := e.target.pipelines[key]
		pd := PipelineDiff{ID: "pipeline:" + key, PipelineKey: key, Signal: signalOf(key), Risk: RiskLow, ComponentRefChanges: []PipelineRefChange{}, Rules: []string{}}
		switch {
		case !bok && tok:
			pd.Kind = ChangeAdded
			pd.After = &t
			pd.Risk = RiskLow
		case bok && !tok:
			pd.Kind = ChangeRemoved
			pd.Before = &b
			pd.Risk = RiskHigh
			pd.Rules = appendRule(pd.Rules, "pipeline_removed")
			e.addRisk("pipeline_removed:"+key, RiskHigh, "data_loss", "pipeline_removed", "Pipeline "+key+" removed", "Telemetry pipeline removed", []string{"service.pipelines." + key}, []string{key})
		default:
			changes := pipelineRefChanges(b, t)
			if len(changes) == 0 {
				continue
			}
			pd.Kind = ChangeModified
			pd.Before = &b
			pd.After = &t
			pd.ComponentRefChanges = changes
			for _, ch := range changes {
				pd.Risk = maxRisk(pd.Risk, ch.Risk)
				if ch.Reason != "" {
					pd.Rules = appendRule(pd.Rules, ch.Reason)
				}
			}
			if len(t.Exporters) == 0 {
				pd.Risk = RiskHigh
				pd.Rules = appendRule(pd.Rules, "pipeline_broken")
				e.addRisk("pipeline_broken:"+key+":exporters", RiskHigh, "data_loss", "pipeline_broken", "Pipeline "+key+" has no exporters", "Telemetry would not be exported", []string{"service.pipelines." + key + ".exporters"}, []string{key})
			}
			if len(t.Receivers) == 0 {
				pd.Risk = RiskHigh
				pd.Rules = appendRule(pd.Rules, "pipeline_broken")
				e.addRisk("pipeline_broken:"+key+":receivers", RiskHigh, "data_loss", "pipeline_broken", "Pipeline "+key+" has no receivers", "Telemetry would not be received", []string{"service.pipelines." + key + ".receivers"}, []string{key})
			}
		}
		for _, ch := range pd.ComponentRefChanges {
			if ch.Reason != "" {
				e.addRisk(ch.Reason+":"+key+":"+ch.ComponentID, ch.Risk, riskCategory(ch.Reason), ch.Reason, titleForRule(ch.Reason), ch.Reason, []string{"service.pipelines." + key + "." + ch.Section}, []string{key})
			}
		}
		e.diff.Pipelines = append(e.diff.Pipelines, pd)
	}
}

func (e *engine) compareEndpoints() {
	base := extractEndpoints(e.base)
	target := extractEndpoints(e.target)
	keys := unionEndpointKeys(base, target)
	for _, k := range keys {
		b, bok := base[k]
		t, tok := target[k]
		ed := EndpointDiff{ID: "endpoint:" + k, FieldPath: k, Rules: []string{}}
		if bok {
			ed.Component = b.component
		} else {
			ed.Component = t.component
		}
		ed.EndpointKind = endpointKind(ed.Component)
		switch {
		case !bok && tok:
			ed.Kind = ChangeAdded
			ed.After = &t.value
			ed.Risk = RiskLow
		case bok && !tok:
			ed.Kind = ChangeRemoved
			ed.Before = &b.value
			ed.Risk = RiskMedium
		default:
			if b.value.Normalized == t.value.Normalized {
				continue
			}
			ed.Kind = ChangeModified
			ed.Before = &b.value
			ed.After = &t.value
			ed.Risk = endpointRisk(ed.Component, b.value, t.value)
			ed.Rules = endpointRules(ed.Component, b.value, t.value)
		}
		for _, r := range ed.Rules {
			e.addRisk(r+":"+k, ed.Risk, riskCategory(r), r, titleForRule(r), r, []string{k}, impactedPipelines(e.base.pipelines, e.target.pipelines, ed.Component.Category, ed.Component.ID))
		}
		e.diff.Endpoints = append(e.diff.Endpoints, ed)
	}
}

func (e *engine) compareSecurity() {
	for _, cd := range e.diff.Components {
		if cd.Kind == ChangeUnchanged {
			continue
		}
		for _, fc := range cd.ChangedFields {
			lower := strings.ToLower(fc.Path)
			if isAuthPath(lower) || strings.Contains(lower, "headers") {
				sd := SecurityDiff{ID: "security:" + fc.Path, Kind: cd.Kind, Component: cd.Component, Path: fc.Path, Field: "headers", Before: fc.Before, After: fc.After, Risk: RiskHigh, Rules: []string{"auth_header_modified"}, Message: "Authentication/header value changed; values redacted"}
				e.diff.Security = append(e.diff.Security, sd)
				e.addRisk("auth_header_modified:"+fc.Path, RiskHigh, "security", "auth_header_modified", "Auth/header modified", "Authentication headers or secret-like auth fields changed", []string{fc.Path}, cd.ImpactedPipelines)
			}
			if isInsecurePath(lower) && boolBecameTrue(fc.Before, fc.After) {
				sd := SecurityDiff{ID: "security:" + fc.Path, Kind: ChangeModified, Component: cd.Component, Path: fc.Path, Field: "insecure", Before: fc.Before, After: fc.After, Risk: RiskHigh, Rules: []string{"transport_security_weakened"}, Message: "Transport security weakened"}
				e.diff.Security = append(e.diff.Security, sd)
				e.addRisk("transport_security_weakened:"+fc.Path, RiskHigh, "security", "transport_security_weakened", "Transport security weakened", "TLS/insecure setting was weakened", []string{fc.Path}, cd.ImpactedPipelines)
			}
		}
		if cd.Kind == ChangeRemoved && cd.Component.Category == "exporters" {
			e.addRisk("exporter_removed:"+cd.Component.ID, RiskHigh, "data_loss", "exporter_removed", "Exporter "+cd.Component.ID+" removed", "Telemetry destination removed", []string{cd.Component.Path}, cd.ImpactedPipelines)
		}
	}
}

func (e *engine) finish() {
	sort.Slice(e.diff.Components, func(i, j int) bool { return e.diff.Components[i].ID < e.diff.Components[j].ID })
	sort.Slice(e.diff.Pipelines, func(i, j int) bool { return e.diff.Pipelines[i].PipelineKey < e.diff.Pipelines[j].PipelineKey })
	sort.Slice(e.diff.Endpoints, func(i, j int) bool { return e.diff.Endpoints[i].FieldPath < e.diff.Endpoints[j].FieldPath })
	sort.Slice(e.diff.Security, func(i, j int) bool { return e.diff.Security[i].Path < e.diff.Security[j].Path })
	sort.Slice(e.diff.RiskItems, func(i, j int) bool {
		if e.diff.RiskItems[i].Risk != e.diff.RiskItems[j].Risk {
			return riskRank(e.diff.RiskItems[i].Risk) > riskRank(e.diff.RiskItems[j].Risk)
		}
		return e.diff.RiskItems[i].ID < e.diff.RiskItems[j].ID
	})
	for _, c := range e.diff.Components {
		switch c.Kind {
		case ChangeAdded:
			e.diff.Summary.Counts.ComponentsAdded++
		case ChangeRemoved:
			e.diff.Summary.Counts.ComponentsRemoved++
		case ChangeModified:
			e.diff.Summary.Counts.ComponentsModified++
		case ChangeUnchanged:
			// No summary counter for unchanged components.
		}
		e.countRisk(c.Risk)
	}
	for _, p := range e.diff.Pipelines {
		switch p.Kind {
		case ChangeAdded:
			e.diff.Summary.Counts.PipelinesAdded++
		case ChangeRemoved:
			e.diff.Summary.Counts.PipelinesRemoved++
		case ChangeModified:
			e.diff.Summary.Counts.PipelinesModified++
		case ChangeUnchanged:
			// No summary counter for unchanged pipelines.
		}
		e.countRisk(p.Risk)
	}
	for _, ep := range e.diff.Endpoints {
		switch ep.Kind {
		case ChangeAdded:
			e.diff.Summary.Counts.EndpointsAdded++
		case ChangeRemoved:
			e.diff.Summary.Counts.EndpointsRemoved++
		case ChangeModified:
			e.diff.Summary.Counts.EndpointsModified++
		case ChangeUnchanged:
			// No summary counter for unchanged endpoints.
		}
		e.countRisk(ep.Risk)
	}
	risk := RiskNone
	for _, item := range e.diff.RiskItems {
		risk = maxRisk(risk, item.Risk)
	}
	for _, c := range e.diff.Components {
		risk = maxRisk(risk, c.Risk)
	}
	for _, p := range e.diff.Pipelines {
		risk = maxRisk(risk, p.Risk)
	}
	for _, ep := range e.diff.Endpoints {
		risk = maxRisk(risk, ep.Risk)
	}
	e.diff.Summary.OverallRisk = risk
	switch risk {
	case RiskHigh:
		e.diff.Summary.Headline = "High risk: dangerous OTel Collector changes detected"
	case RiskMedium:
		e.diff.Summary.Headline = "Medium risk: review routing, endpoints, sampling, or auth changes"
	case RiskLow:
		e.diff.Summary.Headline = "Low risk: no telemetry path or destination appears to be removed"
	case RiskNone:
		e.diff.Summary.Headline = "No OTel semantic changes detected"
	}
	e.diff.RiskScore = BuildRiskScore(e.diff, 0)
}

// BuildRiskScore reduces the full semantic diff to the stable, concise pre-push
// risk contract used by config diff and config plan APIs.
func BuildRiskScore(diff ConfigDiff, appliesToCount int) models.ConfigRiskScore {
	reasons := make([]string, 0, len(diff.RiskItems))
	seen := map[string]bool{}
	for _, item := range diff.RiskItems {
		reason := riskReason(item)
		if reason == "" || seen[reason] {
			continue
		}
		seen[reason] = true
		reasons = append(reasons, reason)
	}
	return models.ConfigRiskScore{Severity: string(diff.Summary.OverallRisk), Reasons: reasons, AppliesToCount: appliesToCount}
}

func riskReason(item RiskItem) string {
	switch item.Rule {
	case "pipeline_removed":
		if len(item.AffectedPipelines) > 0 {
			return "Pipeline " + item.AffectedPipelines[0] + " removed"
		}
		return "Pipeline removed"
	case "otlp_endpoint_changed":
		return "OTLP endpoint changed"
	case "memory_limiter_removed_from_pipeline":
		return "Memory limiter removed from pipeline"
	case "batch_processor_removed_from_pipeline":
		return "Batch processor removed from pipeline"
	case "pipeline_broken":
		return "Pipeline broken"
	case "transport_security_weakened":
		return "Transport security weakened"
	case "auth_header_modified":
		return "Auth/header modified"
	case "exporter_removed":
		return "Exporter removed"
	case "sampling_guard_removed":
		return "Sampling guard removed"
	case "sampling_rate_strongly_changed":
		return "Sampling rate strongly changed"
	case "receiver_endpoint_exposure_changed":
		return "Receiver endpoint exposure changed"
	case "pipeline_exporter_set_changed":
		return "Pipeline exporter set changed"
	default:
		return strings.TrimSpace(item.Title)
	}
}

func (e *engine) countRisk(r Risk) {
	switch r {
	case RiskHigh:
		e.diff.Summary.Counts.HighRisk++
	case RiskMedium:
		e.diff.Summary.Counts.MediumRisk++
	case RiskLow:
		e.diff.Summary.Counts.LowRisk++
	case RiskNone:
		// No counter for absent risk.
	}
}
func (e *engine) addRisk(id string, risk Risk, cat, rule, title, desc string, paths, pipelines []string) {
	if e.seenRisk[id] {
		return
	}
	e.seenRisk[id] = true
	e.diff.RiskItems = append(e.diff.RiskItems, RiskItem{ID: id, Risk: risk, Category: cat, Rule: rule, Title: title, Description: desc, AffectedPaths: dedupeSorted(paths), AffectedPipelines: dedupeSorted(pipelines)})
}

func buildBlastRadius(diff ConfigDiff, ctx BlastRadiusContext) BlastRadius {
	out := emptyBlastRadius()
	signals := map[string]bool{}
	exporters := map[string]bool{}
	for _, p := range diff.Pipelines {
		if p.Signal != "" && p.Signal != "unknown" {
			signals[p.Signal] = true
		}
		for _, ch := range p.ComponentRefChanges {
			if ch.Section == "exporters" && ch.ComponentID != "" {
				exporters[ch.ComponentID] = true
			}
		}
	}
	for _, c := range diff.Components {
		for _, p := range c.ImpactedPipelines {
			if sig := signalOf(p); sig != "unknown" {
				signals[sig] = true
			}
		}
		if c.Component.Category == "exporters" {
			exporters[c.Component.ID] = true
		}
	}
	for _, ep := range diff.Endpoints {
		if ep.Component.Category == "exporters" {
			exporters[ep.Component.ID] = true
		}
	}
	for _, item := range diff.RiskItems {
		for _, p := range item.AffectedPipelines {
			if sig := signalOf(p); sig != "unknown" {
				signals[sig] = true
			}
		}
	}
	out.AffectedSignals = sortedSet(signals)
	out.TouchedExporters = sortedSet(exporters)

	workloads := []BlastRadiusWorkload{}
	if ctx.Workload.ID != "" || ctx.Workload.DisplayName != "" || len(ctx.Workload.Labels) > 0 || len(ctx.Workload.FingerprintKeys) > 0 {
		workloads = append(workloads, ctx.Workload)
	}
	workloads = append(workloads, ctx.FleetPeers...)
	clusterSet := map[string]bool{}
	for _, wl := range workloads {
		for _, key := range clusterKeys() {
			if v := workloadValue(wl, key); v != "" {
				clusterSet[v] = true
			}
		}
	}
	out.ImpactedClusters = sortedSet(clusterSet)

	serviceSeen := map[string]bool{}
	for _, wl := range workloads {
		if !workloadInClusters(wl, clusterSet) {
			continue
		}
		svc := firstWorkloadValue(wl, serviceKeys())
		if svc == "" {
			continue
		}
		key := wl.ID + "\x00" + svc
		if serviceSeen[key] {
			continue
		}
		serviceSeen[key] = true
		out.ImpactedServices = append(out.ImpactedServices, BlastRadiusService{ServiceName: svc, WorkloadID: wl.ID, DisplayName: wl.DisplayName, Type: wl.Type, Status: wl.Status})
	}
	sort.Slice(out.ImpactedServices, func(i, j int) bool { return out.ImpactedServices[i].ServiceName < out.ImpactedServices[j].ServiceName })

	for _, wl := range workloads {
		if !workloadInClusters(wl, clusterSet) || !isCollectorWorkload(wl) {
			continue
		}
		reasons := criticalCollectorReasons(wl)
		if len(reasons) == 0 {
			continue
		}
		out.CriticalCollectors = append(out.CriticalCollectors, BlastRadiusCollector{WorkloadID: wl.ID, DisplayName: wl.DisplayName, Status: wl.Status, Reasons: reasons})
	}
	sort.Slice(out.CriticalCollectors, func(i, j int) bool {
		return out.CriticalCollectors[i].WorkloadID < out.CriticalCollectors[j].WorkloadID
	})
	return out
}

func sortedSet(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func serviceKeys() []string { return []string{"service.name", "service", "app.kubernetes.io/name"} }

func clusterKeys() []string {
	return []string{"k8s.cluster.name", "cluster", "k8s.namespace.name", "deployment.environment"}
}

func firstWorkloadValue(wl BlastRadiusWorkload, keys []string) string {
	for _, key := range keys {
		if v := workloadValue(wl, key); v != "" {
			return v
		}
	}
	return ""
}

func workloadValue(wl BlastRadiusWorkload, key string) string {
	if v := strings.TrimSpace(wl.Labels[key]); v != "" && !looksSecret(v) && !isSecretKey(key) {
		return v
	}
	if v := strings.TrimSpace(wl.FingerprintKeys[key]); v != "" && !looksSecret(v) && !isSecretKey(key) {
		return v
	}
	return ""
}

func workloadInClusters(wl BlastRadiusWorkload, clusters map[string]bool) bool {
	if len(clusters) == 0 {
		return true
	}
	for _, key := range clusterKeys() {
		if clusters[workloadValue(wl, key)] {
			return true
		}
	}
	return false
}

func isCollectorWorkload(wl BlastRadiusWorkload) bool {
	return strings.EqualFold(wl.Type, "collector") || strings.Contains(strings.ToLower(wl.DisplayName), "collector")
}

func criticalCollectorReasons(wl BlastRadiusWorkload) []string {
	reasons := []string{}
	if isCollectorWorkload(wl) {
		reasons = append(reasons, "collector_workload")
	}
	status := strings.ToLower(strings.TrimSpace(wl.Status))
	if status == "degraded" || status == "disconnected" {
		reasons = append(reasons, "status_"+status)
	}
	for k, v := range wl.Labels {
		lk, lv := strings.ToLower(k), strings.ToLower(strings.TrimSpace(v))
		if (lk == "critical" && lv == "true") || (lk == "tier" && lv == "critical") {
			reasons = appendRule(reasons, lk+"_"+lv)
		}
		if lv == "prod" || lv == "production" {
			reasons = appendRule(reasons, "production_label")
		}
	}
	for _, key := range clusterKeys() {
		v := strings.ToLower(workloadValue(wl, key))
		if v == "prod" || v == "production" || strings.HasPrefix(v, "prod-") {
			reasons = appendRule(reasons, "production_label")
		}
	}
	return dedupeSorted(reasons)
}

func componentRef(cat, id string) ComponentRef {
	typ, name := componentTypeName(id)
	return ComponentRef{Category: cat, ID: id, Type: typ, Name: name, Path: cat + "." + id}
}
func componentTypeName(id string) (string, string) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return id, ""
}
func singular(cat string) string { return strings.TrimSuffix(cat, "s") }
func signalOf(key string) string {
	sig := strings.SplitN(key, "/", 2)[0]
	switch sig {
	case "traces", "metrics", "logs", "profiles":
		return sig
	default:
		return "unknown"
	}
}
func removedComponentRisk(cat, id string) Risk {
	typ, _ := componentTypeName(id)
	if cat == "exporters" || (cat == "processors" && (typ == "batch" || typ == "memory_limiter" || isSamplingID(id))) {
		return RiskHigh
	}
	return RiskMedium
}
func removedComponentRules(cat, id string, pipes []string) []string {
	typ, _ := componentTypeName(id)
	var rules []string
	if cat == "exporters" {
		rules = append(rules, "exporter_removed")
	}
	if cat == "processors" && typ == "batch" {
		rules = append(rules, "batch_processor_removed_from_pipeline")
	}
	if cat == "processors" && typ == "memory_limiter" {
		rules = append(rules, "memory_limiter_removed_from_pipeline")
	}
	if cat == "processors" && isSamplingID(id) {
		rules = append(rules, "sampling_guard_removed")
	}
	return rules
}
func componentModifiedRisk(cat, id string, fields []FieldChange) Risk {
	r := RiskLow
	for _, f := range fields {
		l := strings.ToLower(f.Path)
		if isAuthPath(l) || strings.Contains(l, "headers") || isInsecurePath(l) && boolBecameTrue(f.Before, f.After) {
			r = maxRisk(r, RiskHigh)
		}
		if isSamplingID(id) && strings.Contains(l, "sampling_percentage") {
			if strongSamplingChange(f.Before, f.After) {
				r = maxRisk(r, RiskHigh)
			} else {
				r = maxRisk(r, RiskMedium)
			}
		}
	}
	return r
}
func componentModifiedRules(cat, id string, fields []FieldChange) []string {
	var rules []string
	for _, f := range fields {
		l := strings.ToLower(f.Path)
		if isAuthPath(l) || strings.Contains(l, "headers") {
			rules = appendRule(rules, "auth_header_modified")
		}
		if isInsecurePath(l) && boolBecameTrue(f.Before, f.After) {
			rules = appendRule(rules, "transport_security_weakened")
		}
		if isSamplingID(id) && strings.Contains(l, "sampling_percentage") {
			if strongSamplingChange(f.Before, f.After) {
				rules = appendRule(rules, "sampling_rate_strongly_changed")
			} else {
				rules = appendRule(rules, "sampling_policy_modified")
			}
		}
	}
	return rules
}

func pipelineRefChanges(b, t PipelineShape) []PipelineRefChange {
	var out []PipelineRefChange
	out = append(out, refChanges("receivers", b.Receivers, t.Receivers)...)
	out = append(out, refChanges("processors", b.Processors, t.Processors)...)
	out = append(out, refChanges("exporters", b.Exporters, t.Exporters)...)
	return out
}
func refChanges(section string, b, t []string) []PipelineRefChange {
	var out []PipelineRefChange
	bm, tm := indexMap(b), indexMap(t)
	ids := unionStringKeys(bm, tm)
	for _, id := range ids {
		bi, bok := bm[id]
		ti, tok := tm[id]
		switch {
		case bok && !tok:
			r, reason := RiskMedium, ""
			typ, _ := componentTypeName(id)
			if section == "processors" && typ == "batch" {
				r = RiskHigh
				reason = "batch_processor_removed_from_pipeline"
			}
			if section == "processors" && typ == "memory_limiter" {
				r = RiskHigh
				reason = "memory_limiter_removed_from_pipeline"
			}
			if section == "processors" && isSamplingID(id) {
				r = RiskHigh
				reason = "sampling_guard_removed"
			}
			if section == "exporters" {
				if len(t) == 0 {
					r = RiskHigh
					reason = "pipeline_broken"
				} else {
					r = RiskMedium
					reason = "pipeline_exporter_set_changed"
				}
			}
			out = append(out, PipelineRefChange{Section: section, ComponentID: id, Kind: ChangeRemoved, FromIndex: &bi, Risk: r, Reason: reason})
		case !bok && tok:
			r := RiskLow
			if section == "receivers" {
				r = RiskMedium
			}
			out = append(out, PipelineRefChange{Section: section, ComponentID: id, Kind: ChangeAdded, ToIndex: &ti, Risk: r})
		}
	}
	return out
}

func extractEndpoints(g graph) map[string]struct {
	component ComponentRef
	value     EndpointValue
} {
	out := map[string]struct {
		component ComponentRef
		value     EndpointValue
	}{}
	for _, cat := range componentCategories {
		for _, id := range sortedCompKeys(g.components[cat]) {
			c := g.components[cat][id]
			walk(c.cfg, c.ref.Path, func(path string, v any) {
				key := lastPathPart(path)
				if isEndpointKey(key) {
					if s, ok := scalarString(v); ok {
						out[path] = struct {
							component ComponentRef
							value     EndpointValue
						}{c.ref, parseEndpoint(s, c.ref)}
					}
				}
			})
		}
	}
	return out
}
func isEndpointKey(k string) bool {
	switch strings.ToLower(k) {
	case "endpoint", "url", "server", "address", "listen_address":
		return true
	default:
		return false
	}
}
func endpointKind(ref ComponentRef) string {
	if strings.HasPrefix(ref.Type, "otlphttp") {
		return "otlp_http"
	}
	if strings.HasPrefix(ref.Type, "otlp") {
		return "otlp_grpc"
	}
	return "generic"
}
func endpointRisk(ref ComponentRef, b, t EndpointValue) Risk {
	if strings.HasPrefix(ref.Type, "otlp") && ref.Category == "exporters" && (b.Host != t.Host || b.Port != t.Port || b.Scheme != t.Scheme) {
		return RiskHigh
	}
	if ref.Category == "receivers" {
		return RiskMedium
	}
	if b.Scheme == "https" && t.Scheme == "http" {
		return RiskHigh
	}
	return RiskMedium
}
func endpointRules(ref ComponentRef, b, t EndpointValue) []string {
	var r []string
	if strings.HasPrefix(ref.Type, "otlp") && ref.Category == "exporters" && (b.Host != t.Host || b.Port != t.Port || b.Scheme != t.Scheme) {
		r = appendRule(r, "otlp_endpoint_changed")
	}
	if ref.Category == "receivers" {
		r = appendRule(r, "receiver_endpoint_exposure_changed")
	}
	if b.Scheme == "https" && t.Scheme == "http" {
		r = appendRule(r, "transport_security_weakened")
	}
	return r
}
func parseEndpoint(raw string, ref ComponentRef) EndpointValue {
	sanitized := sanitizeEndpoint(raw)
	e := EndpointValue{Raw: sanitized, Normalized: sanitized}
	parse := sanitized
	if !strings.Contains(parse, "://") {
		if strings.HasPrefix(ref.Type, "otlphttp") {
			parse = "http://" + parse
		} else {
			parse = "grpc://" + parse
		}
	}
	if u, err := url.Parse(parse); err == nil {
		e.Scheme = u.Scheme
		e.Host = strings.ToLower(u.Hostname())
		if p := u.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				e.Port = n
			}
		}
		e.Path = u.EscapedPath()
		e.Normalized = normalizeURL(u)
		e.Insecure = (u.Scheme == "http" || u.Scheme == "grpc")
		e.TLSEnabled = (u.Scheme == "https")
	}
	return e
}
func sanitizeEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	parse := raw
	added := false
	if !strings.Contains(parse, "://") {
		parse = "https://" + parse
		added = true
	}
	u, err := url.Parse(parse)
	if err != nil {
		return redactSecretLikeString(raw)
	}
	u.User = nil
	q := u.Query()
	for k := range q {
		if isSecretKey(k) {
			q.Set(k, MaskedValue)
		}
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	s := u.String()
	if added {
		s = strings.TrimPrefix(s, "https://")
	}
	return s
}
func normalizeURL(u *url.URL) string {
	cp := *u
	cp.Host = strings.ToLower(cp.Host)
	cp.User = nil
	cp.Fragment = ""
	return strings.TrimRight(cp.String(), "/")
}

func changedFields(prefix string, b, t any) []FieldChange {
	var out []FieldChange
	walkDiff(prefix, b, t, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func walkDiff(path string, b, t any, out *[]FieldChange) {
	bm, bok := asMap(b)
	tm, tok := asMap(t)
	if bok && tok {
		keys := unionStringKeysFromMaps(bm, tm)
		for _, k := range keys {
			walkDiff(path+"."+k, bm[k], tm[k], out)
		}
		return
	}
	if !reflect.DeepEqual(b, t) {
		*out = append(*out, FieldChange{Path: path, Before: redactValue(b, path), After: redactValue(t, path), Risk: fieldRisk(path, b, t)})
	}
}
func fieldRisk(path string, b, t any) Risk {
	l := strings.ToLower(path)
	if isAuthPath(l) || strings.Contains(l, "headers") || isInsecurePath(l) && boolBecameTrue(redactValue(b, path), redactValue(t, path)) {
		return RiskHigh
	}
	return RiskLow
}

func redactValue(v any, path string) any {
	if v == nil {
		return nil
	}
	if isSensitivePath(path) {
		return MaskedValue
	}
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for _, k := range sortedKeys(x) {
			child := path + "." + k
			if strings.Contains(strings.ToLower(path), "headers") {
				if isSecretKey(k) {
					m[k] = MaskedValue
				} else {
					m[k] = redactValue(x[k], child)
				}
			} else {
				m[k] = redactValue(x[k], child)
			}
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i := range x {
			a[i] = redactValue(x[i], fmt.Sprintf("%s[%d]", path, i))
		}
		return a
	case string:
		if isSensitivePath(path) || looksSecret(x) {
			return MaskedValue
		}
		return sanitizeEndpointIfURL(x)
	default:
		return x
	}
}
func isSensitivePath(path string) bool {
	parts := strings.FieldsFunc(strings.ToLower(path), func(r rune) bool { return r == '.' || r == '_' || r == '-' || r == '/' || r == '[' || r == ']' })
	for _, p := range parts {
		if isSecretKey(p) {
			return true
		}
	}
	return false
}
func isAuthPath(path string) bool {
	return strings.Contains(path, "auth") || strings.Contains(path, "authorization") || strings.Contains(path, "bearer_token") || strings.Contains(path, "api_key") || strings.Contains(path, "client_secret") || strings.Contains(path, "password") || strings.Contains(path, "token")
}
func isSecretKey(k string) bool {
	k = strings.ToLower(strings.ReplaceAll(k, "-", "_"))
	secrets := []string{"authorization", "proxy_authorization", "password", "passwd", "token", "access_token", "refresh_token", "bearer_token", "secret", "client_secret", "api_key", "apikey", "private_key", "key_pem", "cert_pem", "credentials", "cookie", "set_cookie", "x_api_key", "dd_api_key", "signalfx_access_token", "x_sf_token", "x_honeycomb_team", "x_otlp_api_key"}
	for _, s := range secrets {
		if k == s || strings.Contains(k, s) {
			return true
		}
	}
	return false
}
func looksSecret(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "bearer ") || strings.Contains(l, "secret") || strings.Contains(l, "token") || strings.Contains(l, "api-key") || strings.Contains(l, "apikey")
}
func redactSecretLikeString(s string) string {
	if looksSecret(s) {
		return MaskedValue
	}
	return s
}
func sanitizeEndpointIfURL(s string) string {
	if strings.Contains(s, "://") && (strings.Contains(s, "@") || strings.Contains(s, "?")) {
		return sanitizeEndpoint(s)
	}
	return s
}
func isInsecurePath(path string) bool {
	return strings.HasSuffix(path, ".insecure") || strings.HasSuffix(path, ".insecure_skip_verify") || strings.Contains(path, "tls.insecure")
}
func boolBecameTrue(b, t any) bool {
	bv, bok := b.(bool)
	tv, tok := t.(bool)
	return tok && tv && (!bok || !bv)
}

func strongSamplingChange(b, t any) bool {
	bf, bok := number(b)
	tf, tok := number(t)
	if !bok || !tok {
		return false
	}
	if tf == 0 && bf != 0 {
		return true
	}
	if bf == 0 {
		return false
	}
	ratio := tf / bf
	return ratio <= 0.5 || ratio >= 2 || (bf == 100 && tf < 100)
}
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
func isSamplingID(id string) bool {
	l := strings.ToLower(id)
	return strings.Contains(l, "sampling") || strings.Contains(l, "probabilistic_sampler") || strings.Contains(l, "tail_sampling")
}

func asMap(v any) (map[string]any, bool) { m, ok := v.(map[string]any); return m, ok }
func normalizeValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := map[string]any{}
		for k, vv := range x {
			m[k] = normalizeValue(vv)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, vv := range x {
			a[i] = normalizeValue(vv)
		}
		return a
	default:
		return x
	}
}
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, el := range arr {
		if s, ok := el.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func sortedCompKeys(m map[string]component) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func unionComponentIDs(a, b map[string]component) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func unionPipelineKeys(a, b map[string]PipelineShape) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func unionEndpointKeys(a, b map[string]struct {
	component ComponentRef
	value     EndpointValue
}) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func indexMap(s []string) map[string]int {
	m := map[string]int{}
	for i, v := range s {
		m[v] = i
	}
	return m
}
func unionStringKeys(a, b map[string]int) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func unionStringKeysFromMaps(a, b map[string]any) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedBoolKeys(set)
}
func appendRule(rules []string, rule string) []string {
	for _, r := range rules {
		if r == rule {
			return rules
		}
	}
	return append(rules, rule)
}
func dedupeSorted(in []string) []string {
	set := map[string]bool{}
	for _, v := range in {
		if v != "" {
			set[v] = true
		}
	}
	return sortedBoolKeys(set)
}
func canonical(v any) string { b, _ := json.Marshal(v); return string(b) }
func maxRisk(a, b Risk) Risk {
	if riskRank(b) > riskRank(a) {
		return b
	}
	return a
}
func riskRank(r Risk) int {
	switch r {
	case RiskHigh:
		return 3
	case RiskMedium:
		return 2
	case RiskLow:
		return 1
	case RiskNone:
		return 0
	}
	return 0
}
func riskCategory(rule string) string {
	if strings.Contains(rule, "auth") || strings.Contains(rule, "security") || strings.Contains(rule, "tls") || strings.Contains(rule, "insecure") {
		return "security"
	}
	if strings.Contains(rule, "pipeline") || strings.Contains(rule, "exporter") {
		return "data_loss"
	}
	if strings.Contains(rule, "sampling") {
		return "cost"
	}
	return "routing"
}
func titleForRule(rule string) string { return strings.ReplaceAll(rule, "_", " ") }
func countComponents(m map[string]map[string]component) int {
	n := 0
	for _, mm := range m {
		n += len(mm)
	}
	return n
}
func impactedPipelines(base, target map[string]PipelineShape, cat, id string) []string {
	set := map[string]bool{}
	for name, p := range base {
		if pipelineUses(p, cat, id) {
			set[name] = true
		}
	}
	for name, p := range target {
		if pipelineUses(p, cat, id) {
			set[name] = true
		}
	}
	return sortedBoolKeys(set)
}
func pipelineUses(p PipelineShape, cat, id string) bool {
	var list []string
	switch cat {
	case "receivers":
		list = p.Receivers
	case "processors":
		list = p.Processors
	case "exporters":
		list = p.Exporters
	default:
		return false
	}
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}
func walk(v any, path string, fn func(string, any)) {
	fn(path, v)
	if m, ok := asMap(v); ok {
		for _, k := range sortedKeys(m) {
			walk(m[k], path+"."+k, fn)
		}
	}
	if arr, ok := v.([]any); ok {
		for i, x := range arr {
			walk(x, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	}
}
func lastPathPart(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
}
func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int:
		return strconv.Itoa(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	default:
		return "", false
	}
}
