package migrationassistant

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func convertDatadog(state *conversionState) error {
	state.sourceFormat = "yaml"
	var root map[string]any
	if err := yaml.Unmarshal([]byte(state.request.Source), &root); err != nil {
		return err
	}
	if env := asString(root["env"]); env != "" {
		addResourceAttr(state, "deployment.environment", env, "env", "datadog.env.to_resource_attribute")
	}
	if logsConfig, ok := root["logs_config"].(map[string]any); ok {
		for key := range logsConfig {
			if !secretKeyPattern.MatchString(key) {
				state.unsupported = append(state.unsupported, unsupported("logs_config."+key, "Datadog logs_config behavior requires manual review.", "Review Collector receiver and processor settings for equivalent behavior."))
			}
		}
	}
	for _, tag := range asStringSlice(root["tags"]) {
		parts := strings.SplitN(tag, ":", 2)
		if len(parts) == 2 {
			addResourceAttr(state, "otel-magnify.vendor.datadog.tag."+sanitizeAttrKey(parts[0]), parts[1], "tags", "datadog.tags.to_resource_attributes")
		}
	}
	logs, _ := root["logs"].([]any)
	for i, raw := range logs {
		item, _ := raw.(map[string]any)
		if strings.EqualFold(asString(item["type"]), "file") && asString(item["path"]) != "" {
			name := fmt.Sprintf("filelog/datadog_logs_%d", i)
			addLogReceiver(state, name, []string{asString(item["path"])}, fmt.Sprintf("logs[%d].path", i), "receivers."+name+".include", "datadog.logs.file.path_to_filelog.include", "Datadog file log path maps to Collector filelog include glob.")
			if service := asString(item["service"]); service != "" {
				addResourceAttr(state, "service.name", service, fmt.Sprintf("logs[%d].service", i), "datadog.logs.service.to_resource_attribute")
			}
			if source := asString(item["source"]); source != "" {
				addResourceAttr(state, "otel-magnify.vendor.source", source, fmt.Sprintf("logs[%d].source", i), "datadog.logs.source.to_resource_attribute")
			}
		}
		for key := range item {
			switch key {
			case "type", "path", "service", "source":
			default:
				if !secretKeyPattern.MatchString(key) {
					state.unsupported = append(state.unsupported, unsupported(fmt.Sprintf("logs[%d].%s", i, key), "Datadog log processing and vendor-specific settings require manual review.", "Review Collector filelog operators/processors for equivalent behavior."))
				}
			}
		}
	}
	if apm, ok := root["apm_config"].(map[string]any); ok && asBool(apm["enabled"]) {
		state.draft.receivers["otlp"] = nil
		state.draft.pipelines["traces"] = pipeline{receivers: []string{"otlp"}, exporters: []string{defaultExporterName(state)}}
		state.warnings = append(state.warnings, warning("datadog_apm_partial", "warning", "Datadog APM intake semantics are represented as an OTLP traces skeleton only.", "apm_config.enabled"))
		state.mapped++
	}
	if port := asString(root["dogstatsd_port"]); port != "" {
		state.draft.receivers["statsd"] = nil
		state.draft.receiverOptions["statsd"] = []string{"endpoint: 0.0.0.0:" + port}
		state.draft.pipelines["metrics"] = pipeline{receivers: []string{"statsd"}, exporters: []string{defaultExporterName(state)}}
		state.evidence = append(state.evidence, migrationEvidence("dogstatsd_port", "receivers.statsd.endpoint", "datadog.dogstatsd_port.to_statsd.endpoint", "DogStatsD port maps to the Collector statsd receiver endpoint."))
		state.mapped++
	}
	if _, ok := root["site"]; ok {
		state.warnings = append(state.warnings, warning("datadog_site_not_exporter", "warning", "Datadog site is advisory unless the target exporter is datadog.", "site"))
	}
	return nil
}
