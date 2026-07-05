import { test, expect } from './fixtures'

test.describe('Inventory instance count', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await page.route('**/api/workloads*', async (route) => {
      const url = route.request().url()
      // Only stub the collection endpoint, not child resources like /events
      if (/\/api\/workloads(\?|$)/.test(url) || url.endsWith('/api/workloads')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              id: 'w1',
              fingerprint_source: 'k8s',
              fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
              display_name: 'otel-collector',
              type: 'collector',
              version: '0.100.0',
              status: 'connected',
              last_seen_at: new Date().toISOString(),
              labels: { 'k8s.deployment.name': 'otel' },
              accepts_remote_config: true,
            },
          ]),
        })
        return
      }
      await route.continue()
    })
  })

  test('shows connected_instance_count badge after workload_update WS frame', async ({ loggedInPage: page }) => {
    await page.goto('/inventory')
    await expect(page.getByText('otel-collector')).toBeVisible()

    await page.evaluate(() => {
      ;(window as unknown as { __testWsInject: (ev: unknown) => void }).__testWsInject({
        type: 'workload_update',
        workload: {
          id: 'w1',
          fingerprint_source: 'k8s',
          fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
          display_name: 'otel-collector',
          type: 'collector',
          version: '0.100.0',
          status: 'connected',
          last_seen_at: new Date().toISOString(),
          labels: {},
          accepts_remote_config: true,
        },
        connected_instance_count: 3,
        drifted_instance_count: 0,
      })
    })

    await expect(page.locator('.instance-count-badge')).toContainText('3')
  })

  test('updates the inventory card from the TanStack workload cache after a WS frame', async ({ loggedInPage: page }) => {
    const workloadListLoaded = page.waitForResponse(
      (response) => response.url().includes('/api/workloads') && response.status() === 200,
    )
    await page.goto('/inventory')
    await workloadListLoaded
    await expect(page.locator('.workload-card', { hasText: 'otel-collector' }).locator('.badge')).toHaveText(
      'connected',
    )

    await page.evaluate(() => {
      ;(window as unknown as { __testWsInject: (ev: unknown) => void }).__testWsInject({
        type: 'workload_update',
        workload: {
          id: 'w1',
          fingerprint_source: 'k8s',
          fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
          display_name: 'otel-collector',
          type: 'collector',
          version: '0.100.0',
          status: 'degraded',
          last_seen_at: new Date().toISOString(),
          labels: {},
          accepts_remote_config: true,
        },
      })
    })

    await expect(page.locator('.workload-card', { hasText: 'otel-collector' }).locator('.badge')).toHaveText(
      'degraded',
    )
  })
})
