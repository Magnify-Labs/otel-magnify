import test from 'node:test'
import assert from 'node:assert/strict'

import {
  buildBlastRadiusDisplaySections,
  buildSafeOTelDiffContext,
} from '../../src/lib/blastRadiusDisplay.ts'
import type { OTelBlastRadius } from '../../src/types.ts'

const labels = {
  affectedSignals: 'Affected signals',
  touchedExporters: 'Touched exporters',
  impactedServices: 'Impacted services',
  impactedClusters: 'Impacted clusters',
  criticalCollectors: 'Critical collectors',
}

const empty = {
  affectedSignals: 'Unknown signals',
  touchedExporters: 'No exporters touched',
  impactedServices: 'Unknown services',
  impactedClusters: 'Unknown clusters',
  criticalCollectors: 'No critical collectors',
}

test('buildBlastRadiusDisplaySections exposes all blast radius fields from API data', () => {
  const radius: OTelBlastRadius = {
    schema_version: 'otel-config-blast-radius.v1',
    affected_signals: ['traces', 'metrics'],
    touched_exporters: ['otlp/prod', 'debug'],
    impacted_services: [
      {
        service_name: 'checkout-api',
        workload_id: 'wl-checkout',
        display_name: 'checkout collector',
        type: 'collector',
        status: 'degraded',
      },
    ],
    impacted_clusters: ['prod-eu-1', 'checkout'],
    critical_collectors: [
      {
        workload_id: 'wl-critical',
        display_name: 'critical collector',
        status: 'degraded',
        reasons: ['critical=true', 'degraded'],
      },
    ],
  }

  const sections = buildBlastRadiusDisplaySections(radius, labels, empty)

  assert.deepEqual(
    sections.map((section) => section.label),
    [
      'Impacted services',
      'Impacted clusters',
      'Affected signals',
      'Touched exporters',
      'Critical collectors',
    ],
  )
  assert.deepEqual(sections[0]?.items, ['checkout-api · checkout collector · degraded'])
  assert.deepEqual(sections[1]?.items, ['prod-eu-1', 'checkout'])
  assert.deepEqual(sections[2]?.items, ['traces', 'metrics'])
  assert.deepEqual(sections[3]?.items, ['otlp/prod', 'debug'])
  assert.deepEqual(sections[4]?.items, ['critical collector · degraded · critical=true, degraded'])
})

test('buildBlastRadiusDisplaySections keeps every field visible with empty states', () => {
  const sections = buildBlastRadiusDisplaySections(
    {
      schema_version: 'otel-config-blast-radius.v1',
      affected_signals: [],
      touched_exporters: [],
      impacted_services: [],
      impacted_clusters: [],
      critical_collectors: [],
    },
    labels,
    empty,
  )

  assert.deepEqual(
    sections.map((section) => ({ label: section.label, items: section.items, emptyText: section.emptyText })),
    [
      { label: 'Impacted services', items: [], emptyText: 'Unknown services' },
      { label: 'Impacted clusters', items: [], emptyText: 'Unknown clusters' },
      { label: 'Affected signals', items: [], emptyText: 'Unknown signals' },
      { label: 'Touched exporters', items: [], emptyText: 'No exporters touched' },
      { label: 'Critical collectors', items: [], emptyText: 'No critical collectors' },
    ],
  )
})

test('buildSafeOTelDiffContext keeps safe k8s service identity and drops secret-shaped context', () => {
  const context = buildSafeOTelDiffContext(
    {
      id: 'wl-checkout',
      fingerprint_source: 'k8s',
      fingerprint_keys: {
        'service.name': 'checkout-api',
        'api.token': 'should-not-leak',
      },
      display_name: 'checkout collector',
      type: 'collector',
      version: '0.98.0',
      status: 'connected',
      last_seen_at: '2026-07-04T10:00:00Z',
      labels: {
        'app.kubernetes.io/name': 'checkout-api',
        'k8s.cluster.name': 'prod-eu-1',
        authorization: 'Bearer abcdefghijklmnopqrstuvwxyz123456',
      },
    },
    [
      {
        id: 'wl-payments',
        fingerprint_source: 'k8s',
        fingerprint_keys: {},
        display_name: 'payments collector',
        type: 'collector',
        version: '0.98.0',
        status: 'degraded',
        last_seen_at: '2026-07-04T10:00:00Z',
        labels: {
          'app.kubernetes.io/name': 'payments-api',
          'k8s.namespace.name': 'checkout',
          password: 'should-not-leak',
        },
      },
    ],
    { include_raw_paths: true },
  )

  assert.deepEqual(context.labels, {
    'app.kubernetes.io/name': 'checkout-api',
    'k8s.cluster.name': 'prod-eu-1',
  })
  assert.deepEqual(context.fingerprint_keys, { 'service.name': 'checkout-api' })
  assert.deepEqual(context.fleet_peers?.[0]?.labels, {
    'app.kubernetes.io/name': 'payments-api',
    'k8s.namespace.name': 'checkout',
  })
  assert.equal(context.include_raw_paths, true)
})
