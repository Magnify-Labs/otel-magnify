import { test, expect } from './fixtures'

const mockWorkloads = [
  { id: 'w1', fingerprint_source: 'k8s', fingerprint_keys: {}, display_name: 'coll-a', type: 'collector', version: '0.100.0', status: 'connected',    last_seen_at: new Date().toISOString(), labels: {}, accepts_remote_config: true  },
  { id: 'w2', fingerprint_source: 'k8s', fingerprint_keys: {}, display_name: 'coll-b', type: 'collector', version: '0.100.0', status: 'degraded',     last_seen_at: new Date().toISOString(), labels: {}, accepts_remote_config: false },
  { id: 'w3', fingerprint_source: 'uid', fingerprint_keys: {}, display_name: 'sdk-a',  type: 'sdk',       version: '0.99.0',  status: 'disconnected', last_seen_at: new Date().toISOString(), labels: {} },
]

const mockActivity = [
  { day: '2026-04-16', count: 1 },
  { day: '2026-04-17', count: 0 },
  { day: '2026-04-18', count: 2 },
  { day: '2026-04-19', count: 0 },
  { day: '2026-04-20', count: 4 },
  { day: '2026-04-21', count: 1 },
  { day: '2026-04-22', count: 3 },
]

test.describe('Dashboard', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await page.route('**/api/workloads*', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: JSON.stringify(mockWorkloads),
    }))
    await page.route('**/api/alerts*', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: '[]',
    }))
    await page.route('**/api/pushes/activity*', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: JSON.stringify(mockActivity),
    }))
  })

  test('renders the six stat cards', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.stat-grid .stat-card')).toHaveCount(6)
  })

  test('fleet health donut renders with correct total', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.fleet-donut text')).toHaveText('3')
  })

  test('push activity chart renders 7 bars', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.push-chart rect')).toHaveCount(7)
    await expect(page.locator('.push-chart rect.push-chart-bar-last')).toHaveCount(1)
  })

  test('config safety status summarizes supervised config readiness', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: /Config safety status|Statut de sécurité des configs/ })).toBeVisible()
    await expect(page.locator('.config-safety-panel')).toContainText(/Supervised collectors|Collecteurs supervisés/)
    await expect(page.locator('.config-safety-panel')).toContainText('1')
    await expect(page.locator('.config-safety-panel')).toContainText(/Last 7d pushes|Pushes sur 7j/)
    await expect(page.locator('.config-safety-panel')).toContainText('11')
    await expect(page.getByRole('link', { name: /Review supervised workloads|Voir les workloads supervisés/ })).toHaveAttribute('href', '/inventory?control=supervised')
  })

  test('config safety status panel summarizes supervised collectors and safe flow', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/')

    const panel = page.locator('.config-safety-status-panel')
    await expect(panel).toBeVisible()
    await expect(panel).toContainText('Safe remote config across supervised collectors')
    await expect(panel).toContainText('Supervised collectors')
    await expect(panel).toContainText('1')
    await expect(panel).toContainText('Last 7d pushes')
    await expect(panel).toContainText('11')
    await expect(panel).toContainText(
      'Validation, diff, safe push and rollback are available on workload detail pages.',
    )
    await expect(panel.getByRole('link', { name: 'Review supervised workloads' })).toHaveAttribute(
      'href',
      '/inventory?control=supervised',
    )

    const pushPanelTop = await page.locator('.panel', { hasText: 'Push activity' }).boundingBox()
    const safetyTop = await panel.boundingBox()
    const alertsTop = await page.locator('.panel', { hasText: 'Recent alerts' }).boundingBox()
    expect(pushPanelTop?.y).toBeLessThan(safetyTop?.y ?? Number.POSITIVE_INFINITY)
    expect(safetyTop?.y).toBeLessThan(alertsTop?.y ?? Number.POSITIVE_INFINITY)
  })

  test('deployed versions panel groups by version', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.versions-row')).toHaveCount(2)
    await expect(page.locator('.versions-row').first()).toContainText('0.100.0')
    await expect(page.locator('.versions-row').first()).toContainText('2')
  })

  test('clicking the Collectors stat card navigates to filtered inventory', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await page.locator('.stat-card', { hasText: /Collectors|Collecteurs/ }).click()
    await expect(page).toHaveURL(/\/inventory\?type=collector/)
  })
})
