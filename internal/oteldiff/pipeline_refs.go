package oteldiff

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
