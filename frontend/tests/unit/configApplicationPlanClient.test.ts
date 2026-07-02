import test from 'node:test'
import assert from 'node:assert/strict'

import api, { getAPIErrorDetails, workloadsAPI } from '../../src/api/client.ts'
import type { AxiosAdapter, AxiosRequestConfig, AxiosResponse } from 'axios'
import type { ConfigApplicationPlan } from '../../src/types.ts'

const yaml = 'receivers:\n  otlp: {}\n'

const planFixture: ConfigApplicationPlan = {
  schema_version: 'config_application_plan.v1',
  workload_id: 'w-plan',
  config_hash: 'a'.repeat(64),
  summary: {
    target_count: 1,
    collector_target_count: 1,
    remote_config_capable_count: 0,
    read_only_count: 1,
    validation_ok_count: 1,
    validation_failed_count: 0,
    components_missing_count: 0,
    high_risk_change_count: 0,
    excluded_count: 1,
  },
  targets: [
    {
      workload_id: 'w-plan',
      display_name: 'collector-prod',
      type: 'collector',
      accepts_remote_config: false,
      read_only: true,
      validation_status: 'ok',
      validation_errors: [],
      components_missing_count: 0,
      high_risk_change_count: 0,
      excluded: true,
      exclusion_reasons: ['read_only'],
      hard_failures: ['read_only'],
      active_config_unavailable: false,
    },
  ],
  hard_failures: ['all_targets_excluded'],
  can_push: false,
  apply_allowed: false,
  export: {
    supported: true,
    formats: ['json', 'markdown'],
    json_endpoint: '/api/workloads/w-plan/config/plan/export?format=json',
    markdown_endpoint: '/api/workloads/w-plan/config/plan/export?format=markdown',
    persisted_rollout: 'not_persisted',
  },
}

function installLocalStorageMock() {
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: { getItem: () => null, removeItem: () => undefined },
  })
}

function headerValue(config: AxiosRequestConfig, name: string) {
  const headers = config.headers as { get?: (key: string) => unknown; [key: string]: unknown } | undefined
  return headers?.get?.(name) ?? headers?.[name]
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

test('planConfig posts YAML to the config application plan endpoint', async () => {
  installLocalStorageMock()
  let seenConfig: AxiosRequestConfig | undefined

  const result = await withAdapter(
    async (config) => {
      seenConfig = config
      return okResponse(config, planFixture)
    },
    () => workloadsAPI.planConfig('w-plan', yaml),
  )

  assert.equal(result.schema_version, 'config_application_plan.v1')
  assert.equal(result.export.persisted_rollout, 'not_persisted')
  assert.equal(seenConfig?.method, 'post')
  assert.equal(seenConfig?.url, '/workloads/w-plan/config/plan')
  assert.equal(seenConfig?.data, yaml)
  assert.equal(headerValue(seenConfig!, 'Content-Type'), 'text/yaml')
})

test('exportConfigPlanJson and exportConfigPlanMarkdown use supported export formats', async () => {
  installLocalStorageMock()
  const seenConfigs: AxiosRequestConfig[] = []
  const markdownBlob = new Blob(['# Config Safety Plan'], { type: 'text/markdown' })

  await withAdapter(
    async (config) => {
      seenConfigs.push(config)
      return okResponse(config, config.params?.format === 'markdown' ? markdownBlob : planFixture)
    },
    async () => {
      const jsonPlan = await workloadsAPI.exportConfigPlanJson('w-plan', yaml)
      const markdown = await workloadsAPI.exportConfigPlanMarkdown('w-plan', yaml)
      assert.equal(jsonPlan.config_hash, planFixture.config_hash)
      assert.equal(markdown, markdownBlob)
    },
  )

  assert.equal(seenConfigs[0]?.url, '/workloads/w-plan/config/plan/export')
  assert.equal(seenConfigs[0]?.params?.format, 'json')
  assert.equal(seenConfigs[0]?.responseType, undefined)
  assert.equal(seenConfigs[1]?.params?.format, 'markdown')
  assert.equal(seenConfigs[1]?.responseType, 'blob')
})

test('getAPIErrorDetails preserves status, code, and validation errors for blocked pushes', () => {
  const details = getAPIErrorDetails({
    isAxiosError: true,
    message: 'Request failed with status code 400',
    response: {
      status: 400,
      data: {
        error: 'configuration failed validation',
        code: 'validation_failed',
        validation_errors: [{ code: 'missing_pipeline', message: 'service pipeline missing' }],
      },
    },
  })

  assert.equal(details.status, 400)
  assert.equal(details.message, 'configuration failed validation')
  assert.equal(details.code, 'validation_failed')
  assert.equal(details.validation_errors?.[0]?.code, 'missing_pipeline')
})
