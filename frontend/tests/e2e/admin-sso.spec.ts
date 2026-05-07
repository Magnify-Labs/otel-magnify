import { test, expect, mockMe } from './fixtures'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'

// ESM-compatible __dirname substitute
const fixturesDir = fileURLToPath(new URL('../fixtures', import.meta.url))

const adminGroup = {
  id: 'grp_system_administrator',
  name: 'administrator' as const,
  role: 'administrator' as const,
  is_system: true,
  created_at: new Date().toISOString(),
}
const editorGroup = {
  id: 'grp_system_editor',
  name: 'editor' as const,
  role: 'editor' as const,
  is_system: true,
  created_at: new Date().toISOString(),
}

async function mockFeatures(page: Page, features: Record<string, boolean>) {
  await page.route('**/api/features', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ features }),
    }),
  )
}

// Stub out the Dashboard API calls so that a redirect to "/" does not hit the
// real backend and trigger a 401 → /login redirect via the axios interceptor.
async function mockDashboard(page: Page) {
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

async function mockProviders(page: Page, providers: unknown[]) {
  await page.route('**/api/admin/sso/providers', (route) => {
    if (route.request().method() === 'GET') {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(providers),
      })
    }
    return route.continue()
  })
}

const sampleProvider = {
  id: 'okta-main',
  type: 'saml',
  display_name: 'Okta Corporate',
  idp_metadata_url: 'https://corp.okta.com/saml/metadata',
  idp_metadata_xml: '',
  sp_entity_id: 'https://otel-magnify.example.com/api/auth/sso/okta-main/metadata',
  allow_idp_initiated: false,
  default_groups: ['viewer'],
  active: true,
  created_at: '2026-04-30T08:00:00Z',
  updated_at: '2026-04-30T08:00:00Z',
}

test.describe('SSO admin — feature flag gating', () => {
  test('hides SSO sub-link when feature is off (community-only build)', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, {})
    await page.goto('/admin')
    await expect(page.getByRole('link', { name: /SSO providers/i })).toHaveCount(0)

    await page.goto('/admin/sso/providers')
    await expect(page).toHaveURL(/\/admin$/)
  })

  test('shows SSO sub-link when feature is on and user has settings:manage', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })
    await mockProviders(page, [])
    await page.goto('/admin')
    await page.getByRole('link', { name: /SSO providers/i }).click()
    await expect(page).toHaveURL(/\/admin\/sso\/providers/)
    await expect(page.getByText(/No SSO provider configured yet/i)).toBeVisible()
  })

  test('redirects when user is editor (lacks settings:manage)', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [editorGroup] })
    await mockFeatures(page, { 'sso.admin': true })
    // Dashboard APIs must be stubbed so an incidental redirect to "/" does not
    // hit the backend and produce a 401 that the axios interceptor re-routes to /login.
    await mockDashboard(page)
    await page.goto('/admin/sso/providers')
    // Providers gates on settings:manage; editor lacks it → redirect /admin.
    // Admin gates on users:manage; editor lacks it → redirect "/".
    // eslint-disable-next-line security/detect-unsafe-regex -- bounded literal pattern over Playwright-supplied page.url(), no user input; anchors and greedy \d+ make backtracking polynomial
    await expect(page).toHaveURL(/^http:\/\/localhost:\d+\/?(?:#.*)?$/)
  })

  // Regression test for the boot-time race: useFeature must not return a
  // stale "disabled" verdict before /api/features resolves, otherwise pages
  // that gate on `if (!enabled) <Navigate />` redirect away during the
  // initial fetch. The deliberate 300 ms delay below makes the race
  // deterministic.
  test('Providers does not redirect during /api/features initial load', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockProviders(page, [])
    await page.route('**/api/features', async (route) => {
      await new Promise((r) => setTimeout(r, 300))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ features: { 'sso.admin': true } }),
      })
    })

    await page.goto('/admin/sso/providers')
    await expect(page.getByText(/No SSO provider configured yet/i)).toBeVisible()
    await expect(page).toHaveURL(/\/admin\/sso\/providers$/)
  })

  test('ProviderEdit does not redirect during /api/features initial load', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await page.route('**/api/features', async (route) => {
      await new Promise((r) => setTimeout(r, 300))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ features: { 'sso.admin': true } }),
      })
    })

    await page.goto('/admin/sso/providers/new')
    await expect(page.getByTestId('provider-form')).toBeVisible()
    await expect(page).toHaveURL(/\/admin\/sso\/providers\/new$/)
  })
})

test.describe('SSO admin — providers list', () => {
  test('lists existing providers', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })
    await mockProviders(page, [sampleProvider])
    await page.goto('/admin/sso/providers')
    await expect(page.getByTestId('providers-table')).toBeVisible()
    await expect(page.getByText('Okta Corporate')).toBeVisible()
    await expect(page.getByText('okta-main')).toBeVisible()
  })

  test('toggles provider active state via PATCH', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })
    await mockProviders(page, [sampleProvider])

    let patchedActive: boolean | null = null
    await page.route('**/api/admin/sso/providers/okta-main/active', async (route) => {
      const body = JSON.parse(route.request().postData() ?? '{}')
      patchedActive = body.active
      await route.fulfill({ status: 204 })
    })

    await page.goto('/admin/sso/providers')
    // The checkbox is inside the provider row; click it to toggle
    await page.getByTestId('provider-row-okta-main').getByRole('checkbox').click()
    await expect.poll(() => patchedActive).not.toBeNull()
  })

  test('deletes a provider with confirmation', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })
    await mockProviders(page, [sampleProvider])

    let deleted = false
    await page.route('**/api/admin/sso/providers/okta-main', async (route) => {
      if (route.request().method() === 'DELETE') {
        deleted = true
        await route.fulfill({ status: 204 })
      } else {
        await route.continue()
      }
    })

    page.on('dialog', (d) => d.accept())
    await page.goto('/admin/sso/providers')
    await page.getByTestId('provider-row-okta-main').getByRole('button', { name: /delete/i }).click()
    await expect.poll(() => deleted).toBe(true)
  })
})

