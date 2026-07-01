import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { safeRemoteErrorText } from '../../src/lib/safeRemoteErrorText'

const WORKLOAD_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const ACTIVE_CONFIG_ID = 'abc123'

function mockWorkload(page: Page, overrides: Record<string, unknown> = {}) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: WORKLOAD_ID,
        fingerprint_source: 'k8s',
        fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
        display_name: 'test-collector',
        type: 'collector',
        version: '0.98.0',
        status: 'connected',
        last_seen_at: new Date().toISOString(),
        labels: {},
        active_config_id: ACTIVE_CONFIG_ID,
        accepts_remote_config: true,
        available_components: {
          components: {
            receivers: ['otlp'],
            exporters: ['logging', 'debug'],
          },
        },
        ...overrides,
      }),
    }),
  )
}

function mockConfig(page: Page, content: string) {
  return page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: ACTIVE_CONFIG_ID,
        name: 'current',
        content,
        created_at: new Date().toISOString(),
        created_by: 'test',
      }),
    }),
  )
}

function mockHistory(page: Page, rows: unknown[]) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/configs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(rows),
    }),
  )
}

function mockKnownGoodMissing(page: Page) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/known-good`, (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'known-good config not found' }),
    }),
  )
}

function mockValidate(page: Page, result: Record<string, unknown> & { valid: boolean }) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/config/validate`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(result),
    }),
  )
}

const VALIDATION_CHECKS_SUCCESS = [
  {
    id: 'yaml_static',
    label: 'YAML syntax',
    source: 'server.static_yaml',
    status: 'passed',
    required: true,
    messages: [{ code: 'yaml_parse_ok', severity: 'info', message: 'YAML parsed successfully.' }],
  },
  {
    id: 'collector_structure',
    label: 'Collector structure',
    source: 'server.structure',
    status: 'passed',
    required: true,
    messages: [
      {
        code: 'collector_structure_ok',
        severity: 'info',
        message: 'Pipelines reference defined components.',
      },
    ],
  },
  {
    id: 'component_availability',
    label: 'Components available on workload',
    source: 'workload.available_components',
    status: 'passed',
    required: true,
    messages: [
      {
        code: 'components_available',
        severity: 'info',
        message: 'All referenced components are available on this workload.',
      },
    ],
  },
  {
    id: 'otelcol_runtime',
    label: 'Collector runtime validation',
    source: 'otelcol.binary',
    status: 'passed',
    required: false,
    metadata: {
      binary_version: '0.150.1',
      target_version: '0.150.1',
      binary_path: '/usr/local/bin/otelcol',
    },
    messages: [
      { code: 'otelcol_validate_ok', severity: 'info', message: 'otelcol validate succeeded.' },
    ],
  },
]

test.beforeEach(async ({ loggedInPage: page }) => {
  await mockConfigsList(page, [])
  await mockKnownGoodMissing(page)
})

const PUSH_STATUS_LABEL_CASES = [
  ['pending', 'Pending'],
  ['validated', 'Validated'],
  ['submitted', 'Submitted'],
  ['sent', 'Sent via OpAMP'],
  ['applying', 'Applying'],
  ['applied', '✓ Applied'],
  ['failed', '✗ Failed'],
  ['rollback_started', 'Rolling back'],
  ['rollback_applied', 'Rolled back'],
  ['rollback_failed', 'Rollback failed'],
] as const

