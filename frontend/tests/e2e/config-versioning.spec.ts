import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

const WORKLOAD_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const ACTIVE_CONFIG_ID = 'hash-current'

const HASH_OLD = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
const HASH_NEW = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'

const YAML_OLD = `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
const YAML_NEW = `${YAML_OLD}# revision-new\n`

function mockWorkload(page: Page) {
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
        available_components: { components: { receivers: ['otlp'], exporters: ['logging'] } },
      }),
    }),
  )
}

function mockActiveConfig(page: Page) {
  return page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: ACTIVE_CONFIG_ID,
        name: 'current',
        content: YAML_NEW,
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

function mockConfigsList(page: Page) {
  return page.route(`**/api/configs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: ACTIVE_CONFIG_ID,
          name: 'current',
          content: YAML_NEW,
          created_at: new Date().toISOString(),
          created_by: 'test',
        },
      ]),
    }),
  )
}

async function gotoWorkloadDetail(page: Page) {
  // Seed auth on the app origin through a stable document before hitting the
  // protected route; this keeps the spec independent from login submission.
  await page.route('**/api/auth/methods', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        methods: [
          {
            id: 'password',
            type: 'password',
            display_name: 'Email + password',
            login_url: '/api/auth/login',
          },
        ],
      }),
    }),
  )
  await page.route('**/api/me', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: 'u-test',
        email: 'test@example.com',
        groups: [
          {
            id: 'grp_system_viewer',
            name: 'viewer',
            role: 'viewer',
            is_system: true,
            created_at: new Date().toISOString(),
          },
        ],
        preferences: {
          user_id: 'u-test',
          theme: 'system',
          language: 'en',
          updated_at: new Date().toISOString(),
        },
      }),
    }),
  )
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  await page.evaluate(() => localStorage.setItem('token', 'test.token.stub'))
  await page.goto(`/workloads/${WORKLOAD_ID}`)
}

const BASE_HISTORY = [
  {
    workload_id: WORKLOAD_ID,
    config_id: HASH_NEW,
    applied_at: '2026-05-08T12:00:00Z',
    status: 'applied',
    pushed_by: 'admin@e2e.local',
    content: YAML_NEW,
  },
  {
    workload_id: WORKLOAD_ID,
    config_id: HASH_OLD,
    applied_at: '2026-05-01T09:00:00Z',
    status: 'applied',
    pushed_by: 'admin@e2e.local',
    content: YAML_OLD,
    label: 'stable-2026-04',
  },
]

test('label can be set inline via double-click', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  let labelPosted: { hash: string; body: unknown } | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/*/label`, async (route, request) => {
    const url = new URL(request.url())
    const segments = url.pathname.split('/')
    const hash = segments[segments.length - 2]
    labelPosted = { hash, body: request.postDataJSON() }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ label: (request.postDataJSON() as { label: string }).label }),
    })
  })

  await gotoWorkloadDetail(page)
  await expect(page.getByText('Push history')).toBeVisible()

  // The newest row has no label yet — double-click its label cell to edit.
  const newestRow = page.locator('.history-table tbody tr').first()
  await newestRow.locator('.history-label').dblclick()
  const input = newestRow.locator('.history-label input')
  await input.fill('canary')
  await input.blur()

  await expect.poll(() => labelPosted).not.toBeNull()
  expect(labelPosted!.hash).toBe(HASH_NEW)
  expect(labelPosted!.body).toEqual({ label: 'canary' })
})

test('compare dialog diffs two arbitrary revisions', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  // The dialog fetches each revision by hash on demand.
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/${HASH_OLD}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...BASE_HISTORY[1],
        content: YAML_OLD,
      }),
    }),
  )
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/${HASH_NEW}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...BASE_HISTORY[0],
        content: YAML_NEW,
      }),
    }),
  )

  await gotoWorkloadDetail(page)
  await page.getByRole('button', { name: 'Compare revisions' }).click()

  await expect(page.getByRole('dialog', { name: 'Compare two revisions' })).toBeVisible()
  // The MergeView from CodeMirror renders two .cm-content panes side-by-side.
  await expect(page.locator('.config-diff-view .cm-content')).toHaveCount(2)
})

