import { test, expect, mockCapabilities, mockMe } from './fixtures'
import type { Page } from '@playwright/test'

const WORKLOAD_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const RESOURCE_ID = 'workload:prod-eu'
const CONFIG_HASH = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'

const adminGroup = {
  id: 'grp_system_administrator',
  name: 'administrator' as const,
  role: 'administrator' as const,
  is_system: true,
  created_at: new Date().toISOString(),
}

const viewerGroup = {
  id: 'grp_system_viewer',
  name: 'viewer' as const,
  role: 'viewer' as const,
  is_system: true,
  created_at: new Date().toISOString(),
}

async function routeDashboardNoise(page: Page) {
  await page.route('**/api/workloads*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
  await page.route('**/api/alerts*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
  await page.route('**/api/pushes/activity*', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
}

test.describe('Audit viewer', () => {
  test('administrator can filter audit events, preserve query params, export CSV, and open config links', async ({
    loggedInPage: page,
  }) => {
    await routeDashboardNoise(page)
    await mockCapabilities(page, { 'audit.viewer': true })
    await mockMe(page, { groups: [adminGroup] })

    const requests: URL[] = []
    await page.route('**/api/audit/events?**', async (route, request) => {
      const url = new URL(request.url())
      requests.push(url)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          available: true,
          total: 1,
          limit: 50,
          offset: 0,
          events: [
            {
              id: 'evt-1',
              occurred_at: '2026-07-02T10:11:12Z',
              action: 'config.rollback',
              user_id: 'u-1',
              email: 'admin@example.com',
              resource: 'workload',
              resource_id: RESOURCE_ID,
              workload_id: WORKLOAD_ID,
              config_hash: CONFIG_HASH,
              detail: 'Rolled back collector config',
              prev_hash: 'prevhash',
              event_hash: 'eventhash',
              immutable_ref: 'siem://events/evt-1',
            },
          ],
        }),
      })
    })

    await page.goto(
      '/audit?user=admin%40example.com&action=config.rollback&workload_id=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa&resource_id=workload%3Aprod-eu&config_hash=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&from=2026-07-01T00%3A00%3A00Z&to=2026-07-03T00%3A00%3A00Z',
    )

    await expect(page.getByRole('link', { name: /Audit/i })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Audit events' })).toBeVisible()
    await expect(page.getByLabel('User or email')).toHaveValue('admin@example.com')
    await expect(page.getByLabel('Action')).toHaveValue('config.rollback')
    await expect(page.getByLabel('Workload ID')).toHaveValue(WORKLOAD_ID)
    await expect(page.getByLabel('Resource ID')).toHaveValue(RESOURCE_ID)
    await expect(page.getByLabel('Config hash')).toHaveValue(CONFIG_HASH)

    await expect(page.getByRole('cell', { name: 'admin@example.com' })).toBeVisible()
    await expect(page.getByRole('cell', { name: 'config.rollback' })).toBeVisible()
    await expect(page.getByText('Rolled back collector config')).toBeVisible()
    await expect(page.getByText('prevhash')).toBeVisible()
    await expect(page.getByText('eventhash')).toBeVisible()

    const lastRequest = requests.at(-1)
    expect(lastRequest?.searchParams.get('user')).toBe('admin@example.com')
    expect(lastRequest?.searchParams.get('action')).toBe('config.rollback')
    expect(lastRequest?.searchParams.get('workload_id')).toBe(WORKLOAD_ID)
    expect(lastRequest?.searchParams.get('resource_id')).toBe(RESOURCE_ID)
    expect(lastRequest?.searchParams.get('config_hash')).toBe(CONFIG_HASH)
    expect(lastRequest?.searchParams.get('from')).toBe('2026-07-01T00:00:00Z')
    expect(lastRequest?.searchParams.get('to')).toBe('2026-07-03T00:00:00Z')

    const exportHref = await page.getByRole('link', { name: 'Export CSV' }).getAttribute('href')
    expect(exportHref).toContain('/api/audit/events.csv?')
    expect(exportHref).toContain('user=admin%40example.com')
    expect(exportHref).toContain(`workload_id=${WORKLOAD_ID}`)
    expect(exportHref).toContain('resource_id=workload%3Aprod-eu')
    expect(exportHref).toContain(`config_hash=${CONFIG_HASH}`)

    await expect(page.getByRole('link', { name: 'View diff' })).toHaveAttribute(
      'href',
      `/workloads/${WORKLOAD_ID}?config_hash=${CONFIG_HASH}#config-history`,
    )
    await expect(page.getByRole('link', { name: 'Rollback' })).toHaveAttribute(
      'href',
      `/workloads/${WORKLOAD_ID}?rollback_hash=${CONFIG_HASH}#config-history`,
    )
  })

  test('non-audit users do not see nav and manual route is not useful', async ({
    loggedInPage: page,
  }) => {
    await routeDashboardNoise(page)
    await mockCapabilities(page, { 'audit.viewer': true })
    await mockMe(page, { groups: [viewerGroup] })

    let auditHit = false
    await page.route('**/api/audit/events**', (route) => {
      auditHit = true
      return route.fulfill({ status: 500, body: 'viewer should not query audit' })
    })

    await page.goto('/')
    await expect(page.getByRole('link', { name: /Audit/i })).toHaveCount(0)

    await page.goto('/audit')
    await expect(page.getByRole('heading', { name: 'Audit events' })).toHaveCount(0)
    await expect(page.getByText('Audit access required')).toBeVisible()
    expect(auditHit).toBe(false)
  })

  test('distinguishes unavailable audit sink from empty filtered results', async ({
    loggedInPage: page,
  }) => {
    await routeDashboardNoise(page)
    await mockCapabilities(page, { 'audit.viewer': true })
    await mockMe(page, { groups: [adminGroup] })

    await page.route('**/api/audit/events?**', (route, request) => {
      const url = new URL(request.url())
      const available = url.searchParams.get('action') !== 'sink.missing'
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ available, total: 0, limit: 50, offset: 0, events: [] }),
      })
    })

    await page.goto('/audit?action=sink.missing')
    await expect(page.getByText('No readable audit sink is configured.')).toBeVisible()

    await page.goto('/audit?action=config.create')
    await expect(page.getByText('No audit events match the current filters.')).toBeVisible()
  })
})
