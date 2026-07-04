package migrationassistant

import (
	"strings"

	"gopkg.in/yaml.v3"
)

func convertNewRelic(state *conversionState) error {
	state.sourceFormat = "yaml"
	var root map[string]any
	if err := yaml.Unmarshal([]byte(state.request.Source), &root); err != nil {
		return err
	}
	if displayName := asString(root["display_name"]); displayName != "" {
		addResourceAttr(state, "host.name", displayName, "display_name", "newrelic.display_name.to_host_name")
	}
	if attrs, ok := root["custom_attributes"].(map[string]any); ok {
		for key, value := range attrs {
			addResourceAttr(state, key, asString(value), "custom_attributes."+key, "newrelic.custom_attributes.to_resource_attributes")
		}
	}
	if logFile := asString(root["log_file"]); logFile != "" {
		addLogReceiver(state, "filelog/newrelic_log_0", []string{logFile}, "log_file", "receivers.filelog/newrelic_log_0.include", "newrelic.log_file.path_to_filelog.include", "New Relic infra log_file maps to Collector filelog include glob.")
	}
	for key, value := range root {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "metrics_") && !strings.Contains(lower, "disabled") && asString(value) != "false" {
			state.draft.receivers["hostmetrics"] = nil
			state.draft.receiverOptions["hostmetrics"] = []string{"collection_interval: 60s", "scrapers:", "  cpu: {}", "  memory: {}", "  filesystem: {}"}
			state.draft.pipelines["metrics"] = pipeline{receivers: []string{"hostmetrics"}, exporters: []string{defaultExporterName(state)}}
			state.evidence = append(state.evidence, migrationEvidence(key, "receivers.hostmetrics", "newrelic.metrics_toggle.to_hostmetrics_skeleton", "New Relic metrics toggle is represented as a hostmetrics receiver skeleton."))
			state.warnings = append(state.warnings, warning("newrelic_metrics_partial", "warning", "New Relic dimensional metric normalization is not migrated; hostmetrics is a starting skeleton only.", key))
			state.mapped++
			break
		}
	}
	for key := range root {
		switch key {
		case "license_key", "display_name", "custom_attributes", "log_file":
		default:
			if !strings.HasPrefix(strings.ToLower(key), "metrics_") && !secretKeyPattern.MatchString(key) {
				state.unsupported = append(state.unsupported, unsupported(key, "New Relic infra agent setting requires manual review.", "Review Collector receiver/processor equivalents manually."))
			}
		}
	}
	return nil
}
