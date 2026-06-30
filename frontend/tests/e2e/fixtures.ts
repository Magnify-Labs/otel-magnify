import { test as base, expect, type Page } from '@playwright/test'

interface MeStub {
  id?: string
  email?: string
  groups?: Array<{
    id: string
    name: 'viewer' | 'editor' | 'administrator'
    role: 'viewer' | 'editor' | 'administrator'
    is_system: boolean
    created_at: string
  }>
  preferences?: {
    user_id: string
    theme: 'light' | 'dark' | 'system'
    language: 'en' | 'fr'
    updated_at: string
  }
}

function buildMe(stub: MeStub) {
  return {
    id: stub.id ?? 'u-test',
    email: stub.email ?? 'test@example.com',
    groups: stub.groups ?? [
      {
        id: 'grp_system_viewer',
        name: 'viewer',
        role: 'viewer',
        is_system: true,
        created_at: new Date().toISOString(),
      },
    ],
    preferences: stub.preferences ?? {
      user_id: stub.id ?? 'u-test',
      theme: 'system',
      language: 'en',
      updated_at: new Date().toISOString(),
    },
  }
}

// Override the default /api/me mock for a given test with a custom stub.
// Playwright runs handlers in reverse registration order, so this overrides
// the fixture's default mock installed in loggedInPage.
export async function mockMe(page: Page, stub: MeStub) {
  const me = buildMe(stub)
  await page.route('**/api/me**', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(me),
    }),
  )
  return me
}

// Logged-in page fixture: stubs a JWT in localStorage and installs a default
// /api/me mock so that AppShell's boot-time hydration doesn't hit the backend
// (which would 401 and trigger the axios interceptor's redirect to /login).
export const test = base.extend<{ loggedInPage: Page }>({
  loggedInPage: async ({ page }, use) => {
    await page.addInitScript(() => {
      localStorage.setItem('token', 'test.token.stub')
    })
    const defaultMe = buildMe({})
    await page.route('**/api/me**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(defaultMe),
      }),
    )
    await use(page)
  },
})

export { expect }
