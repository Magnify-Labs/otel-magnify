package migrationassistant

import (
	"strconv"
	"strings"
)

type confSection struct {
	Name   string
	Header string
	Keys   map[string][]string
}

func parseBracketConf(source string) []confSection {
	sections := []confSection{}
	var current *confSection
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			if current != nil {
				sections = append(sections, *current)
			}
			header := strings.TrimSpace(line[1:strings.Index(line, "]")])
			fields := strings.Fields(header)
			if len(fields) == 0 {
				current = nil
				continue
			}
			current = &confSection{Name: fields[0], Header: header, Keys: map[string][]string{}}
			continue
		}
		if current == nil {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			idx = strings.IndexAny(line, " \t")
		}
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		current.Keys[strings.ToLower(key)] = append(current.Keys[strings.ToLower(key)], value)
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return sections
}

func sectionValue(section confSection, key string) string {
	values := section.Keys[strings.ToLower(key)]
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[len(values)-1])
}

func convertFluentBit(state *conversionState) error {
	state.sourceFormat = "conf"
	sections := parseBracketConf(state.request.Source)
	if len(sections) == 0 {
		state.warnings = append(state.warnings, warning("parse_fallback", "warning", "Source did not contain recognizable Fluent Bit sections; generated a low-confidence skeleton for manual completion.", "source"))
	}
	inputIndex := 0
	for _, section := range sections {
		switch strings.ToUpper(section.Name) {
		case "INPUT":
			if strings.EqualFold(sectionValue(section, "Name"), "tail") && sectionValue(section, "Path") != "" {
				name := "filelog/fluentbit_tail_" + itoa(inputIndex)
				inputIndex++
				addLogReceiver(state, name, []string{sectionValue(section, "Path")}, "[INPUT].Path", "receivers."+name+".include", "fluentbit.input.tail.path_to_filelog.include", "Fluent Bit tail Path maps to Collector filelog include glob.")
				if strings.EqualFold(sectionValue(section, "Parser"), "json") {
					state.draft.receiverOptions[name] = append(state.draft.receiverOptions[name], "operators:", "  - type: json_parser")
					state.evidence = append(state.evidence, migrationEvidence("[INPUT].Parser", "receivers."+name+".operators", "fluentbit.input.parser_json.to_filelog.json_parser", "Simple json Parser maps to a filelog json_parser operator."))
				}
			}
		case "FILTER":
			switch strings.ToLower(sectionValue(section, "Name")) {
			case "kubernetes":
				state.warnings = append(state.warnings, warning("fluentbit_kubernetes_filter_partial", "warning", "Kubernetes filter metadata enrichment should be reviewed against the Collector k8sattributes processor.", "[FILTER] Name kubernetes"))
			case "modify", "record_modifier":
				for key, values := range section.Keys {
					if key == "name" || key == "match" || len(values) == 0 {
						continue
					}
					addResourceAttr(state, "otel-magnify.vendor.fluentbit."+sanitizeAttrKey(key), values[len(values)-1], "[FILTER]."+key, "fluentbit.filter.static_key.to_resource_attribute")
				}
			default:
				state.unsupported = append(state.unsupported, unsupported("[FILTER] Name "+sectionValue(section, "Name"), "Fluent Bit filter semantics require manual review.", "Map supported enrichment to Collector processors manually."))
			}
		case "OUTPUT":
			if strings.EqualFold(sectionValue(section, "Name"), "opentelemetry") || strings.EqualFold(sectionValue(section, "Name"), "forward") || strings.EqualFold(sectionValue(section, "Name"), "http") {
				state.evidence = append(state.evidence, migrationEvidence("[OUTPUT].Name", "exporters.otlp", "fluentbit.output.to_otlp_exporter_hint", "Output stanza is represented by the configured OTLP exporter placeholder."))
			}
		case "PARSER":
			state.unsupported = append(state.unsupported, unsupported("[PARSER]", "Custom Fluent Bit parser fidelity is not migrated automatically.", "Review filelog operators for equivalent parsing."))
		}
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }
