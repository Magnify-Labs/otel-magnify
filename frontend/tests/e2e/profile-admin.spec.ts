import { test, expect, mockMe } from './fixtures'

test('administrator sees Administration link and stub page', async ({ loggedInPage: page }) => {
  await page.route('**/api/workloads/version-intelligence*', (route) => route.fulfill({
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
  }))
  await page.route('**/api/workloads*', (route) => route.fulfill({
    status: 200, contentType: 'application/json', body: '[]',
  }))
  await page.route('**/api/alerts*', (route) => route.fulfill({
    status: 200, contentType: 'application/json', body: '[]',
  }))
  await page.route('**/api/pushes/activity*', (route) => route.fulfill({
    status: 200, contentType: 'application/json', body: '[]',
  }))
  await page.route('**/api/config-safety/drift*', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ total: 0, drifted: 0, current: 0, unknown: 0, items: [] }),
  }))
  await page.route('**/api/features*', (route) => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify({ features: {} }),
  }))
  await mockMe(page, {
    groups: [{ id: 'grp_system_administrator', name: 'administrator', role: 'administrator', is_system: true, created_at: new Date().toISOString() }],
  })
  await page.goto('/')
  await expect(page.getByRole('link', { name: /administration/i })).toBeVisible()
  await page.getByRole('link', { name: /administration/i }).click()
  await expect(page.getByRole('heading', { name: /administration/i })).toBeVisible()
  await expect(page.getByText(/v0\.3|coming|arrive/i)).toBeVisible()
})
