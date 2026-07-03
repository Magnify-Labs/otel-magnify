import { test, expect } from './fixtures'

interface TestEvent {
  id: number
  workload_id: string
  instance_uid: string
  pod_name?: string
  event_type: 'connected' | 'disconnected' | 'version_changed'
  version?: string
  prev_version?: string
  occurred_at: string
}

test.describe('Workload Activity tab', () => {
  test('renders events newest-first and reacts to WS injection', async ({ loggedInPage: page }) => {
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
          accepts_remote_config: true,
        }),
      })
    })

    await page.route(`**/api/workloads/${workloadID}/instances`, async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })

    await page.route('**/api/configs', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })

    await page.route(`**/api/workloads/${workloadID}/configs`, async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    })

    await page.route(`**/api/workloads/${workloadID}/known-good`, async (route) => {
      await route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ error: 'not found' }) })
    })

    const events: TestEvent[] = [
      {
        id: 1,
        workload_id: workloadID,
        instance_uid: 'uid-aaaa',
        pod_name: 'otel-old',
        event_type: 'connected',
        version: '0.100.0',
        occurred_at: '2026-04-22T10:00:00Z',
      },
      {
        id: 2,
        workload_id: workloadID,
        instance_uid: 'uid-aaaa',
        pod_name: 'otel-old',
        event_type: 'disconnected',
        occurred_at: '2026-04-22T11:00:00Z',
      },
    ]

    await page.route(`**/api/workloads/${workloadID}/events**`, async (route) => {
      const u = new URL(route.request().url())
      if (u.pathname.endsWith('/stats')) {
        const disconnected = events.filter((e) => e.event_type === 'disconnected').length
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            connected: events.filter((e) => e.event_type === 'connected').length,
            disconnected,
            version_changed: 0,
            churn_rate_per_hour: disconnected / 24,
          }),
        })
        return
      }
      const sorted = [...events].sort((a, b) => (a.occurred_at < b.occurred_at ? 1 : -1))
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sorted) })
    })

    await page.goto(`/workloads/${workloadID}`)
    await page.getByRole('button', { name: /^activity$/i }).click()

    const entries = page.locator('.activity-entry')
    await expect(entries).toHaveCount(2)
    await expect(entries.nth(0)).toContainText(/disconnect/i)
    await expect(entries.nth(1)).toContainText(/connect/i)

    await expect(page.locator('.activity-header')).toContainText('1')

    // WS injection of a new connect event.
    events.push({
      id: 3,
      workload_id: workloadID,
      instance_uid: 'uid-bbbb',
      pod_name: 'otel-new',
      event_type: 'connected',
      version: '0.100.0',
      occurred_at: '2026-04-22T12:00:00Z',
    })
    await page.evaluate(() => {
      ;(window as unknown as { __testWsInject: (ev: unknown) => void }).__testWsInject({
        type: 'workload_event',
        event: {
          id: 3,
          workload_id: 'w1',
          instance_uid: 'uid-bbbb',
          pod_name: 'otel-new',
          event_type: 'connected',
          version: '0.100.0',
          occurred_at: '2026-04-22T12:00:00Z',
        },
      })
    })

    await expect(entries).toHaveCount(3, { timeout: 5000 })
    await expect(entries.nth(0)).toContainText('otel-new')
  })
})
