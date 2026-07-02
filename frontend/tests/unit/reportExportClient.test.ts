import test from 'node:test'
import assert from 'node:assert/strict'

import api, { reportsAPI } from '../../src/api/client.ts'
import type { AxiosAdapter, AxiosRequestConfig, AxiosResponse } from 'axios'
import type { EvidencePack, ReportExportRequest } from '../../src/types.ts'

const request: ReportExportRequest = {
  schema_version: 'report_export_request.v1',
  report_type: 'evidence_pack',
  scope: { workload_ids: ['collector-prod'], since: '2026-07-02T19:00:00Z' },
  include: {
    workload_summary: true,
    config_history: true,
    current_config: true,
    config_plan: true,
    drift_findings: true,
    version_intelligence: true,
    alerts: true,
    workload_events: true,
    rollback_readiness: true,
    audit_verification: true,
    signed_audit_metadata: true,
  },
  redaction: 'strict',
}

const previewFixture: EvidencePack = {
  schema_version: 'evidence_pack.v1',
  generated_at: '2026-07-02T20:00:00Z',
  inputs_hash: 'inputs-hash',
  report_hash: 'report-hash',
  scope: {
    workload_ids: ['collector-prod'],
    workload_count: 1,
    requested_scope: request.scope,
  },
  sections: [
    {
      id: 'audit_verification',
      title: 'Audit verification',
      order: 90,
      items: [
        {
          id: 'audit/head',
          resource: 'audit',
          resource_id: 'head',
          observed_at: '2026-07-02T19:00:00Z',
          severity: 'info',
          summary: 'audit chain verified',
          facts: { verified: true, head_hash: 'abc123' },
          content_hash: 'content-hash',
          redacted: true,
        },
      ],
      csv_table: {
        columns: [
          'section_id',
          'item_id',
          'resource',
          'resource_id',
          'observed_at',
          'severity',
          'summary',
          'key',
          'value',
          'content_hash',
          'redacted',
        ],
        rows: [
          [
            'audit_verification',
            'audit/head',
            'audit',
            'head',
            '2026-07-02T19:00:00Z',
            'info',
            'audit chain verified',
            'verified',
            'true',
            'content-hash',
            'true',
          ],
        ],
      },
    },
  ],
  signatures: [
    {
      scheme: 'none',
      signed_at: '2026-07-02T20:01:00Z',
      payload_hash: 'report-hash',
      verifier: 'community-none',
    },
  ],
  signed_audit: {
    status: 'verified',
    verifier: 'enterprise-audit-chain',
    verified_from: '2026-07-02T19:00:00Z',
    verified_until: '2026-07-02T20:00:00Z',
    head_hash: 'audit-head',
    checked_at: '2026-07-02T20:01:00Z',
  },
  warnings: [{ code: 'pdf_minimal_renderer', message: 'PDF renderer is text-only' }],
}

function installLocalStorageMock() {
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: { getItem: () => null, removeItem: () => undefined },
  })
}

async function withAdapter<T>(adapter: AxiosAdapter, run: () => Promise<T>) {
  const previousAdapter = api.defaults.adapter
  api.defaults.adapter = adapter
  try {
    return await run()
  } finally {
    api.defaults.adapter = previousAdapter
  }
}

function okResponse(config: AxiosRequestConfig, data: unknown): AxiosResponse {
  return {
    data,
    status: 200,
    statusText: 'OK',
    headers: {},
    config,
  }
}

test('reportsAPI exports evidence packs as markdown, csv, and pdf blobs', async () => {
  installLocalStorageMock()
  const seenConfigs: AxiosRequestConfig[] = []
  const markdown = new Blob(['# Evidence Pack'], { type: 'text/markdown' })
  const csv = new Blob(['section_id,item_id\n'], { type: 'text/csv' })
  const pdf = new Blob(['%PDF-1.4'], { type: 'application/pdf' })

  await withAdapter(
    async (config) => {
      seenConfigs.push(config)
      const data =
        config.params?.format === 'csv' ? csv : config.params?.format === 'pdf' ? pdf : markdown
      return okResponse(config, data)
    },
    async () => {
      assert.equal(await reportsAPI.exportEvidencePack('markdown', request), markdown)
      assert.equal(await reportsAPI.exportEvidencePack('csv', request), csv)
      assert.equal(await reportsAPI.exportEvidencePack('pdf', request), pdf)
    },
  )

  assert.equal(seenConfigs.length, 3)
  for (const config of seenConfigs) {
    assert.equal(config.method, 'post')
    assert.equal(config.url, '/reports/evidence-pack/export')
    assert.equal(config.headers?.get?.('Content-Type'), 'application/json')
    assert.equal(config.responseType, 'blob')
    assert.deepEqual(JSON.parse(config.data as string), request)
  }
  assert.deepEqual(
    seenConfigs.map((config) => config.params?.format),
    ['markdown', 'csv', 'pdf'],
  )
})

test('reportsAPI previews evidence pack JSON contract including signed audit metadata', async () => {
  installLocalStorageMock()
  let seenConfig: AxiosRequestConfig | undefined

  const result = await withAdapter(
    async (config) => {
      seenConfig = config
      return okResponse(config, previewFixture)
    },
    () => reportsAPI.previewEvidencePack(request),
  )

  assert.equal(result.schema_version, 'evidence_pack.v1')
  assert.equal(result.signed_audit?.status, 'verified')
  assert.equal(result.signatures?.[0]?.verifier, 'community-none')
  assert.equal(seenConfig?.method, 'post')
  assert.equal(seenConfig?.url, '/reports/evidence-pack')
  assert.equal(seenConfig?.headers?.get?.('Content-Type'), 'application/json')
  assert.deepEqual(JSON.parse(seenConfig?.data as string), request)
})
