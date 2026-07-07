// Package oteldiff computes an OpenTelemetry-aware, redacted semantic diff
// between two Collector YAML configurations.
package oteldiff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"

	"gopkg.in/yaml.v3"
)

// Compare returns the semantic OTel diff for two Collector YAML documents.
func Compare(baseYAML, targetYAML []byte) ConfigDiff {
	return CompareWithContext(baseYAML, targetYAML, BlastRadiusContext{})
}

// CompareWithContext returns the semantic OTel diff plus optional fleet blast-radius metadata.
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
		HumanSummary:  []HumanSummaryItem{},
		Components:    []ComponentDiff{}, Pipelines: []PipelineDiff{}, Endpoints: []EndpointDiff{}, Security: []SecurityDiff{}, RiskItems: []RiskItem{}, Diagnostics: []Diagnostic{},
		Normalized: Normalized{BaseHash: hashYAML(baseYAML), TargetHash: hashYAML(targetYAML)},
	}
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
	e.diff.HumanSummary = e.buildHumanSummary()
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