test.describe('SSO admin — provider create', () => {
  test('creates a SAML provider with metadata URL', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })

    let payload: Record<string, unknown> | null = null
    await page.route('**/api/admin/sso/providers', async (route) => {
      if (route.request().method() === 'POST') {
        payload = JSON.parse(route.request().postData() ?? '{}')
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ ...payload, created_at: '...', updated_at: '...' }),
        })
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
      }
    })

    await page.goto('/admin/sso/providers/new')
    await page.getByLabel(/Provider ID/i).fill('keycloak-test')
    await page.getByLabel(/Display name/i).fill('Keycloak Test')
    // MetadataInput renders both a radio ("Metadata URL") and a URL textbox with aria-label "Metadata URL".
    // Use getByRole to disambiguate and target the textbox specifically.
    await page.getByRole('textbox', { name: /Metadata URL/i }).fill('https://kc.example.com/realms/test/saml')
    await page.getByLabel(/SP entity ID/i).fill('https://app.example.com/api/auth/sso/keycloak-test/metadata')
    await page.getByRole('button', { name: /^save$/i }).click()

    await expect.poll(() => payload?.id).toBe('keycloak-test')
    await expect.poll(() => payload?.idp_metadata_url).toBe('https://kc.example.com/realms/test/saml')
    await expect(page).toHaveURL(/\/admin\/sso\/providers$/)
  })

  test('creates a SAML provider with metadata XML uploaded via file picker', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })

    let payload: Record<string, unknown> | null = null
    await page.route('**/api/admin/sso/providers', async (route) => {
      if (route.request().method() === 'POST') {
        payload = JSON.parse(route.request().postData() ?? '{}')
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify({ ...payload, created_at: '...', updated_at: '...' }),
        })
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
      }
    })

    await page.goto('/admin/sso/providers/new')
    await page.getByLabel(/Provider ID/i).fill('xml-test')
    await page.getByLabel(/Display name/i).fill('XML Test IdP')
    // Switch to XML mode by clicking the "Metadata XML" radio
    await page.getByRole('radio', { name: /Metadata XML/i }).check()

    const fixturePath = `${fixturesDir}/idp-metadata.xml`
    await page.getByLabel(/Choose XML file/i).setInputFiles(fixturePath)

    await page.getByLabel(/SP entity ID/i).fill('https://app.example.com/api/auth/sso/xml-test/metadata')
    await page.getByRole('button', { name: /^save$/i }).click()

    await expect.poll(() => payload?.idp_metadata_xml as string | undefined).toContain('EntityDescriptor')
    await expect.poll(() => payload?.idp_metadata_url).toBe('')
  })

  test('shows backend validation error inline (400)', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })

    await page.route('**/api/admin/sso/providers', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 400,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'validation_error', message: 'id must match ^[a-z0-9-]{1,64}$, got "Bad ID"' }),
        })
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([]),
        })
      }
    })

    await page.goto('/admin/sso/providers/new')
    await page.getByLabel(/Provider ID/i).fill('bad-id')
    await page.getByLabel(/Display name/i).fill('Bad')
    await page.getByRole('textbox', { name: /Metadata URL/i }).fill('https://kc.example.com')
    await page.getByLabel(/SP entity ID/i).fill('https://app.example.com/sp')
    await page.getByRole('button', { name: /^save$/i }).click()
    await expect(page.getByRole('alert')).toContainText(/id must match/i)
  })
})

test.describe('SSO admin — mappings', () => {
  test('CRUD mappings inline on edit page', async ({ loggedInPage: page }) => {
    await mockMe(page, { groups: [adminGroup] })
    await mockFeatures(page, { 'sso.admin': true })

    let mappings: Array<{ provider_id: string; idp_group: string; system_group: string; created_at: string }> = [
      { provider_id: 'okta-main', idp_group: 'magnify-admins', system_group: 'administrator', created_at: '...' },
    ]

    await page.route('**/api/admin/sso/providers/okta-main', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(sampleProvider),
        })
      } else {
        await route.continue()
      }
    })

    await page.route('**/api/admin/sso/providers/okta-main/mappings', async (route) => {
      const m = route.request().method()
      if (m === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(mappings),
        })
      } else if (m === 'POST') {
        const body = JSON.parse(route.request().postData() ?? '{}')
        const created = { provider_id: 'okta-main', ...body, created_at: '...' }
        mappings = [...mappings, created]
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(created),
        })
      } else if (m === 'DELETE') {
        const body = JSON.parse(route.request().postData() ?? '{}')
        mappings = mappings.filter(
          (x) => !(x.idp_group === body.idp_group && x.system_group === body.system_group),
        )
        await route.fulfill({ status: 204 })
      }
    })

    await page.goto('/admin/sso/providers/okta-main')
    await expect(page.getByText('magnify-admins')).toBeVisible()

    // Add a new mapping
    await page.getByLabel(/IdP group/i).fill('magnify-editors')
    await page.getByLabel(/System group/i).selectOption('editor')
    await page.getByRole('button', { name: /add mapping/i }).click()
    await expect(page.getByText('magnify-editors')).toBeVisible()

    // Delete the first mapping
    await page
      .getByRole('row', { name: /magnify-admins/i })
      .getByRole('button', { name: /delete/i })
      .click()
    await expect(page.getByText('magnify-admins')).not.toBeVisible()
  })
})
