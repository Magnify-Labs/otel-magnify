import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'

interface ConfigStub {
  id: string
  name: string
  content: string
  created_at: string
  created_by: string
}

function buildConfig(overrides: Partial<ConfigStub> = {}): ConfigStub {
  return {
    id: 'cfg-prod-eu',
    name: 'collector-prod-eu',
    content: 'receivers:\n  otlp: {}\n',
    created_at: '2026-07-09T10:00:00.000Z',
    created_by: 'tester@example.com',
    ...overrides,
  }
}

async function mockConfigsAPI(page: Page, initialConfigs: ConfigStub[]) {
  const configs = [...initialConfigs]

  await page.route('**/api/configs', async (route) => {
    const request = route.request()

    if (request.method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(configs),
      })
      return
    }

    if (request.method() === 'POST') {
      const body = request.postDataJSON() as { name: string; content: string }
      const created = buildConfig({
        id: 'cfg-created',
        name: body.name,
        content: body.content,
        created_at: '2026-07-09T10:05:00.000Z',
      })
      configs.unshift(created)

      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(created),
      })
      return
    }

    await route.fulfill({ status: 405 })
  })
}

test('configs page creates configs from a dedicated tab and returns to the saved list', async ({
  loggedInPage: page,
}) => {
  let createdPayload: unknown
  await mockConfigsAPI(page, [buildConfig()])
  await page.route('**/api/configs', async (route) => {
    const request = route.request()

    if (request.method() === 'GET') {
      await route.fallback()
      return
    }

    createdPayload = request.postDataJSON()
    await route.fallback()
  })

  await page.goto('/configs')

  await expect(page.getByRole('tab', { name: 'Saved configs' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByText('collector-prod-eu')).toBeVisible()
  await expect(page.getByLabel('Name')).toHaveCount(0)
  await expect(page.getByText(/Migration assistant/i)).toHaveCount(0)
  await expect(page.getByText(/Assistant de migration/i)).toHaveCount(0)
  await expect(page.getByText(/Convert vendor snippets/i)).toHaveCount(0)
  await expect(page.getByText(/Convertir des snippets vendor/i)).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Preview migration/i })).toHaveCount(0)

  await page.getByRole('tab', { name: 'New config' }).click()
  await expect(page.getByRole('tab', { name: 'New config' })).toHaveAttribute(
    'aria-selected',
    'true',
  )

  await page.getByLabel('Name').fill('collector-staging')
  await page.locator('.cm-content').first().click()
  await page.keyboard.type('receivers:\n  otlp: {}\nservice:\n  pipelines: {}\n')
  await page.getByRole('button', { name: 'Create' }).click()

  await expect(page.getByRole('tab', { name: 'Saved configs' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect
    .poll(() => createdPayload)
    .toMatchObject({
      name: 'collector-staging',
      content: expect.stringContaining('receivers:'),
      kind: 'saved',
      status: 'ready',
      source_type: 'manual',
    })
  await expect(page.getByText('collector-staging')).toBeVisible()
  await expect(page.getByLabel('Name')).toHaveCount(0)
})

test('configs page shows a load error instead of an empty saved list', async ({
  loggedInPage: page,
}) => {
  await page.route('**/api/configs', (route) =>
    route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'boom' }),
    }),
  )

  await page.goto('/configs')

  await expect(page.getByRole('tab', { name: 'Saved configs' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByText('Failed to load configs')).toBeVisible()
  await expect(page.getByText('No configurations yet')).toHaveCount(0)
})

test('config create errors stay on the new config tab and preserve the draft', async ({
  loggedInPage: page,
}) => {
  await page.route('**/api/configs', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      })
      return
    }

    await route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'create_failed' }),
    })
  })

  await page.goto('/configs')
  await page.getByRole('tab', { name: 'New config' }).click()
  await page.getByLabel('Name').fill('broken-config')
  await page.locator('.cm-content').first().click()
  await page.keyboard.type('receivers:\n  otlp: {}\n')
  await page.getByRole('button', { name: 'Create' }).click()

  await expect(page.getByRole('tab', { name: 'New config' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByText('create_failed')).toBeVisible()
  await expect(page.getByLabel('Name')).toHaveValue('broken-config')
  await expect(page.locator('.cm-content').first()).toContainText('receivers')
})