function buildRollbackPrepare(overrides: Record<string, unknown> = {}) {
  return {
    schema_version: 'guided-rollback-prepare.v1',
    workload: {
      id: WORKLOAD_ID,
      display_name: 'test-collector',
      type: 'collector',
      status: 'connected',
      accepts_remote_config: true,
      active_config_hash: HASH_NEW,
      remote_config_status: {
        status: 'applied',
        config_hash: HASH_NEW,
        updated_at: '2026-05-08T12:05:00Z',
      },
    },
    target_ref: {
      selector: 'hash',
      source: 'push_history_row',
      workload_id: WORKLOAD_ID,
      target_hash: HASH_OLD,
      known_good: true,
      known_good_source: 'label_convention',
    },
    current_config: {
      hash: HASH_NEW,
      content_available: true,
      content: YAML_NEW,
      content_sha256: HASH_NEW,
      source: 'active_config',
      metadata: { known_good: false, active_config_hash: HASH_NEW },
    },
    target_config: {
      hash: HASH_OLD,
      content_available: true,
      content: YAML_OLD,
      content_sha256: HASH_OLD,
      source: 'history',
      metadata: {
        label: 'stable-2026-04',
        known_good: true,
        applied_at: '2026-05-01T09:00:00Z',
        pushed_by: 'admin@e2e.local',
        previous_status: 'applied',
      },
    },
    diff: {
      status: 'available',
      direction: 'current_to_target',
      computation: 'backend_raw',
      base_hash: HASH_NEW,
      target_hash: HASH_OLD,
      raw_diff: {
        format: 'unified',
        language: 'yaml',
        base_label: 'Current bbbbbbbb',
        target_label: 'Rollback target aaaaaaaa',
        text: '--- current\n+++ target\n@@\n-# revision-new',
        truncated: false,
      },
      inputs: {
        current_content_available: true,
        target_content_available: true,
        current_yaml: YAML_NEW,
        target_yaml: YAML_OLD,
      },
    },
    validation: {
      status: 'valid_with_warnings',
      valid: true,
      can_confirm: true,
      checked_at: '2026-05-08T12:06:00Z',
      validator_version: 'light-validator.v1',
      inputs: {
        workload_id: WORKLOAD_ID,
        workload_type: 'collector',
        accepts_remote_config: true,
        target_hash: HASH_OLD,
      },
      findings: [
        {
          code: 'noop_target_equals_current',
          severity: 'warning',
          message: 'This target matches the current config.',
          blocking: false,
          source: 'target',
        },
      ],
      unavailable_components: [
        {
          category: 'exporters',
          component_id: 'datadog/main',
          component_type: 'datadog',
          path: 'service.pipelines.traces.exporters[0]',
          available: ['otlp', 'logging'],
          blocking: true,
        },
      ],
    },
    action: {
      can_submit: true,
      submit_url: `/api/workloads/${WORKLOAD_ID}/configs/${HASH_OLD}/rollback`,
      method: 'POST',
      requires_confirmation: true,
      confirmation_label: 'Confirm rollback with warnings',
      blocking_reasons: [],
      warnings: [
        {
          code: 'noop_target_equals_current',
          severity: 'warning',
          message: 'This target matches the current config.',
          blocking: false,
          source: 'target',
        },
      ],
    },
    status_context: {
      initial_remote_config_status: {
        status: 'applied',
        config_hash: HASH_NEW,
        updated_at: '2026-05-08T12:05:00Z',
      },
      timeout_seconds: 30,
    },
    ...overrides,
  }
}

