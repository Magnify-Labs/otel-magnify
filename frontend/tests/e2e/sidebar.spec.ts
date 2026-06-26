import { test, expect } from './fixtures'

test.describe('Sidebar', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await page.route('**/api/workloads*', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: '[]',
    }))
    await page.route('**/api/alerts*', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: '[]',
    }))
    await page.route('**/api/pushes/activity*', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: '[]',
    }))
  })

  test('renders FLEET + ACCOUNT sections with five items and no ENTERPRISE section', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.sidebar-section-label').filter({ hasText: /FLEET|FLOTTE/ })).toBeVisible()
    await expect(page.locator('.sidebar-section-label').filter({ hasText: /ENTERPRISE/i })).toHaveCount(0)
    await expect(page.locator('.sidebar-nav-item a')).toHaveCount(5)
  })

  test('dashboard item is active on /', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.sidebar-nav-item a.active')).toHaveCount(1)
    await expect(page.locator('.sidebar-nav-item a.active')).toContainText(/Dashboard|Tableau/)
  })

  test('inventory item is active on /inventory', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await expect(page.locator('.sidebar-nav-item a.active')).toContainText(/Inventory|Inventaire/)
  })


  test('configs item is renamed to Config Library and remains active on /configs', async ({ loggedInPage: page }) => {
    await page.route('**/api/configs', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: '[]',
    }))
    await page.goto('/configs')
    await expect(page.locator('.sidebar-nav-item a.active')).toContainText(/Config Library|Bibliothèque de configs/)
    await expect(page.getByRole('heading', { name: /Config Library|Bibliothèque de configs/ })).toBeVisible()
    await expect(page.getByRole('button', { name: /\+ New config|\+ Nouvelle config/ })).toBeVisible()
  })

  test('alert badge appears when alerts > 0', async ({ loggedInPage: page }) => {
    await page.route('**/api/alerts*', (route) => route.fulfill({
      status: 200, contentType: 'application/json',
      body: JSON.stringify([
        { id: 'a1', workload_id: 'w1', rule: 'workload_down', severity: 'critical', message: 'down', fired_at: new Date().toISOString() },
        { id: 'a2', workload_id: 'w1', rule: 'config_drift',  severity: 'warning',  message: 'drift', fired_at: new Date().toISOString() },
      ]),
    }))
    await page.goto('/')
    await expect(page.locator('.sidebar-badge')).toHaveText('2')
  })

  test('footer shows LIVE pill with version', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.sidebar-footer')).toContainText('LIVE')
    await expect(page.locator('.sidebar-footer')).toContainText(/v\d/)
  })
})
