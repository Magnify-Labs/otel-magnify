import test from 'node:test'
import assert from 'node:assert/strict'

import api, { configSafetyAPI, getAPIErrorDetails } from '../../src/api/client.ts'
import type { AxiosAdapter, AxiosRequestConfig, AxiosResponse } from 'axios'
import type { EvidenceReport } from '../../src/types.ts'

const emptyReportFixture: EvidenceReport = {
  schema_version: 'config_safety_evidence_report.v1',
  report_id: 'rpt_empty_1234567890',
  generated_at: '2026-07-02T20:00:00Z',
  recommended_version: '0.100.0',
  summary: {
    config_changes: 0,
    validation_failures: 0,
    rollbacks: 0,
    drifted_collectors: 0,
    outdated_collectors: 0,
    audit_events: 1,
  },
  config_changes: [],
  validation_failures: [],
  rollbacks: [],
  drift: {
    generated_at: '2026-07-02T20:00:00Z',
    summary: {
      total_collectors: 0,
      drifted_collectors: 0,
      missing_effective_config: 0,
      pending_too_long: 0,
      unknown_incomplete_components: 0,
      group_heterogeneous_config: 0,
      with_drift_alert: 0,
      with_version_outdated_alert: 0,
    },
    items: [],
  },
  outdated_collectors: [],
  audit_trail: [],
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

function okResponse(
  config: AxiosRequestConfig,
  data: unknown,
  headers: Record<string, string> = {},
): AxiosResponse {
  return {
    data,
    status: 200,
    statusText: 'OK',
    headers,
    config,
  }
}

test('report fetches the JSON evidence summary without requiring signed Enterprise metadata', async () => {
  installLocalStorageMock()
  let seenConfig: AxiosRequestConfig | undefined

  const result = await withAdapter(
    async (config) => {
      seenConfig = config
      return okResponse(config, emptyReportFixture)
    },
    () => configSafetyAPI.report('0.100.0'),
  )

  assert.equal(result.schema_version, 'config_safety_evidence_report.v1')
  assert.equal(result.summary.config_changes, 0)
  assert.equal(result.signature, undefined)
  assert.deepEqual(result.config_changes, [])
  assert.equal(seenConfig?.method, 'get')
  assert.equal(seenConfig?.url, '/reports/config-safety')
  assert.equal(seenConfig?.params?.format, 'json')
  assert.equal(seenConfig?.params?.recommended_version, '0.100.0')
  assert.equal(seenConfig?.responseType, undefined)
})

test('exportReportDownload handles markdown, csv, and pdf blobs with download metadata', async () => {
  installLocalStorageMock()
  const seenConfigs: AxiosRequestConfig[] = []

  await withAdapter(
    async (config) => {
      seenConfigs.push(config)
      const format = config.params?.format as string
      const contentType = format === 'pdf' ? 'application/pdf' : format === 'csv' ? 'text/csv' : 'text/markdown'
      const extension = format === 'markdown' ? 'md' : format
      return okResponse(
        config,
        new Blob([`${format} evidence`], { type: contentType }),
        {
          'Content-Type': contentType,
          'Content-Disposition': `attachment; filename="config-safety-evidence-rpt123.${extension}"`,
        },
      )
    },
    async () => {
      const markdown = await configSafetyAPI.exportReportDownload('markdown', '0.100.0')
      const csv = await configSafetyAPI.exportReportDownload('csv')
      const pdf = await configSafetyAPI.exportReportDownload('pdf')

      assert.equal(markdown.filename, 'config-safety-evidence-rpt123.md')
      assert.equal(markdown.contentType, 'text/markdown')
      assert.equal(markdown.format, 'markdown')
      assert.equal(markdown.blob.type, 'text/markdown')
      assert.equal(csv.filename, 'config-safety-evidence-rpt123.csv')
      assert.equal(pdf.filename, 'config-safety-evidence-rpt123.pdf')
      assert.equal(pdf.contentType, 'application/pdf')
    },
  )

  assert.deepEqual(
    seenConfigs.map((config) => config.params?.format),
    ['markdown', 'csv', 'pdf'],
  )
  assert.equal(seenConfigs[0]?.params?.recommended_version, '0.100.0')
  assert.ok(seenConfigs.every((config) => config.url === '/reports/config-safety'))
  assert.ok(seenConfigs.every((config) => config.responseType === 'blob'))
})

test('exportReportDownload accepts the backend md alias and normalizes its fallback filename', async () => {
  installLocalStorageMock()
  let seenConfig: AxiosRequestConfig | undefined

  const result = await withAdapter(
    async (config) => {
      seenConfig = config
      return okResponse(config, new Blob(['# evidence'], { type: 'text/markdown' }), {
        'content-type': 'text/markdown',
      })
    },
    () => configSafetyAPI.exportReportDownload('md'),
  )

  assert.equal(seenConfig?.params?.format, 'md')
  assert.match(result.filename, /^config-safety-evidence-\d{8}\.md$/)
  assert.equal(result.format, 'md')
  assert.equal(result.contentType, 'text/markdown')
})

test('getAPIErrorDetails exposes report export failures in a UI-friendly shape', () => {
  const details = getAPIErrorDetails({
    isAxiosError: true,
    message: 'Request failed with status code 400',
    response: {
      status: 400,
      data: {
        error: 'unsupported report format',
        code: 'unsupported_report_format',
      },
    },
  })

  assert.equal(details.status, 400)
  assert.equal(details.message, 'unsupported report format')
  assert.equal(details.code, 'unsupported_report_format')
})
