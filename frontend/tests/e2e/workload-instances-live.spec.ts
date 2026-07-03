import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

test.describe('Workload Instances tab', () => {
  test('renders topology status and heterogeneity warnings', async ({ loggedInPage: page }) => {
    const workloadID = 'w1'

    await page.route(`**/api/workloads/${workloadID}`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: workloadID,
          fingerprint_source: 'k8s',
          fingerprint_keys: { kind: 'deployment', name: 'otel' },
          display_name: 'otel-collector',
          type: 'collector',
          version: '0.100.0',
          status: 'connected',
          last_seen_at: new Date().toISOString(),
          labels: {},
          active_config_hash: 'abcdef1234567890',
          accepts_remote_config: true,
        }),
      })
    })

    await page.route(`**/api/workloads/${workloadID}/topology`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'workload-topology.v1',
          workload_id: workloadID,
          summary: {
            connected_count: 2,
            healthy_count: 1,
            unhealthy_count: 1,
            drifted_count: 1,
            heterogeneous: true,
            version_diversity: ['0.100.0', '0.99.0'],
            config_hash_diversity: ['abcdef1234567890', 'deadbeefcafebabe'],
            remote_config_status_counts: { applied: 1, failed: 1, no_status: 0 },
            heterogeneity: {
              mixed_versions: true,
              mixed_effective_config_hashes: true,
              unhealthy_instances: true,
              failed_remote_config: true,
            },
            heterogeneity_reasons: [
              'mixed_versions',
              'mixed_effective_config_hashes',
              'unhealthy_instances',
              'failed_remote_config',
            ],
          },
          instances: [
            {
              instance_uid: 'uid-aaaaaaaa',
              pod_name: 'otel-abc',
              version: '0.100.0',
              connected_at: new Date().toISOString(),
              last_message_at: new Date().toISOString(),
              effective_config_hash: 'abcdef1234567890',
              healthy: true,
              accepts_remote_config: true,
              remote_config_status: {
                status: 'applied',
                config_hash: 'abcdef1234567890',
                updated_at: new Date().toISOString(),
              },
            },
            {
              instance_uid: 'uid-bbbbbbbb',
              pod_name: 'otel-xyz',
              version: '0.99.0',
              connected_at: new Date().toISOString(),
              last_message_at: new Date(Date.now() - 120_000).toISOString(),
              effective_config_hash: 'deadbeefcafebabe',
              healthy: false,
              accepts_remote_config: true,
              remote_config_status: {
                status: 'failed',
                config_hash: 'deadbeefcafebabe',
                error_message: 'processor missing',
                updated_at: new Date().toISOString(),
              },
            },
          ],
        }),
      })
    })

    await page.route(`**/api/workloads/${workloadID}/events*`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    })
    await mockConfigSafetyReads(page, workloadID)

    await page.goto(`/workloads/${workloadID}`)

    await page.getByRole('button', { name: /^instances$/i }).click()

    await expect(page.getByText('otel-abc')).toBeVisible()
    await expect(page.getByText('otel-xyz')).toBeVisible()
    await expect(page.locator('.topology-summary')).toContainText('2 connected')
    await expect(page.locator('.topology-warning-list')).toContainText('Mixed collector versions')
    await expect(page.locator('.topology-warning-list')).toContainText('Remote config failed')
    await expect(page.getByRole('cell', { name: /Failed/ })).toBeVisible()
    await expect(page.getByText('processor missing')).toBeVisible()
    await expect(page.locator('.instance-drift-tag')).toContainText(/Drift/i)
  })

  test('shows topology empty and error states', async ({ loggedInPage: page }) => {
    const workloadID = 'w-empty'

    await page.route(`**/api/workloads/${workloadID}`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: workloadID,
          fingerprint_source: 'k8s',
          fingerprint_keys: {},
          display_name: 'empty-collector',
          type: 'collector',
          version: '0.100.0',
          status: 'connected',
          last_seen_at: new Date().toISOString(),
          labels: {},
          accepts_remote_config: true,
        }),
      })
    })
    await page.route(`**/api/workloads/${workloadID}/events*`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    })
    await mockConfigSafetyReads(page, workloadID)

    await page.route(`**/api/workloads/${workloadID}/topology`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'workload-topology.v1',
          workload_id: workloadID,
          summary: {
            connected_count: 0,
            healthy_count: 0,
            unhealthy_count: 0,
            drifted_count: 0,
            heterogeneous: false,
            version_diversity: [],
            config_hash_diversity: [],
            remote_config_status_counts: {},
            heterogeneity: {},
            heterogeneity_reasons: [],
          },
          instances: [],
        }),
      })
    })

    await page.goto(`/workloads/${workloadID}`)
    await page.getByRole('button', { name: /^instances$/i }).click()
    await expect(page.getByText('No instance topology is currently available.')).toBeVisible()

    await page.route(`**/api/workloads/${workloadID}/topology`, async (route) => {
      await route.fulfill({ status: 500, contentType: 'application/json', body: '{}' })
    })
    await page.reload()
    await page.getByRole('button', { name: /^instances$/i }).click()
    await expect(page.getByText('Unable to load workload topology.')).toBeVisible()
  })
})

async function mockConfigSafetyReads(page: Page, workloadID: string) {
  await page.route('**/api/configs', async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })
  await page.route(`**/api/workloads/${workloadID}/configs`, async (route) => {
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
  })
  await page.route(`**/api/workloads/${workloadID}/known-good`, async (route) => {
    await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({}) })
  })
}
