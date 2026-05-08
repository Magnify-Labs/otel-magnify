import { test, expect } from './fixtures'
import type { Page, Route } from '@playwright/test'

const WORKLOAD_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const ACTIVE_CONFIG_ID = 'cfg-active'

// The deeper otelcol-binary validation lives at /api/configs/validate.
// The workload-scoped /api/workloads/{id}/config/validate covers structural
// shape + AvailableComponents; the global endpoint adds per-component schema
// errors. The frontend calls both and merges errors. This spec drives the
// state-machine: idle → loading → invalid → fix → valid → push enabled.

function mockWorkload(page: Page) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: WORKLOAD_ID,
        fingerprint_source: 'k8s',
        fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
        display_name: 'svc',
        type: 'collector',
        version: '0.150.1',
        status: 'connected',
        last_seen_at: new Date().toISOString(),
        labels: {},
        active_config_id: ACTIVE_CONFIG_ID,
        accepts_remote_config: true,
        available_components: {
          components: { receivers: ['otlp'], exporters: ['logging'] },
        },
      }),
    }),
  )
}

function mockActiveConfig(page: Page) {
  return page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: ACTIVE_CONFIG_ID,
        name: 'current',
        content: 'receivers:\n  otlp: {}\n',
        created_at: new Date().toISOString(),
        created_by: 'seed',
      }),
    }),
  )
}

function mockHistoryEmpty(page: Page) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/configs`, (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  )
}

function mockConfigsList(page: Page) {
  return page.route('**/api/configs', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' })
    }
    return route.fallback()
  })
}

// Programmable validate mocks: each call to next(...) sets the body of the
// NEXT validate request. The route handler latches the queued response and
// resets to a sane default afterwards, so a test can drive a sequence
// (invalid → valid → ...) without re-registering routes.
function programmableValidate(page: Page, urlPattern: string | RegExp) {
  let queued: { valid: boolean; errors?: unknown[] } | null = null
  let calls = 0
  page.route(urlPattern, async (route: Route) => {
    calls += 1
    const body = queued ?? { valid: true }
    queued = null
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    })
  })
  return {
    next(payload: { valid: boolean; errors?: unknown[] }) {
      queued = payload
    },
    callCount() {
      return calls
    },
  }
}

test('idle → loading → invalid → fix → valid → push enabled', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistoryEmpty(page)
  await mockConfigsList(page)

  // Both validate endpoints are programmable. We script them as a pair:
  // first call returns invalid (with a path); second call returns valid.
  const workloadValidate = programmableValidate(
    page,
    `**/api/workloads/${WORKLOAD_ID}/config/validate`,
  )
  const configValidate = programmableValidate(page, '**/api/configs/validate')

  // Slow the deeper validator by a tick so the user sees the loading state.
  await page.route('**/api/configs/validate', async (route) => {
    await new Promise((r) => setTimeout(r, 200))
    await route.fallback()
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()

  // ── idle: Validate button visible, Push disabled, no validation block ──
  const validateBtn = page.getByRole('button', { name: 'Validate' })
  const pushBtn = page.getByTestId('push-button')
  await expect(validateBtn).toBeEnabled()
  await expect(pushBtn).toBeDisabled()
  await expect(page.getByTestId('validation-block')).toHaveCount(0)

  // ── invalid run: queue an error from the deep validator ──
  workloadValidate.next({ valid: true })
  configValidate.next({
    valid: false,
    errors: [
      {
        code: 'otelcol_validate',
        message: "exporter 'logging' has invalid keys: bogus",
        path: 'exporters.logging',
      },
    ],
  })
  // Make a draft change so the button can fire (Validate is disabled on empty draft).
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type('\n# touched-1')
  await validateBtn.click()

  // ── loading: button switches to Validating... ──
  await expect(page.getByRole('button', { name: 'Validating...' })).toBeVisible()

  // ── invalid: errors listed, push still disabled, override link surfaces ──
  await expect(page.getByTestId('validation-block')).toHaveClass(/validation-errors/)
  await expect(page.getByTestId('validation-block')).toContainText('exporters.logging')
  await expect(pushBtn).toBeDisabled()
  await expect(page.getByTestId('override-link')).toBeVisible()

  // ── fix: editing the draft clears the previous validation result ──
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type('\n# touched-2')
  await expect(page.getByTestId('validation-block')).toHaveCount(0)

  // ── valid run: both endpoints return valid ──
  workloadValidate.next({ valid: true })
  configValidate.next({ valid: true })
  await validateBtn.click()
  await expect(page.getByTestId('validation-block')).toHaveClass(/validation-ok/)
  await expect(pushBtn).toBeEnabled()
  // Once validation passes, the override link is no longer surfaced.
  await expect(page.getByTestId('override-link')).toHaveCount(0)
})

test('override link enables push without validation and forwards ?override=true', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistoryEmpty(page)
  await mockConfigsList(page)

  // Both validators report invalid; user must override to push.
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/validate`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        valid: false,
        errors: [{ code: 'undefined_component', message: 'oops', path: 'service.pipelines.x' }],
      }),
    }),
  )
  await page.route('**/api/configs/validate', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '{"valid":false}' }),
  )

  // Capture every push request URL via a network listener (instead of a
  // route override) so the test does not race with route registration order:
  // page.on('request') fires regardless of which mock fulfilled the request.
  const pushUrls: string[] = []
  page.on('request', (req) => {
    const url = req.url()
    if (req.method() === 'POST' && /\/api\/workloads\/[^/]+\/config(\?|$)/.test(url)) {
      pushUrls.push(url)
    }
  })
  await page.route(/\/api\/workloads\/[^/]+\/config(\?|$)/, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'config push initiated', config_hash: 'cafebabecafebabe' }),
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit' }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type('\n# changed')
  await page.getByRole('button', { name: 'Validate' }).click()

  await expect(page.getByTestId('validation-block')).toHaveClass(/validation-errors/)
  const pushBtn = page.getByTestId('push-button')
  await expect(pushBtn).toBeDisabled()

  // Engage override: push button enables and its label changes to convey the
  // bypass (so the user understands they're not on the green path).
  await page.getByTestId('override-link').click()
  await expect(pushBtn).toBeEnabled()
  await expect(pushBtn).toHaveText(/override/i)

  await pushBtn.click()
  await expect.poll(() => pushUrls.length).toBeGreaterThan(0)
  expect(pushUrls[0]).toMatch(/[?&]override=true/)
})
