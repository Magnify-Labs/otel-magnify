import type { OTelBlastRadius, OTelConfigDiffContext, Workload } from '../types'

export interface BlastRadiusLabels {
  affectedSignals: string
  touchedExporters: string
  impactedServices: string
  impactedClusters: string
  criticalCollectors: string
}

export interface BlastRadiusEmptyLabels {
  affectedSignals: string
  touchedExporters: string
  impactedServices: string
  impactedClusters: string
  criticalCollectors: string
}

export interface BlastRadiusDisplaySection {
  key:
    | 'impacted_services'
    | 'impacted_clusters'
    | 'affected_signals'
    | 'touched_exporters'
    | 'critical_collectors'
  label: string
  items: string[]
  emptyText: string
}

const safeLabelKeys = new Set([
  'app',
  'app.kubernetes.io/name',
  'application',
  'cluster',
  'critical',
  'deployment.environment',
  'env',
  'environment',
  'k8s.cluster.name',
  'k8s.namespace.name',
  'namespace',
  'service',
  'service.name',
  'team',
  'workload_type',
])

const unsafeKeyPattern = /auth|bearer|cookie|credential|key|password|secret|token/i
const unsafeValuePattern =
  /bearer\s+|[?&](?:token|api_key|apikey|password|secret)=|-----BEGIN|\b[A-Za-z0-9_-]{32,}\b/i

export function buildBlastRadiusDisplaySections(
  radius: OTelBlastRadius | undefined,
  labels: BlastRadiusLabels,
  empty: BlastRadiusEmptyLabels,
): BlastRadiusDisplaySection[] {
  return [
    {
      key: 'impacted_services',
      label: labels.impactedServices,
      items: (radius?.impacted_services ?? []).map((service) =>
        joinParts([service.service_name, service.display_name, service.status]),
      ),
      emptyText: empty.impactedServices,
    },
    {
      key: 'impacted_clusters',
      label: labels.impactedClusters,
      items: radius?.impacted_clusters ?? [],
      emptyText: empty.impactedClusters,
    },
    {
      key: 'affected_signals',
      label: labels.affectedSignals,
      items: radius?.affected_signals ?? [],
      emptyText: empty.affectedSignals,
    },
    {
      key: 'touched_exporters',
      label: labels.touchedExporters,
      items: radius?.touched_exporters ?? [],
      emptyText: empty.touchedExporters,
    },
    {
      key: 'critical_collectors',
      label: labels.criticalCollectors,
      items: (radius?.critical_collectors ?? []).map((collector) =>
        joinParts([
          collector.display_name || collector.workload_id,
          collector.status,
          collector.reasons.length > 0 ? collector.reasons.join(', ') : undefined,
        ]),
      ),
      emptyText: empty.criticalCollectors,
    },
  ]
}

export function buildSafeOTelDiffContext(
  workload: Workload,
  fleetPeers: Workload[] = [],
  labels: Pick<OTelConfigDiffContext, 'base_label' | 'target_label' | 'include_raw_paths'> = {},
): OTelConfigDiffContext {
  return {
    workload_id: workload.id,
    display_name: workload.display_name,
    workload_type: workload.type,
    status: workload.status,
    labels: safeStringMap(workload.labels),
    fingerprint_keys: safeStringMap(workload.fingerprint_keys),
    fleet_peers: fleetPeers
      .filter((peer) => peer.id !== workload.id)
      .map((peer) => ({
        id: peer.id,
        display_name: peer.display_name,
        workload_type: peer.type,
        status: peer.status,
        labels: safeStringMap(peer.labels),
        fingerprint_keys: safeStringMap(peer.fingerprint_keys),
      })),
    ...labels,
  }
}

function joinParts(parts: Array<string | undefined>): string {
  return parts.filter((part): part is string => Boolean(part?.trim())).join(' · ')
}

function safeStringMap(
  input: Record<string, string> | undefined,
): Record<string, string> | undefined {
  const entries = Object.entries(input ?? {}).filter(
    ([key, value]) =>
      safeLabelKeys.has(key) && !unsafeKeyPattern.test(key) && !unsafeValuePattern.test(value),
  )
  return entries.length > 0 ? Object.fromEntries(entries) : undefined
}
