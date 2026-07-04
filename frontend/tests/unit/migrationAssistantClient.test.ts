import test from 'node:test'
import assert from 'node:assert/strict'

import api, { configsAPI } from '../../src/api/client.ts'
import type { AxiosAdapter, AxiosRequestConfig, AxiosResponse } from 'axios'
import type { Config, ConfigMigrationPreviewResponse } from '../../src/types.ts'

const draftYaml = 'receivers:\n  otlp: {}\nexporters:\n  otlp:\n    endpoint: ${OTLP_ENDPOINT}\n'

const previewFixture: ConfigMigrationPreviewResponse = {
  schema_version: 'config_migration_preview.v1',
  vendor: 'datadog_agent',
  source_format: 'yaml',
  draft_yaml: draftYaml,
  draft_name: 'Migrated Datadog Agent draft',
  confidence: 'medium',
  summary: 'Mapped Datadog logs and OTLP exporter placeholders.',
  warnings: [{ code: 'partial', severity: 'warning', message: 'Review vendor-specific checks.' }],
  unsupported_keys: [{ path: 'apm_config', reason: 'APM tuning needs manual review.' }],
  evidence: [
    {
      source_path: 'logs_enabled',
      target_path: 'service.pipelines.logs',
      rule_id: 'datadog.logs.enabled',
      explanation: 'Log collection maps to a logs pipeline.',
    },
  ],
  redactions: [{ path: 'api_key', placeholder: '${DATADOG_API_KEY}', reason: 'secret-like value' }],
  validation: {
    valid: true,
    overall_status: 'ok',
    summary: 'Collector YAML is syntactically valid.',
    validated_at: '2026-07-04T12:00:00Z',
  },
  save_hint: {
    kind: 'draft',
    source_type: 'migration_assistant',
    tags: ['migration', 'datadog_agent'],
    category: 'migration',
    stack: 'datadog',
  },
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

function response(config: AxiosResponse['config'], data: unknown, status = 200): AxiosResponse {
  return {
    data,
    status,
    statusText: status === 201 ? 'Created' : 'OK',
    headers: {},
    config,
  }
}

test('previewConfigMigration posts a typed request and normalizes list fields', async () => {
  installLocalStorageMock()
  let seenConfig: AxiosRequestConfig | undefined

  const result = await withAdapter(
    async (config) => {
      seenConfig = config
      return response(config, { ...previewFixture, unsupported_keys: undefined })
    },
    () =>
      configsAPI.previewConfigMigration({
        schema_version: 'config_migration_preview_request.v1',
        vendor: 'datadog_agent',
        source: 'logs_enabled: true\napi_key: ${DATADOG_API_KEY}\n',
        source_format: 'yaml',
        context: { target_exporter: 'otlp', otlp_endpoint: '${OTLP_ENDPOINT}' },
      }),
  )

  assert.equal(seenConfig?.method, 'post')
  assert.equal(seenConfig?.url, '/configs/migration-assistant/preview')
  assert.deepEqual(JSON.parse(String(seenConfig?.data)), {
    schema_version: 'config_migration_preview_request.v1',
    vendor: 'datadog_agent',
    source: 'logs_enabled: true\napi_key: ${DATADOG_API_KEY}\n',
    source_format: 'yaml',
    context: { target_exporter: 'otlp', otlp_endpoint: '${OTLP_ENDPOINT}' },
  })
  assert.equal(result.draft_name, 'Migrated Datadog Agent draft')
  assert.deepEqual(result.unsupported_keys, [])
  assert.equal(result.redactions[0]?.placeholder, '${DATADOG_API_KEY}')
})

test('create can persist migration assistant drafts with draft metadata', async () => {
  installLocalStorageMock()
  let seenPayload: unknown
  const createdConfig: Config = {
    id: 'cfg-migration-draft',
    name: previewFixture.draft_name,
    content: draftYaml,
    created_at: '2026-07-04T12:01:00Z',
    created_by: 'operator@example.com',
    kind: 'draft',
    status: 'draft',
    category: 'migration',
    stack: 'datadog',
    tags: ['migration', 'datadog_agent'],
    source_type: 'migration_assistant',
  }

  const result = await withAdapter(
    async (config) => {
      seenPayload = JSON.parse(String(config.data))
      return response(config, createdConfig, 201)
    },
    () =>
      configsAPI.create(previewFixture.draft_name, draftYaml, {
        kind: 'draft',
        status: 'draft',
        source_type: 'migration_assistant',
        category: 'migration',
        stack: 'datadog',
        tags: ['migration', 'datadog_agent'],
      }),
  )

  assert.deepEqual(seenPayload, {
    name: previewFixture.draft_name,
    content: draftYaml,
    kind: 'draft',
    status: 'draft',
    source_type: 'migration_assistant',
    category: 'migration',
    stack: 'datadog',
    tags: ['migration', 'datadog_agent'],
  })
  assert.equal(result.kind, 'draft')
  assert.equal(result.status, 'draft')
  assert.equal(result.source_type, 'migration_assistant')
})
