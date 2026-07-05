import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

type TestWorkload = {
  id: string
  fingerprint_source: 'k8s'
  fingerprint_keys: Record<string, string>
  display_name: string
  type: 'collector'
  version: string
  status: 'connected' | 'disconnected' | 'degraded'
  last_seen_at: string
  labels: Record<string, string>
  accepts_remote_config: boolean
}

type TestEvent = {
  id: number
  workload_id: string
  instance_uid: string
  pod_name?: string
  event_type: 'connected' | 'disconnected' | 'version_changed'
  version?: string
  prev_version?: string
  occurred_at: string
}

async function injectWs(page: Page, payload: unknown) {
  await page.evaluate((ev) => {
    ;(window as unknown as { __testWsInject: (ev: unknown) => void }).__testWsInject(ev)
  }, payload)
}

function buildWorkload(overrides: Partial<TestWorkload> = {}): TestWorkload {
  return {
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
    ...overrides,
  }
}

test.describe('WebSocket query cache patching', () => {
  test('patches workload and activity cache without refetching safe queries', async ({
    loggedInPage: page,
  }) => {
    let collectionRequests = 0
    let detailRequests = 0
    let eventRequests = 0
    let statsRequests = 0

    const workload = buildWorkload()
    const initialEvents: TestEvent[] = [
      {
        id: 1,
        workload_id: 'w1',
        instance_uid: 'uid-old',
        pod_name: 'otel-old',
        event_type: 'disconnected',
        occurred_at: new Date(Date.now() - 60_000).toISOString(),
      },
    ]

    await page.route('**/api/configs', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
    })

    await page.route('**/api/workloads**', async (route) => {
      const url = new URL(route.request().url())
      if (url.pathname === '/api/workloads') {
        collectionRequests += 1
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([workload]),
        })
        return
      }
      if (url.pathname === '/api/workloads/w1') {
        detailRequests += 1
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(workload),
        })
        return
      }
      if (url.pathname === '/api/workloads/w1/configs') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
        return
      }
      if (url.pathname === '/api/workloads/w1/events/stats') {
        statsRequests += 1
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            connected: 0,
            disconnected: 1,
            version_changed: 0,
            churn_rate_per_hour: 1 / 24,
          }),
        })
        return
      }
      if (url.pathname === '/api/workloads/w1/events') {
        eventRequests += 1
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(initialEvents),
        })
        return
      }
      await route.continue()
    })

    await page.goto('/inventory')
    await expect(page.getByText('otel-collector')).toBeVisible()
    expect(collectionRequests).toBe(1)

    await injectWs(page, {
      type: 'workload_update',
      workload: buildWorkload({ display_name: 'otel-renamed', status: 'degraded' }),
      connected_instance_count: 4,
      drifted_instance_count: 1,
    })

    await expect(page.getByText('otel-renamed')).toBeVisible()
    await expect(page.locator('.workload-card').first()).toContainText('degraded')
    await page.waitForTimeout(300)
    expect(collectionRequests).toBe(1)

    await page.goto('/workloads/w1')
    await expect(page.getByRole('heading', { name: 'otel-collector' })).toBeVisible()
    expect(detailRequests).toBe(1)

    await injectWs(page, {
      type: 'workload_update',
      workload: buildWorkload({
        display_name: 'otel-offline',
        status: 'disconnected',
        version: '0.101.0',
      }),
    })

    await expect(page.getByRole('heading', { name: 'otel-offline' })).toBeVisible()
    await expect(page.getByText('v0.101.0')).toBeVisible()
    await expect(page.getByText('disconnected')).toBeVisible()
    await page.waitForTimeout(300)
    expect(detailRequests).toBe(1)

    await page.getByRole('button', { name: /^activity$/i }).click()
    await expect(page.locator('.activity-entry')).toHaveCount(1)
    expect(eventRequests).toBe(1)
    expect(statsRequests).toBe(1)

    await injectWs(page, {
      type: 'workload_event',
      event: {
        id: 2,
        workload_id: 'w1',
        instance_uid: 'uid-new',
        pod_name: 'otel-new',
        event_type: 'disconnected',
        occurred_at: new Date().toISOString(),
      },
    })

    await expect(page.locator('.activity-entry')).toHaveCount(2)
    await expect(page.locator('.activity-entry').first()).toContainText('otel-new')
    await expect(page.locator('.activity-header')).toContainText('2 disconnects')
    await page.waitForTimeout(300)
    expect(eventRequests).toBe(1)
    expect(statsRequests).toBe(1)

    await injectWs(page, {
      type: 'workload_event',
      event: {
        id: 3,
        workload_id: 'w1',
        instance_uid: 'uid-invalid',
        pod_name: 'otel-invalid',
        event_type: '__proto__',
        occurred_at: new Date().toISOString(),
      },
    })

    await expect(page.locator('.activity-entry')).toHaveCount(2)
    await expect(page.locator('.activity-header')).toContainText('2 disconnects')
  })
})
