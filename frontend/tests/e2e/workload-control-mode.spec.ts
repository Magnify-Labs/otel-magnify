import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

const SUPERVISED_ID = 'ssssssss-ssss-ssss-ssss-ssssssssssss'
const READONLY_ID   = 'rrrrrrrr-rrrr-rrrr-rrrr-rrrrrrrrrrrr'
const CONFIG_ID     = 'cfg1'

function baseWorkload(id: string, name: string, acceptsRemoteConfig: boolean) {
  return {
    id,
    fingerprint_source: 'k8s',
    fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name },
    display_name: name,
    type: 'collector',
    version: '0.98.0',
    status: 'connected',
    last_seen_at: new Date().toISOString(),
    labels: {},
    active_config_id: CONFIG_ID,
    accepts_remote_config: acceptsRemoteConfig,
    available_components: { components: { receivers: ['otlp'], exporters: ['logging'] } },
  }
}

async function mockList(page: Page, extra: Record<string, unknown>[] = []) {
  await page.route('**/api/workloads/version-intelligence*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 'fleet-version-intelligence.v1',
        recommended_version: '0.100.0',
        version_matrix: [],
        collectors_below_recommended: [],
        unsupported_config_components: [],
        invalid_versions: [],
        recommendations: [],
      }),
    }),
  )
  await page.route('**/api/config-safety/drift*', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ total: 0, drifted: 0, current: 0, unknown: 0, items: [] }),
    }),
  )
  await page.route('**/api/pushes/activity*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
  return page.route('**/api/workloads*', async (route) => {
    const url = route.request().url()
    // Only stub the collection endpoint, not child resources like /:id or /:id/*
    if (/\/api\/workloads(\?|$)/.test(url) || url.endsWith('/api/workloads')) {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          baseWorkload(SUPERVISED_ID, 'collector-supervised', true),
          baseWorkload(READONLY_ID,   'collector-readonly',   false),
          ...extra,
        ]),
      })
      return
    }
    await route.continue()
  })
}

async function mockWorkload(page: Page, id: string, acceptsRemoteConfig: boolean) {
  const name = id === SUPERVISED_ID ? 'collector-supervised' : 'collector-readonly'
  await page.route('**/api/configs', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
  await page.route(`**/api/workloads/${id}/config/plan*`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 'config_application_plan.v1',
        workload_id: id,
        config_hash: 'test-hash',
        summary: {
          target_count: 1,
          collector_target_count: 1,
          remote_config_capable_count: acceptsRemoteConfig ? 1 : 0,
          read_only_count: acceptsRemoteConfig ? 0 : 1,
          validation_ok_count: acceptsRemoteConfig ? 1 : 0,
          validation_failed_count: 0,
          components_missing_count: 0,
          high_risk_change_count: 0,
          excluded_count: acceptsRemoteConfig ? 0 : 1,
        },
        targets: [],
        hard_failures: acceptsRemoteConfig ? [] : ['workload does not accept remote config'],
        can_push: acceptsRemoteConfig,
        apply_allowed: acceptsRemoteConfig,
        export: {
          supported: true,
          formats: ['json', 'markdown'],
          json_endpoint: `/api/workloads/${id}/config/plan/export?format=json`,
          markdown_endpoint: `/api/workloads/${id}/config/plan/export?format=markdown`,
          persisted_rollout: 'not_persisted',
        },
      }),
    }),
  )
  await page.route(`**/api/workloads/${id}/instances*`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
  await page.route(`**/api/workloads/${id}/events/stats*`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ total: 0, by_severity: {}, by_type: {} }),
    }),
  )
  await page.route(`**/api/workloads/${id}/events*`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
  await page.route(`**/api/workloads/${id}/known-good*`, (route) =>
    route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) }),
  )
  return page.route(`**/api/workloads/${id}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(baseWorkload(id, name, acceptsRemoteConfig)),
    }),
  )
}

async function mockConfig(page: Page) {
  await page.route('**/api/configs', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
  return page.route(`**/api/configs/${CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: CONFIG_ID,
        name: 'current',
        content: 'receivers:\n  otlp: {}\n',
        created_at: new Date().toISOString(),
        created_by: 'agent-reported',
      }),
    }),
  )
}

