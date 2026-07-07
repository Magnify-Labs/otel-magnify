package oteldiff

import (
	"fmt"
	"reflect"
)

func (e *engine) buildHumanSummary() []HumanSummaryItem {
	items := []HumanSummaryItem{}
	seen := map[string]bool{}
	add := func(item HumanSummaryItem) {
		if item.Text == "" || seen[item.Text] {
			return
		}
		seen[item.Text] = true
		items = append(items, item)
	}

	for _, c := range e.diff.Components {
		componentName := humanComponentName(c.Component.ID)
		componentKind := singular(c.Component.Category)
		componentID := humanSafeLabel(c.Component.ID)
		switch c.Kind {
		case ChangeAdded:
			add(HumanSummaryItem{Text: fmt.Sprintf("Adds %s %s", componentName, componentKind), Category: "component", Kind: c.Kind, Risk: c.Risk, ComponentID: componentID})
		case ChangeRemoved:
			add(HumanSummaryItem{Text: fmt.Sprintf("Removes %s %s", componentName, componentKind), Category: "component", Kind: c.Kind, Risk: c.Risk, ComponentID: componentID})
		case ChangeModified:
			for _, field := range c.ChangedFields {
				add(HumanSummaryItem{Text: fmt.Sprintf("Changes %s %s", componentName, humanFieldName(field.Path)), Category: "field", Kind: c.Kind, Risk: field.Risk, ComponentID: componentID, Path: humanSafePath(field.Path)})
			}
		case ChangeUnchanged:
			// Unchanged components are not emitted by compareComponents.
		}
	}

	for _, p := range e.diff.Pipelines {
		for _, ch := range p.ComponentRefChanges {
			if ch.Section != "exporters" {
				continue
			}
			name := humanComponentName(ch.ComponentID)
			componentID := humanSafeLabel(ch.ComponentID)
			pipelineKey := humanSafeLabel(p.PipelineKey)
			switch ch.Kind {
			case ChangeAdded:
				add(HumanSummaryItem{Text: fmt.Sprintf("Routes %s to %s", p.Signal, name), Category: "pipeline", Kind: ch.Kind, Risk: ch.Risk, ComponentID: componentID, PipelineKey: pipelineKey, Signal: p.Signal})
			case ChangeRemoved:
				// Component removal summaries already cover removed destinations; avoid
				// repeating the same event as both removal and routing noise.
			case ChangeModified, ChangeUnchanged:
				// Pipeline reference diffs are currently additions/removals only.
			}
		}
	}

	if len(e.diff.Pipelines) > 0 {
		changedPipelines := map[string]bool{}
		for _, p := range e.diff.Pipelines {
			changedPipelines[p.PipelineKey] = true
		}
		for _, key := range unionPipelineKeys(e.base.pipelines, e.target.pipelines) {
			if changedPipelines[key] {
				continue
			}
			base, bok := e.base.pipelines[key]
			target, tok := e.target.pipelines[key]
			if bok && tok && reflect.DeepEqual(base, target) {
				signal := signalOf(key)
				add(HumanSummaryItem{Text: fmt.Sprintf("Keeps %s unchanged", signal), Category: "unchanged", Kind: ChangeUnchanged, Risk: RiskNone, PipelineKey: humanSafeLabel(key), Signal: signal})
			}
		}
	}

	return items
}
