import { test, expect } from './fixtures'

test('renders enabled v1 navigation without calling the legacy endpoint', async ({ loggedInPage: page }) => {
  const legacyRequests: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/features') legacyRequests.push(request.url())
  })
  await page.route('**/api/v1/capabilities', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      api_version: 'v1',
      capabilities: [{ id: 'config_safety.drift_dashboard', state: 'enabled' }],
    }),
  }))

  await page.goto('/profile')
  await expect(page.getByRole('link', { name: /config drift|drift config/i })).toBeVisible()
  expect(legacyRequests).toEqual([])
})

test('fails closed and reports a v1 loading error', async ({ loggedInPage: page }) => {
  await page.route('**/api/v1/capabilities', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ api_version: 'v1', capabilities: [{ id: 'audit.viewer', state: 'future' }] }),
  }))

  await page.goto('/profile')
  await expect(page.getByRole('alert')).toContainText(/capabilit/i)
  await expect(page.getByRole('link', { name: /config drift|drift config/i })).toHaveCount(0)
})

test('fails closed when the v1 endpoint returns an HTTP error', async ({ loggedInPage: page }) => {
  await page.route('**/api/v1/capabilities', (route) => route.fulfill({ status: 500 }))

  await page.goto('/profile')
  await expect(page.getByRole('alert')).toContainText(/capabilit/i)
  await expect(page.getByRole('link', { name: /config drift|drift config/i })).toHaveCount(0)
})
