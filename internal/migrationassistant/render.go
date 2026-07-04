package migrationassistant

import (
	"fmt"
	"sort"
	"strings"
)

func renderDraft(d draft) string {
	var b strings.Builder
	b.WriteString("receivers:\n")
	for _, name := range sortedMapKeys(d.receivers) {
		paths := d.receivers[name]
		b.WriteString("  " + name + ":")
		if len(paths) == 0 && len(d.receiverOptions[name]) == 0 {
			b.WriteString(" {}\n")
			continue
		}
		b.WriteString("\n")
		if len(paths) > 0 {
			b.WriteString("    include: [")
			for i, p := range paths {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(quoteYAML(p))
			}
			b.WriteString("]\n")
		}
		for _, option := range d.receiverOptions[name] {
			b.WriteString("    " + option + "\n")
		}
	}
	b.WriteString("processors:\n")
	if _, ok := d.processors["resource"]; ok && len(d.resourceAttrs) > 0 {
		b.WriteString("  resource:\n    attributes:\n")
		for _, key := range sortedMapKeys(d.resourceAttrs) {
			b.WriteString("      - key: " + key + "\n")
			b.WriteString("        value: " + quoteYAML(d.resourceAttrs[key]) + "\n")
			b.WriteString("        action: upsert\n")
		}
	}
	b.WriteString("  batch: {}\n")
	b.WriteString("exporters:\n")
	for _, name := range sortedMapKeys(d.exporters) {
		endpoint := d.exporters[name]
		b.WriteString("  " + name + ":\n")
		if name == "splunk_hec" {
			b.WriteString("    endpoint: " + quoteYAML(endpoint) + "\n")
			b.WriteString("    token: ${SPLUNK_HEC_TOKEN}\n")
		} else {
			b.WriteString("    endpoint: " + quoteYAML(endpoint) + "\n")
		}
	}
	b.WriteString("service:\n  pipelines:\n")
	for _, name := range sortedMapKeys(d.pipelines) {
		p := d.pipelines[name]
		b.WriteString("    " + name + ":\n")
		b.WriteString("      receivers: " + renderInlineList(p.receivers) + "\n")
		if len(p.processors) > 0 {
			b.WriteString("      processors: " + renderInlineList(p.processors) + "\n")
		}
		b.WriteString("      exporters: " + renderInlineList(p.exporters) + "\n")
	}
	return b.String()
}

func sortedMapKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func quoteYAML(s string) string {
	if s == "" {
		return `""`
	}
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return s
	}
	if strings.ContainsAny(s, ":#[]{}*,&!|>'\"%@` ") {
		return fmt.Sprintf("%q", s)
	}
	return s
}

func renderInlineList(items []string) string {
	return "[" + strings.Join(items, ", ") + "]"
}
