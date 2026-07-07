package oteldiff

import (
	"sort"
	"strings"
)

func emptyBlastRadius() BlastRadius {
	return BlastRadius{SchemaVersion: BlastRadiusSchemaVersion, AffectedSignals: []string{}, TouchedExporters: []string{}, ImpactedServices: []BlastRadiusService{}, ImpactedClusters: []string{}, CriticalCollectors: []BlastRadiusCollector{}}
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

	targetWorkloads := []BlastRadiusWorkload{}
	if ctx.Workload.ID != "" || ctx.Workload.DisplayName != "" || len(ctx.Workload.Labels) > 0 || len(ctx.Workload.FingerprintKeys) > 0 {
		targetWorkloads = append(targetWorkloads, ctx.Workload)
	}
	clusterSet := map[string]bool{}
	for _, wl := range targetWorkloads {
		for _, key := range clusterKeys() {
			if v := workloadValue(wl, key); v != "" {
				clusterSet[v] = true
			}
		}
	}
	out.ImpactedClusters = sortedSet(clusterSet)

	scopedWorkloads := append([]BlastRadiusWorkload{}, targetWorkloads...)
	for _, wl := range ctx.FleetPeers {
		if len(clusterSet) > 0 && workloadInClusters(wl, clusterSet) {
			scopedWorkloads = append(scopedWorkloads, wl)
		}
	}

	serviceSeen := map[string]bool{}
	for _, wl := range scopedWorkloads {
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

	for _, wl := range scopedWorkloads {
		if !isCollectorWorkload(wl) {
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