test('push status labels render every backend status', async ({ loggedInPage: page }) => {
  let currentStatus: (typeof PUSH_STATUS_LABEL_CASES)[number][0] = 'pending'
  await mockWorkload(page, {
    get current_config_push() {
      return {
        push_id: `push_${currentStatus}`,
        workload_id: WORKLOAD_ID,
        config_id: 'feedfacefeedface',
        config_hash: 'feedfacefeedface',
        status: currentStatus,
        submitted_at: '2026-06-30T10:12:00Z',
        sent_at: '2026-06-30T10:12:01Z',
        updated_at: '2026-06-30T10:12:05Z',
        target_count: 1,
        applied_count: currentStatus === 'applied' || currentStatus === 'rollback_applied' ? 1 : 0,
        failed_count: currentStatus === 'failed' || currentStatus === 'rollback_failed' ? 1 : 0,
        pending_count:
          currentStatus === 'applied' ||
          currentStatus === 'failed' ||
          currentStatus.startsWith('rollback_')
            ? 0
            : 1,
      }
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  for (const [status, label] of PUSH_STATUS_LABEL_CASES) {
    currentStatus = status
    await page.goto('/login')
    await page.goto(`/workloads/${WORKLOAD_ID}`)
    await expect(page.locator('.push-banner-label')).toHaveText(label)
  }
})

test('edit button enables YAML editing (regression)', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await expect(page.getByRole('button', { name: 'Validate for this collector' })).toBeVisible()

  // The draft editor is the second `.cm-content` after Edit is clicked? Actually
  // when entering edit mode, only the draft editor remains (readOnly one unmounts).
  const editor = page.locator('.cm-content').first()
  await editor.click()
  await page.keyboard.type('# edited')
  await expect(editor).toContainText('# edited')
})

test('config safety explains the supervised collector flow before the editor', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const safety = page.locator('.config-safety-card')
  await expect(safety).toBeVisible()
  await expect(safety).toContainText('Know what will change before it reaches this workload.')
  await expect(safety.locator('.config-safety-step-title')).toHaveText([
    'Validate before push',
    'Compare with active config',
    'Push safely',
    'Roll back to known-good',
  ])
  await expect(safety).toContainText('Validation required')
  await expect(safety).toContainText('Diff available')
  await expect(safety).toContainText('Validate the configuration first.')
  await expect(safety).toContainText('History available')

  const safetyTop = await safety.boundingBox()
  const configTop = await page.getByText('Configuration', { exact: true }).boundingBox()
  expect(safetyTop?.y).toBeLessThan(configTop?.y ?? Number.POSITIVE_INFINITY)
})

test('workload detail stays readable without horizontal overflow on mobile', async ({
  loggedInPage: page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const viewport = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }))
  expect(viewport.scrollWidth).toBeLessThanOrEqual(viewport.clientWidth)

  const safety = page.locator('.config-safety-card')
  await expect(safety).toBeVisible()
  const safetyBox = await safety.boundingBox()
  expect(safetyBox?.width).toBeGreaterThan(320)

  const firstStep = await safety.locator('.config-safety-step').nth(0).boundingBox()
  const secondStep = await safety.locator('.config-safety-step').nth(1).boundingBox()
  expect(secondStep?.y).toBeGreaterThan((firstStep?.y ?? 0) + 20)
})

test('config safety shows read-only collector caveat and no enabled push CTA', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, { accepts_remote_config: false })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const safety = page.locator('.config-safety-card')
  await expect(safety).toBeVisible()
  await expect(safety).toContainText('Read-only collector')
  await expect(safety).toContainText(
    'Push is unavailable because this collector only reports config.',
  )
  await expect(safety).not.toContainText('Push validated config')
})

test('config safety is hidden for SDK workload detail pages', async ({ loggedInPage: page }) => {
  await mockWorkload(page, {
    type: 'sdk',
    active_config_id: undefined,
    accepts_remote_config: false,
    available_components: undefined,
    labels: { 'service.name': 'demo-app' },
  })
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('.config-safety-card')).toHaveCount(0)
})

test('validate exposes errors and blocks push', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: false,
    errors: [
      {
        code: 'undefined_component',
        message: 'pipeline "traces" references exporter "nope"',
        path: 'service.pipelines.traces.exporters[0]',
      },
    ],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type('bad: yaml')
  await page.getByRole('button', { name: 'Validate for this collector' }).click()

  await expect(page.locator('.validation-errors')).toContainText('undefined_component')
  // Push stays disabled
  await expect(page.getByRole('button', { name: 'Push' })).toBeDisabled()
})

