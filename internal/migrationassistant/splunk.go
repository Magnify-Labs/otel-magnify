package migrationassistant

import (
	"strings"
)

func convertSplunk(state *conversionState) error {
	state.sourceFormat = "ini"
	sections := parseBracketConf(state.request.Source)
	monitorIndex := 0
	for _, section := range sections {
		lowerHeader := strings.ToLower(section.Header)
		switch {
		case strings.HasPrefix(lowerHeader, "monitor://"):
			path := strings.TrimPrefix(section.Header, "monitor://")
			if sectionValue(section, "disabled") == "1" {
				state.warnings = append(state.warnings, warning("splunk_monitor_disabled", "warning", "Disabled Splunk monitor was not included in the active Collector draft.", "["+section.Header+"]"))
				state.evidence = append(state.evidence, migrationEvidence("["+section.Header+"].disabled", "service.pipelines.logs.receivers", "splunk.monitor.disabled.skip", "Disabled monitor is intentionally omitted from the draft."))
				continue
			}
			name := "filelog/splunk_monitor_" + itoa(monitorIndex)
			monitorIndex++
			addLogReceiver(state, name, []string{path}, "["+section.Header+"]", "receivers."+name+".include", "splunk.monitor.path_to_filelog.include", "Splunk monitor path maps to Collector filelog include glob.")
			if v := sectionValue(section, "sourcetype"); v != "" {
				addResourceAttr(state, "splunk.sourcetype", v, "["+section.Header+"].sourcetype", "splunk.monitor.sourcetype.to_resource_attribute")
			}
			if v := sectionValue(section, "index"); v != "" {
				addResourceAttr(state, "splunk.index", v, "["+section.Header+"].index", "splunk.monitor.index.to_resource_attribute")
			}
			if v := sectionValue(section, "host"); v != "" {
				addResourceAttr(state, "host.name", v, "["+section.Header+"].host", "splunk.monitor.host.to_resource_attribute")
			}
		case strings.Contains(lowerHeader, "httpout") || strings.Contains(lowerHeader, "hec"):
			if endpoint := sectionValue(section, "uri"); endpoint != "" {
				state.draft.exporters["splunk_hec"] = endpoint
				state.evidence = append(state.evidence, migrationEvidence("["+section.Header+"].uri", "exporters.splunk_hec.endpoint", "splunk.outputs.hec.uri_to_splunk_hec.endpoint", "Splunk HEC endpoint maps to Collector splunk_hec exporter endpoint."))
				for name, p := range state.draft.pipelines {
					p.exporters = []string{"splunk_hec"}
					state.draft.pipelines[name] = p
				}
			}
		case strings.Contains(lowerHeader, "props"):
			state.unsupported = append(state.unsupported, unsupported("["+section.Header+"]", "Splunk props/transforms semantics are not migrated automatically.", "Review timestamp, line breaking, and transform settings manually."))
		case strings.HasPrefix(lowerHeader, "source::"):
			for key := range section.Keys {
				state.unsupported = append(state.unsupported, unsupported("["+section.Header+"]."+strings.ToUpper(key), "Splunk source stanza parsing semantics are not migrated automatically.", "Review timestamp, line breaking, and transform settings manually."))
			}
		}
	}
	return nil
}
