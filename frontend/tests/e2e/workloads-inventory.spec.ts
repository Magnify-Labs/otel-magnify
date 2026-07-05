import { test, expect, mockMe } from './fixtures'

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

test.describe('Workload archive UX', () => {
  test('hides archived workloads by default and reveals them on demand', async ({ loggedInPage: page }) => {
    await page.route('**/api/workloads*', async (route) => {
      const url = route.request().url()
      if (!/\/api\/workloads(\?|$)/.test(url) && !url.endsWith('/api/workloads')) {
        await route.continue()
        return
      }
      const includeArchived = new URL(url).searchParams.get('include_archived') === 'true'
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'w-live',
            fingerprint_source: 'k8s',
            fingerprint_keys: {},
            display_name: 'live-collector',
            type: 'collector',
            version: '0.100.0',
            status: 'connected',
            last_seen_at: new Date().toISOString(),
            labels: {},
            accepts_remote_config: true,
          },
          ...(includeArchived
            ? [
                {
                  id: 'w-old',
                  fingerprint_source: 'k8s',
                  fingerprint_keys: {},
                  display_name: 'old-collector',
                  type: 'collector',
                  version: '0.99.0',
                  status: 'disconnected',
                  last_seen_at: new Date(Date.now() - 86_400_000).toISOString(),
                  labels: {},
                  archived_at: new Date().toISOString(),
                },
              ]
            : []),
        ]),
      })
    })

    await page.goto('/inventory')
    await expect(page.getByText('live-collector')).toBeVisible()
    await expect(page.getByText('old-collector')).toHaveCount(0)

    await page.getByLabel('Show archived').check()

    await expect(page.getByText('old-collector')).toBeVisible()
    await expect(page.locator('.agent-archived-pill')).toHaveText('ARCHIVED')
  })

  test('lets editors archive a disconnected workload from detail', async ({ loggedInPage: page }) => {
    await mockMe(page, {
      groups: [
        {
          id: 'grp_system_editor',
          name: 'editor',
          role: 'editor',
          is_system: true,
          created_at: new Date().toISOString(),
        },
      ],
    })
    await page.route('**/api/workloads/w-old', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'w-old',
          fingerprint_source: 'k8s',
          fingerprint_keys: {},
          display_name: 'old-collector',
          type: 'collector',
          version: '0.99.0',
          status: 'disconnected',
          last_seen_at: new Date(Date.now() - 86_400_000).toISOString(),
          labels: {},
          accepts_remote_config: true,
        }),
      })
    })
    await page.route('**/api/configs', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/workloads/w-old/configs', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/workloads/w-old/archive', async (route) => {
      await route.fulfill({ status: 204 })
    })

    page.on('dialog', (dialog) => dialog.accept())
    await page.goto('/workloads/w-old')
    await page.getByRole('button', { name: 'Archive workload' }).click()

    await expect(page).toHaveURL(/\/inventory$/)
  })
})