test('valid config unlocks push button', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type(' # touched')
  await page.getByRole('button', { name: 'Validate for this collector' }).click()

  await expect(page.locator('.validation-ok')).toContainText('valid')
  await expect(page.getByRole('button', { name: 'Push' })).toBeEnabled()
})

test('validation details show separated static component and runtime checks', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: true,
    overall_status: 'passed',
    summary: 'All validation checks passed.',
    target_collector_version: '0.150.1',
    checks: VALIDATION_CHECKS_SUCCESS,
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.getByRole('button', { name: 'Validate for this collector' }).click()

  const details = page.locator('.validation-details')
  await expect(details).toContainText('Validation passed')
  await expect(details).toContainText('Target 0.150.1')
  await expect(details).toContainText('otelcol 0.150.1')
  await expect(details.locator('.validation-check-card')).toHaveCount(4)
  await expect(details.locator('.validation-check-card').nth(0)).toContainText('YAML syntax')
  await expect(details.locator('.validation-check-card').nth(2)).toContainText(
    'Components available on workload',
  )
  await expect(details.locator('.validation-check-card').nth(3)).toContainText(
    'Collector runtime validation',
  )
})

test('validation details separate non-blocking warnings from blocking errors', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: false,
    overall_status: 'failed',
    summary: 'One blocking validation check failed.',
    target_collector_version: '0.150.1',
    errors: [
      {
        code: 'undefined_component',
        message: 'pipeline "traces" references exporter "nope"',
        path: 'service.pipelines.traces.exporters[0]',
        check_id: 'collector_structure',
      },
    ],
    warnings: [
      {
        code: 'otelcol_version_mismatch',
        severity: 'warning',
        message: 'Runtime validation used otelcol 0.149.0 while target version is 0.150.1.',
        check_id: 'otelcol_runtime',
      },
    ],
    checks: [
      VALIDATION_CHECKS_SUCCESS[0],
      {
        id: 'collector_structure',
        label: 'Collector structure',
        source: 'server.structure',
        status: 'failed',
        required: true,
        messages: [
          {
            code: 'undefined_component',
            severity: 'error',
            message: 'pipeline "traces" references exporter "nope"',
            path: 'service.pipelines.traces.exporters[0]',
          },
        ],
      },
      {
        id: 'otelcol_runtime',
        label: 'Collector runtime validation',
        source: 'otelcol.binary',
        status: 'warning',
        required: false,
        metadata: { binary_version: '0.149.0', target_version: '0.150.1' },
        messages: [
          {
            code: 'otelcol_version_mismatch',
            severity: 'warning',
            message: 'Runtime validation used otelcol 0.149.0 while target version is 0.150.1.',
          },
        ],
      },
    ],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.getByRole('button', { name: 'Validate for this collector' }).click()

  const details = page.locator('.validation-details')
  await expect(details).toContainText('Blocking errors')
  await expect(details).toContainText('Warnings')
  await expect(details.locator('.validation-check-card-failed')).toContainText('Required')
  await expect(details.locator('.validation-check-card-warning')).toContainText('Advisory')
  await expect(details.locator('.validation-check-card-failed')).toContainText(
    'undefined_component',
  )
  await expect(details.locator('.validation-check-card-warning')).toContainText(
    'otelcol_version_mismatch',
  )
  await expect(page.getByRole('button', { name: 'Push' })).toBeDisabled()
})

