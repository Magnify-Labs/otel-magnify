import { test, expect } from './fixtures'

function workload(id: string, displayName: string) {
  return {
    id,
    fingerprint_source: 'k8s',
    fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: displayName },
    display_name: displayName,
    type: 'collector',
    version: '0.100.0',
    status: 'connected',
    last_seen_at: new Date().toISOString(),
    labels: {},
    accepts_remote_config: true,
  }
}

function topology(workloadId: string, instances: unknown[]) {
  return {
    schema_version: 'workload-topology.v1',
    workload_id: workloadId,
    summary: {
      connected_count: instances.length,
      healthy_count: instances.length,
      unhealthy_count: 0,
      drifted_count: 0,
      heterogeneous: false,
      version_diversity: ['0.100.0'],
      config_hash_diversity: [],
      remote_config_status_counts: {},
      heterogeneity: {},
      heterogeneity_reasons: [],
    },
    instances,
  }
}

test.describe('Virtualized large frontend lists', () => {
  test('renders only the visible inventory cards while keeping the full scroll range reachable', async ({
    loggedInPage: page,
  }) => {
    const workloads = Array.from({ length: 160 }, (_, index) =>
      workload(`w-${index}`, `collector-${index.toString().padStart(3, '0')}`),
    )

    await page.route('**/api/workloads*', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname === '/api/workloads') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(workloads),
        })
        return
      }
      await route.continue()
    })

    await page.goto('/inventory')
    await expect(page.getByText('collector-000')).toBeVisible()

    expect(await page.locator('.workload-card').count()).toBeLessThan(80)

    const list = page.getByTestId('inventory-virtual-list')
    await list.evaluate((node) => {
      node.scrollTop = node.scrollHeight
      node.dispatchEvent(new Event('scroll'))
    })

    await expect(page.getByText('collector-159')).toBeVisible()
  })

  test('renders only the visible connected instance rows while preserving table semantics', async ({
    loggedInPage: page,
  }) => {
    const instances = Array.from({ length: 180 }, (_, index) => ({
      instance_uid: `instance-${index.toString().padStart(3, '0')}`,
      pod_name: `otel-pod-${index.toString().padStart(3, '0')}`,
      version: '0.100.0',
      connected_at: new Date().toISOString(),
      last_message_at: new Date().toISOString(),
      effective_config_hash: `hash-${index.toString().padStart(3, '0')}`,
      healthy: true,
    }))

    await page.route('**/api/configs', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/workloads/w1/configs', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })
    await page.route('**/api/workloads/w1/topology', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(topology('w1', instances)),
      })
    })
    await page.route('**/api/workloads/w1', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(workload('w1', 'otel-collector')),
      })
    })

    await page.goto('/workloads/w1')
    await page.getByRole('button', { name: /^instances$/i }).click()
    await expect(page.getByText('otel-pod-000')).toBeVisible()

    expect(await page.locator('.instances-table tbody tr').count()).toBeLessThan(80)
    await expect(page.locator('.instances-table')).toHaveAttribute('aria-rowcount', '180')

    const tableScroller = page.getByTestId('instances-virtual-table')
    await tableScroller.evaluate((node) => {
      node.scrollTop = node.scrollHeight
      node.dispatchEvent(new Event('scroll'))
    })

    await expect(page.getByText('otel-pod-179')).toBeVisible()
  })
})
