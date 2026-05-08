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

  let labelPosted: { hash: string; body: unknown } | null = null
  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/configs/*/label`,
    async (route, request) => {
      const url = new URL(request.url())
      const segments = url.pathname.split('/')
      const hash = segments[segments.length - 2]
      labelPosted = { hash, body: request.postDataJSON() }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ label: (request.postDataJSON() as { label: string }).label }),
      })
    },
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
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

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Compare revisions' }).click()

  await expect(page.getByRole('dialog', { name: 'Compare two revisions' })).toBeVisible()
  // The MergeView from CodeMirror renders two .cm-content panes side-by-side.
  await expect(page.locator('.config-diff-view .cm-content')).toHaveCount(2)
})

test('rollback opens confirm dialog and posts to /rollback', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)

  let rollbackHash: string | null = null
  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/configs/*/rollback`,
    async (route, request) => {
      const url = new URL(request.url())
      const segments = url.pathname.split('/')
      rollbackHash = segments[segments.length - 2]
      await route.fulfill({
        status: 202,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'rollback initiated', config_hash: rollbackHash }),
      })
    },
  )

  // Pre-emptively block the legacy POST /api/workloads/{id}/config so the
  // test fails loudly if PushHistoryTable still calls the old endpoint.
  let legacyPushHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config`, async (route) => {
    legacyPushHit = true
    await route.fulfill({ status: 500, body: 'should not be called' })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  // Click Rollback on the older "stable-2026-04" row.
  const olderRow = page.locator('.history-table tbody tr').nth(1)
  await olderRow.getByRole('button', { name: 'Rollback' }).click()

  // Confirm dialog appears with the abbreviated hash.
  await expect(page.getByText('Rollback to past revision?')).toBeVisible()
  const dialog = page.locator('.modal').filter({ hasText: 'Rollback to past revision?' })
  await dialog.getByRole('button', { name: 'Rollback', exact: true }).click()

  await expect.poll(() => rollbackHash).not.toBeNull()
  expect(rollbackHash).toBe(HASH_OLD)
  expect(legacyPushHit).toBe(false)
})