test('validation details explain when otelcol runtime check is skipped', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: true,
    overall_status: 'warning',
    summary: 'Static validation passed; runtime validation skipped.',
    target_collector_version: '0.150.1',
    warnings: [
      {
        code: 'otelcol_not_found',
        severity: 'warning',
        message: 'otelcol binary "otelcol" was not found on the server.',
        check_id: 'otelcol_runtime',
      },
    ],
    checks: [
      ...VALIDATION_CHECKS_SUCCESS.slice(0, 3),
      {
        id: 'otelcol_runtime',
        label: 'Collector runtime validation',
        source: 'otelcol.binary',
        status: 'skipped',
        required: false,
        metadata: { target_version: '0.150.1' },
        messages: [
          {
            code: 'otelcol_not_found',
            severity: 'warning',
            message: 'otelcol binary "otelcol" was not found on the server.',
          },
        ],
      },
    ],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.getByRole('button', { name: 'Validate for this collector' }).click()

  const details = page.locator('.validation-details')
  await expect(details).toContainText('Validation passed with warnings')
  await expect(details).toContainText('Skipped')
  await expect(details).toContainText('otelcol binary "otelcol" was not found on the server.')
  await expect(page.getByRole('button', { name: 'Push' })).toBeEnabled()
})

test('push timeline renders aggregate progress and per-instance details', async ({
  loggedInPage: page,
}) => {
  const submittedAt = '2026-06-30T10:12:00Z'
  const sentAt = '2026-06-30T10:12:01Z'
  await mockWorkload(page, {
    current_config_push: {
      push_id: 'push_01JVISIBLE',
      workload_id: WORKLOAD_ID,
      config_id: 'feedfacefeedface',
      config_hash: 'feedfacefeedface',
      status: 'applying',
      submitted_at: submittedAt,
      sent_at: sentAt,
      updated_at: '2026-06-30T10:12:05Z',
      target_count: 3,
      applied_count: 1,
      failed_count: 0,
      pending_count: 2,
      timeline: [
        { state: 'submitted', at: submittedAt, terminal: false },
        { state: 'sent', at: sentAt, terminal: false },
        { state: 'applying', at: '2026-06-30T10:12:05Z', terminal: false },
      ],
      instance_statuses: [
        {
          instance_uid: 'inst-a',
          pod_name: 'otel-a',
          required: true,
          status: 'applied',
          config_hash: 'feedfacefeedface',
          updated_at: '2026-06-30T10:12:06Z',
        },
        {
          instance_uid: 'inst-b',
          pod_name: 'otel-b',
          required: true,
          status: 'applying',
          config_hash: 'feedfacefeedface',
          updated_at: '2026-06-30T10:12:05Z',
        },
        {
          instance_uid: 'inst-c',
          pod_name: 'otel-c',
          required: true,
          status: 'no_status',
          config_hash: 'feedfacefeedface',
          error_cause: 'apply_timeout',
          error_message: 'No OpAMP status after 30s',
          timed_out: true,
        },
      ],
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const panel = page.locator('.push-status-panel')
  await expect(panel).toContainText('Applying')
  await expect(panel).toContainText('1/3 applied')
  await expect(panel).toContainText('2 pending')
  await expect(panel.locator('.push-timeline-step')).toContainText([
    'Validated',
    'Submitted',
    'Sent via OpAMP',
    'Applying',
    'Applied',
  ])

  await page.getByRole('button', { name: 'View instance details' }).click()
  await expect(page.locator('.push-instance-table tbody tr')).toHaveCount(3)
  await expect(page.locator('.push-instance-table')).toContainText('inst-a')
  await expect(page.locator('.push-timeout-banner')).toContainText('No OpAMP status after 30s')
  await expect(page.locator('.push-instance-table')).toContainText('otel-c')
  await expect(page.locator('.push-instance-table')).toContainText('No status yet')
  await expect(page.locator('.push-instance-table')).toContainText('No OpAMP status after 30s')
})

test('push status shows the explicit OpAMP timeout message from the API', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, {
    current_config_push: {
      push_id: 'push_timeout',
      workload_id: WORKLOAD_ID,
      config_id: 'deadbeefdeadbeef',
      config_hash: 'deadbeefdeadbeef',
      status: 'sent',
      submitted_at: '2026-06-30T10:12:00Z',
      sent_at: '2026-06-30T10:12:01Z',
      updated_at: '2026-06-30T10:12:30Z',
      timed_out_waiting_for_opamp_status: true,
      timeout_message: 'No OpAMP status after 30s',
      target_count: 1,
      applied_count: 0,
      failed_count: 0,
      pending_count: 1,
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('.push-timeout-banner')).toContainText('No OpAMP status after 30s')
  await expect(page.locator('.push-timeout-banner')).toContainText(
    'otel-magnify sent the config but has not received an OpAMP status from the workload yet.',
  )
})

test('remote config errors are grouped by cause with affected instances', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, {
    current_config_push: {
      push_id: 'push_failed',
      workload_id: WORKLOAD_ID,
      config_id: 'badbadbadbad',
      config_hash: 'badbadbadbad',
      status: 'failed',
      submitted_at: '2026-06-30T10:12:00Z',
      updated_at: '2026-06-30T10:12:08Z',
      target_count: 3,
      applied_count: 0,
      failed_count: 3,
      pending_count: 0,
      error_message:
        'collector failed with authorization: Bearer raw-to...oken and endpoint https://collector.internal/v1/traces',
      instance_statuses: [
        {
          instance_uid: 'inst-a',
          required: true,
          status: 'failed',
          error_cause: 'collector_validation',
          error_message: 'password: raw-instance-password',
        },
      ],
      error_groups: [
        {
          cause: 'collector_validation',
          title: 'Collector rejected the config',
          severity: 'high',
          count: 2,
          affected_instances: ['inst-a', 'inst-b'],
          sample_message:
            "unknown exporter 'othttp' token=raw-group-secret endpoint https://collector.internal/v1/traces",
          sample_path: 'service.pipelines.traces.exporters[0]',
          config_hash: 'badbadbadbad',
          retryable: true,
        },
        {
          cause: 'opamp_send_failed',
          title: 'OpAMP delivery failed',
          severity: 'high',
          count: 1,
          affected_instances: ['inst-c'],
          sample_message: 'connection lost',
          config_hash: 'badbadbadbad',
          retryable: true,
        },
      ],
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const errors = page.locator('.push-error-groups')
  await expect(errors).toContainText('Collector rejected the config')
  await expect(errors).toContainText('2 instances')
  await expect(errors).toContainText("unknown exporter 'othttp'")
  await expect(errors).toContainText('redacted credential; redacted endpoint')
  await expect(errors).toContainText('Severity: high')
  await expect(errors).toContainText('collector_validation')
  await expect(errors).toContainText('inst-a')
  await expect(errors).toContainText('OpAMP delivery failed')
  await page.getByRole('button', { name: 'View instance details' }).click()
  await expect(page.locator('body')).not.toContainText('raw-top-secret-token')
  await expect(page.locator('body')).not.toContainText('raw-group-secret')
  await expect(page.locator('body')).not.toContainText('raw-instance-password')
  await expect(page.locator('body')).not.toContainText('https://collector.internal/v1/traces')
})

test('push failed shows error banner and preserves draft', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config`, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'config push initiated', config_hash: 'deadbeefdeadbeef' }),
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type(' # touched')
  await page.getByRole('button', { name: 'Validate for this collector' }).click()
  await expect(page.locator('.validation-ok')).toBeVisible()
  await page.getByRole('button', { name: 'Push' }).click()

  // Simulate FAILED WS event
  await page.evaluate(() => {
    const evt = {
      type: 'workload_config_status',
      workload_id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
      status: {
        status: 'failed',
        config_hash: 'deadbeefdeadbeef',
        error_message: "unknown exporter 'othttp'",
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  })

  await expect(page.locator('.push-banner-failed')).toContainText("unknown exporter 'othttp'")
  // Draft preserved — editor still shows our addition
  await expect(page.locator('.cm-content').first()).toContainText('# touched')
})

test('push and rollback banners redact sensitive remote error text', () => {
  const rawError =
    'collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces'

  const rendered = safeRemoteErrorText(rawError)

  expect(rendered).toBe('redacted credential; redacted endpoint; redacted tenant')
  expect(rendered).not.toContain('SECRET_TOKEN')
  expect(rendered).not.toContain('authorization=Bearer')
  expect(rendered).not.toContain('abc123')
  expect(rendered).not.toContain('super-secret')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('/v1/traces')
})

test('rollback reasons keep a safe short cause while redacting legacy remote details', () => {
  const rawReason =
    "collector rejected config for tenant-a.internal: unknown exporter 'othttp' SECRET_TOKEN=abc123 authorization=Bearer super-secret exporters:\n  otlphttp:\n    endpoint: https://tenant-a.internal:4318/v1/traces"

  const rendered = safeRemoteErrorText(rawReason)

  expect(rendered).toBe(
    "unknown exporter 'othttp' — redacted credential; redacted endpoint; redacted tenant; configuration error",
  )
  expect(rendered).not.toContain('SECRET_TOKEN')
  expect(rendered).not.toContain('authorization=Bearer')
  expect(rendered).not.toContain('abc123')
  expect(rendered).not.toContain('super-secret')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('exporters:')
  expect(rendered).not.toContain('otlphttp')
})

test('rollback reasons do not preserve sensitive unknown component names as causes', () => {
  const rawReason =
    "unknown exporter 'tenant-a.internal' authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"

  const rendered = safeRemoteErrorText(rawReason)

  expect(rendered).toBe('redacted credential; redacted endpoint; redacted tenant')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('authorization=Bearer')
  expect(rendered).not.toContain('super-secret')
  expect(rendered).not.toContain('/v1/traces')
})

test('rollback reasons map config snippets and tenant identifiers to concise safe labels', () => {
  const rawReason =
    'tenant-a failed rollback validation: service:\n  pipelines:\n    traces:\n      exporters: [otlp]\nexporters:\n  otlp:\n    endpoint: tenant-a.internal:4317'

  const rendered = safeRemoteErrorText(rawReason)

  expect(rendered).toBe('redacted endpoint; redacted tenant; configuration error')
  expect(rendered).not.toContain('tenant-a')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('service:')
  expect(rendered).not.toContain('exporters:')
  expect(rendered).not.toContain('otlp')
})

test('push applied closes edit mode, clears draft, shows applied banner', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config`, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'config push initiated', config_hash: 'feedfacefeedface' }),
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type(' # applied-flow')
  await page.getByRole('button', { name: 'Validate for this collector' }).click()
  await expect(page.locator('.validation-ok')).toBeVisible()
  await page.getByRole('button', { name: 'Push' }).click()

  // While the push is pending, the Push button switches to Applying...
  await expect(page.getByRole('button', { name: 'Applying...' })).toBeVisible()

  // Simulate APPLIED WS event matching our pending hash
  await page.evaluate(() => {
    const evt = {
      type: 'workload_config_status',
      workload_id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
      status: {
        status: 'applied',
        config_hash: 'feedfacefeedface',
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  })

  // Edit mode closed — the editor toolbar buttons are gone, the Edit entry-point is back
  await expect(page.getByRole('button', { name: 'Push' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Validate for this collector' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()

  // Banner reflects the applied status
  await expect(page.locator('.push-banner-applied')).toBeVisible()
  await expect(page.locator('.push-banner-applied')).toContainText('feedface')

  // Re-entering edit mode starts from the active config (not the previous draft)
  await page.getByRole('button', { name: 'Edit' }).click()
  await expect(page.locator('.cm-content').first()).not.toContainText('# applied-flow')
})

test('diff tab shows two editor panels', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type('a: 2\n')
  await page.getByRole('button', { name: 'Diff' }).click()

  await expect(page.locator('.cm-mergeView .cm-editor')).toHaveCount(2)
})

test('history refreshes when WS workload_config_status arrives from another session', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')

  let rows: unknown[] = []
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs`, (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(rows),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  // No history yet: table not rendered
  await expect(page.locator('.history-table')).toHaveCount(0)

  // Simulate a config applied event from another session (not our local push)
  rows = [
    {
      workload_id: WORKLOAD_ID,
      config_id: 'ccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
      applied_at: new Date().toISOString(),
      status: 'applied',
      pushed_by: 'other@user',
      error_message: '',
    },
  ]
  await page.evaluate((workloadId) => {
    const evt = {
      type: 'workload_config_status',
      workload_id: workloadId,
      status: {
        status: 'applied',
        config_hash: 'cccccccc',
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  }, WORKLOAD_ID)

  // Table appears because the query was invalidated and refetched
  await expect(page.locator('.history-table tbody tr')).toHaveCount(1)
  await expect(page.locator('.history-table')).toContainText('other@user')
})

test('history table renders with rollback action', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [
    {
      workload_id: WORKLOAD_ID,
      config_id: '1111111111111111',
      applied_at: new Date().toISOString(),
      status: 'applied',
      pushed_by: 'me@x',
      content: 'old: true',
    },
    {
      workload_id: WORKLOAD_ID,
      config_id: '2222222222222222',
      applied_at: new Date().toISOString(),
      status: 'failed',
      error_message: 'boom',
      pushed_by: 'me@x',
      content: 'bad',
    },
  ])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await expect(page.locator('.history-table tbody tr')).toHaveCount(2)
  await expect(page.locator('.history-error').first()).toHaveText('')
  await expect(page.locator('.history-error').nth(1)).toContainText('boom')
  // Rollback affordance was renamed (config-versioning v0.5.0): each row with
  // recoverable content shows a "Rollback" button that opens a confirm dialog.
  await expect(page.getByRole('button', { name: 'Rollback' }).first()).toBeVisible()
})

test('YAML keys are colored via Signal Deck theme', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'key: value\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  // Wait for the editor to render
  await expect(page.locator('.cm-content')).toBeVisible()

  // Find the span that carries the color attribution (Lezer highlight uses
  // generated class names, so we match on computed style instead of class).
  const firstSpan = page.locator('.cm-line span').first()
  const color = await firstSpan.evaluate((el) => getComputedStyle(el).color)
  expect(color).toBe('rgb(212, 168, 74)')
})

function mockConfigsList(page: Page, configs: Array<{ id: string; name: string }>) {
  return page.route(`**/api/configs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(
        configs.map((c) => ({
          id: c.id,
          name: c.name,
          content: '',
          created_at: new Date().toISOString(),
          created_by: 'tester',
        })),
      ),
    }),
  )
}

function mockConfigDetail(page: Page, id: string, name: string, content: string) {
  return page.route(`**/api/configs/${id}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id,
        name,
        content,
        created_at: new Date().toISOString(),
        created_by: 'tester',
      }),
    }),
  )
}

test('selecting a saved config loads YAML into editor and switches to Diff tab', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'old: true\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])
  await mockConfigDetail(page, 'cfg-eu', 'collector-prod-eu', 'new: true\n')

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.locator('select.apply-config-select').selectOption('cfg-eu')

  // Diff tab should be the active one (workload has active_config_id)
  await expect(page.locator('.tab-active')).toHaveText('Diff')
  // Two editor panels visible (the MergeView)
  await expect(page.locator('.cm-mergeView .cm-editor')).toHaveCount(2)
  // The right-hand (newYaml) editor contains the selected config's content
  await expect(page.locator('.cm-mergeView .cm-content').nth(1)).toContainText('new: true')
})

test('apply-saved-config selector renders in supervised collector branch', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [
    { id: 'cfg-eu', name: 'collector-prod-eu' },
    { id: 'cfg-us', name: 'collector-prod-us' },
  ])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const selector = page.locator('select.apply-config-select')
  await expect(selector).toBeVisible()
  await expect(selector.locator('option')).toHaveCount(3) // placeholder + 2 configs
  await expect(selector.locator('option').nth(1)).toContainText('collector-prod-eu')
  await expect(selector.locator('option').nth(2)).toContainText('collector-prod-us')
})

test('bootstrap workload (no active config): selecting falls back to Edit tab', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, { active_config_id: undefined })
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])
  await mockConfigDetail(page, 'cfg-eu', 'collector-prod-eu', 'fresh: true\n')

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.locator('select.apply-config-select').selectOption('cfg-eu')

  // Edit tab is the only navigable one (Diff is disabled when no active_config_id)
  await expect(page.locator('.tab-active')).toHaveText('Edit')
  await expect(page.getByRole('button', { name: 'Diff' })).toBeDisabled()
  // Editor draft contains the selected config's content
  await expect(page.locator('.cm-content').first()).toContainText('fresh: true')
})

test('selector annotates the currently applied config', async ({ loggedInPage: page }) => {
  await mockWorkload(page) // active_config_id = ACTIVE_CONFIG_ID = 'abc123'
  await mockConfig(page, 'old: true\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [
    { id: 'abc123', name: 'collector-prod-eu' },
    { id: 'cfg-us', name: 'collector-prod-us' },
  ])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const eu = page.locator('select.apply-config-select option').nth(1)
  await expect(eu).toContainText('collector-prod-eu')
  await expect(eu).toContainText('(currently applied)')

  const us = page.locator('select.apply-config-select option').nth(2)
  await expect(us).toContainText('collector-prod-us')
  await expect(us).not.toContainText('(currently applied)')
})

test('empty configs list disables selector with explanatory text', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const selector = page.locator('select.apply-config-select')
  await expect(selector).toBeDisabled()
  await expect(selector).toHaveValue('')
  await expect(selector.locator('option')).toHaveCount(1)
  await expect(selector.locator('option').first()).toContainText('No saved configs')

  // Editor copy-paste flow still functional: Edit button visible
  await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
})

test('configs list fetch error shows disabled selector with retry', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await page.route('**/api/configs', (route) =>
    route.fulfill({ status: 500, body: '{"error":"boom"}' }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const selector = page.locator('select.apply-config-select')
  await expect(selector).toBeDisabled()
  await expect(selector.locator('option').first()).toContainText('Failed to load configs')

  // Editor copy-paste flow still works
  await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
})

test('selector is absent in read-only collector branch', async ({ loggedInPage: page }) => {
  await mockWorkload(page, { accepts_remote_config: false })
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('select.apply-config-select')).toHaveCount(0)
  // Read-only message still shown
  await expect(page.locator('.config-readonly-note')).toContainText('Read-only')
})

test('selector is absent for SDK workloads', async ({ loggedInPage: page }) => {
  await mockWorkload(page, {
    type: 'sdk',
    active_config_id: undefined,
    accepts_remote_config: false,
    available_components: undefined,
    labels: { 'service.name': 'demo-app' },
  })
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('select.apply-config-select')).toHaveCount(0)
  // SDK label chips visible (page shows labels in both the Labels section and the
  // Configuration section; assert at least one chip carries the expected text)
  await expect(page.locator('.label-chip').first()).toContainText('demo-app')
})

test('selecting a config overwrites in-progress draft silently (no confirm)', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'old: true\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])
  await mockConfigDetail(page, 'cfg-eu', 'collector-prod-eu', 'replaced: true\n')

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  // Enter edit mode and type something
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type('user-typed-mess: yes\n')

  // Now select a saved config
  await page.locator('select.apply-config-select').selectOption('cfg-eu')

  // The draft should now contain the saved config's content, not the typed mess.
  // Editor visible is the right-hand panel of the MergeView (Diff tab is auto-active).
  await expect(page.locator('.cm-mergeView .cm-content').nth(1)).toContainText('replaced: true')
  await expect(page.locator('.cm-mergeView .cm-content').nth(1)).not.toContainText(
    'user-typed-mess',
  )
})