test('guided rollback previews diff, warnings, confirmation, and final status report', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/prepare?target_hash=${HASH_OLD}`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(buildRollbackPrepare()),
      }),
  )

  let rollbackHash: string | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/*/rollback`, async (route, request) => {
    const url = new URL(request.url())
    const segments = url.pathname.split('/')
    rollbackHash = segments[segments.length - 2]
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 'guided-rollback-action.v1',
        request_id: 'rb-test-1',
        status: 'accepted',
        message: 'rollback initiated',
        workload_id: WORKLOAD_ID,
        target_hash: rollbackHash,
        config_hash: rollbackHash,
        status_url: `/api/workloads/${WORKLOAD_ID}/rollback/status?request_id=rb-test-1`,
        timeout_seconds: 30,
      }),
    })
  })
  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/status?request_id=rb-test-1`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'guided-rollback-status.v1',
          request_id: 'rb-test-1',
          workload_id: WORKLOAD_ID,
          target_hash: HASH_OLD,
          target_label: 'stable-2026-04',
          request_status: 'accepted',
          apply_status: 'applied',
          terminal: true,
          terminal_status: 'applied',
          started_at: '2026-05-08T12:07:00Z',
          updated_at: '2026-05-08T12:07:04Z',
          elapsed_ms: 4100,
          timeout_seconds: 30,
          timed_out: false,
          remote_config_status: {
            status: 'applied',
            config_hash: HASH_OLD,
            updated_at: '2026-05-08T12:07:04Z',
          },
        }),
      }),
  )

  let legacyPushHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config`, async (route) => {
    legacyPushHit = true
    await route.fulfill({ status: 500, body: 'should not be called' })
  })

  await gotoWorkloadDetail(page)
  const olderRow = page.locator('.history-table tbody tr').nth(1)
  await olderRow.getByRole('button', { name: 'Rollback' }).click()

  const dialog = page.getByRole('dialog', { name: 'Guided rollback' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('heading', { name: 'Current config' })).toBeVisible()
  await expect(dialog.getByText(HASH_NEW.slice(0, 12), { exact: true })).toBeVisible()
  await expect(dialog.getByText('stable-2026-04')).toBeVisible()
  await expect(dialog.getByText('admin@e2e.local')).toBeVisible()
  await expect(dialog.getByText('Validation passed with warnings')).toBeVisible()
  await expect(dialog.getByText('datadog/main')).toBeVisible()
  await expect(dialog.getByText('--- current')).toBeVisible()

  const confirmButton = dialog.getByRole('button', { name: 'Confirm rollback with warnings' })
  await expect(confirmButton).toBeDisabled()
  await dialog.getByLabel('I understand this will replace the collector remote config').check()
  await confirmButton.click()

  await expect.poll(() => rollbackHash).toBe(HASH_OLD)
  expect(legacyPushHit).toBe(false)
  await expect(dialog.getByText('Rollback applied')).toBeVisible()
  await expect(dialog.getByText('Duration: 4.1s')).toBeVisible()
  await expect(dialog.getByText(`Remote config: applied ${HASH_OLD.slice(0, 12)}`)).toBeVisible()
})

test('guided rollback blocks invalid targets and reports prepare failures', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)

  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/prepare?target_hash=${HASH_OLD}`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          buildRollbackPrepare({
            validation: {
              status: 'invalid',
              valid: false,
              can_confirm: false,
              checked_at: '2026-05-08T12:06:00Z',
              validator_version: 'light-validator.v1',
              inputs: {
                workload_id: WORKLOAD_ID,
                workload_type: 'collector',
                accepts_remote_config: true,
                target_hash: HASH_OLD,
              },
              findings: [
                {
                  code: 'component_not_installed',
                  severity: 'error',
                  message: 'exporter type "datadog" is not installed on the target agent',
                  path: 'service.pipelines.traces.exporters[0]',
                  blocking: true,
                  source: 'capabilities',
                },
              ],
              unavailable_components: [],
            },
            action: {
              can_submit: false,
              submit_url: `/api/workloads/${WORKLOAD_ID}/configs/${HASH_OLD}/rollback`,
              method: 'POST',
              requires_confirmation: true,
              confirmation_label: 'Confirm rollback',
              blocking_reasons: [
                {
                  code: 'component_not_installed',
                  severity: 'error',
                  message: 'exporter type "datadog" is not installed on the target agent',
                  path: 'service.pipelines.traces.exporters[0]',
                  blocking: true,
                  source: 'capabilities',
                },
              ],
              warnings: [],
            },
          }),
        ),
      }),
  )

  await gotoWorkloadDetail(page)
  await page
    .locator('.history-table tbody tr')
    .nth(1)
    .getByRole('button', { name: 'Rollback' })
    .click()

  const dialog = page.getByRole('dialog', { name: 'Guided rollback' })
  await expect(dialog.getByText('Validation failed')).toBeVisible()
  await expect(
    dialog.getByText('exporter type "datadog" is not installed on the target agent'),
  ).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Confirm rollback' })).toBeDisabled()

  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/prepare?target_hash=${HASH_OLD}`,
    (route) =>
      route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'OpAMP unavailable' }),
      }),
  )
  await gotoWorkloadDetail(page)
  await page
    .locator('.history-table tbody tr')
    .nth(1)
    .getByRole('button', { name: 'Rollback' })
    .click()
  await expect(page.getByText('Failed to load rollback preview: OpAMP unavailable')).toBeVisible()
})