function mockHistory(page: Page, id: string) {
  return page.route(`**/api/workloads/${id}/configs`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
}

function mockAlerts(page: Page) {
  return page.route('**/api/alerts**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
}

function mockPushActivity(page: Page) {
  return page.route('**/api/pushes/activity*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
}

test.beforeEach(async ({ loggedInPage: page }) => {
  await mockAlerts(page)
})

test('Inventory shows supervised pill only on supervised collectors', async ({ loggedInPage: page }) => {
  await mockList(page)
  await page.goto('/inventory')

  const supervisedCard = page.locator('.workload-card', { hasText: 'collector-supervised' })
  const readonlyCard   = page.locator('.workload-card', { hasText: 'collector-readonly' })

  await expect(supervisedCard.locator('.agent-supervised-pill')).toBeVisible()
  await expect(readonlyCard.locator('.agent-supervised-pill')).toHaveCount(0)
})

test('Inventory control filter narrows to supervised or read-only', async ({ loggedInPage: page }) => {
  await mockList(page)

  await page.goto('/inventory?control=supervised')
  await expect(page.locator('.workload-card')).toHaveCount(1)
  await expect(page.locator('.workload-card')).toContainText('collector-supervised')

  await page.goto('/inventory?control=readonly')
  await expect(page.locator('.workload-card')).toHaveCount(1)
  await expect(page.locator('.workload-card')).toContainText('collector-readonly')
})

test('Read-only collector detail page hides Edit and shows note', async ({ loggedInPage: page }) => {
  await mockWorkload(page, READONLY_ID, false)
  await mockConfig(page)
  await mockHistory(page, READONLY_ID)

  await page.goto(`/workloads/${READONLY_ID}`)

  await expect(page.locator('.detail-cell-label', { hasText: 'Control' })).toBeVisible()
  await expect(page.locator('.detail-cell-value', { hasText: 'Read-only' })).toBeVisible()
  await expect(page.locator('.config-readonly-note')).toContainText('OpAMP Supervisor')
  await expect(page.locator('.config-readonly-note a')).toHaveAttribute(
    'href',
    'https://github.com/magnify-labs/otel-magnify/blob/main/docs/users/connecting-agents.md#running-a-collector-via-opamp-supervisor',
  )
  await expect(page.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Push a config' })).toHaveCount(0)
})

test('Supervised collector detail page shows Edit button and Supervised cell', async ({ loggedInPage: page }) => {
  await mockWorkload(page, SUPERVISED_ID, true)
  await mockConfig(page)
  await mockHistory(page, SUPERVISED_ID)

  await page.goto(`/workloads/${SUPERVISED_ID}`)

  await expect(page.locator('.detail-cell-value', { hasText: 'Supervised' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit' })).toBeVisible()
  await expect(page.locator('.config-readonly-note')).toHaveCount(0)
})

test('Inventory control filter excludes SDK workloads entirely', async ({ loggedInPage: page }) => {
  const SDK_ID = 'dddddddd-dddd-dddd-dddd-dddddddddddd'
  await mockList(page, [
    {
      id: SDK_ID,
      fingerprint_source: 'uid',
      fingerprint_keys: { uid: SDK_ID },
      display_name: 'sdk-service',
      type: 'sdk',
      version: '1.0.0',
      status: 'connected',
      last_seen_at: new Date().toISOString(),
      labels: {},
    },
  ])

  // Baseline: all three visible with no control filter
  await page.goto('/inventory')
  await expect(page.locator('.workload-card')).toHaveCount(3)

  // control=supervised → SDK excluded, only supervised collector shown
  await page.goto('/inventory?control=supervised')
  await expect(page.locator('.workload-card')).toHaveCount(1)
  await expect(page.locator('.workload-card')).toContainText('collector-supervised')
  await expect(page.locator('.workload-card', { hasText: 'sdk-service' })).toHaveCount(0)

  // control=readonly → SDK also excluded, only read-only collector shown
  await page.goto('/inventory?control=readonly')
  await expect(page.locator('.workload-card')).toHaveCount(1)
  await expect(page.locator('.workload-card')).toContainText('collector-readonly')
  await expect(page.locator('.workload-card', { hasText: 'sdk-service' })).toHaveCount(0)
})

test('Dashboard Supervised stat card links to filtered Inventory', async ({ loggedInPage: page }) => {
  await mockList(page)
  await mockAlerts(page)
  await mockPushActivity(page)

  await page.goto('/')
  const supervisedCard = page.locator('.stat-card', { hasText: 'Supervised' })
  await expect(supervisedCard.locator('.stat-value')).toHaveText('1')

  await supervisedCard.click()
  await expect(page).toHaveURL(/\/inventory\?control=supervised/)
})
