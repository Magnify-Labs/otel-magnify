import { test, expect } from './fixtures'

const createdAt = '2026-01-01T00:00:00Z'

test('configs page keeps the library focused on saved configs and hides disabled assistants', async ({
  loggedInPage: page,
}) => {
  await page.route('**/api/configs', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: 'cfg-saved-row',
          name: 'legacy saved collector config',
          content: 'receivers:\n  otlp: {}\n',
          created_at: createdAt,
          created_by: 'operator@example.com',
          kind: 'saved',
          status: 'ready',
        },
        {
          id: 'tpl-hidden-row',
          name: 'Kubernetes OTLP to Datadog',
          content: 'receivers:\n  otlp: {}\n',
          created_at: createdAt,
          created_by: 'otel-magnify',
          kind: 'template',
          status: 'ready',
        },
        {
          id: 'draft-hidden-row',
          name: 'Migrated Datadog Agent draft',
          content: 'receivers:\n  otlp: {}\n',
          created_at: createdAt,
          created_by: 'otel-magnify',
          kind: 'draft',
          status: 'draft',
        },
      ]),
    }),
  )

  await page.goto('/configs')

  await expect(page.getByRole('tab', { name: 'Saved configs' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(page.getByRole('tab', { name: 'New config' })).toBeVisible()
  await expect(page.getByText('legacy saved collector config')).toBeVisible()
  await expect(page.getByText('Kubernetes OTLP to Datadog')).toHaveCount(0)
  await expect(page.getByText('Migrated Datadog Agent draft')).toHaveCount(0)

  await expect(page.getByText(/Migration assistant/i)).toHaveCount(0)
  await expect(page.getByText(/Assistant de migration/i)).toHaveCount(0)
  await expect(page.getByText(/Convert vendor snippets/i)).toHaveCount(0)
  await expect(page.getByText(/Convertir des snippets vendor/i)).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Preview migration/i })).toHaveCount(0)
})

test('new config creation persists saved config metadata', async ({ loggedInPage: page }) => {
  let createdPayload: unknown

  await page.route('**/api/configs', async (route) => {
    if (route.request().method() === 'POST') {
      createdPayload = route.request().postDataJSON()
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'cfg-created',
          name: 'collector-prod-baseline',
          content: 'receivers:\n  otlp: {}\n',
          created_at: createdAt,
          created_by: 'test@example.com',
          kind: 'saved',
          status: 'ready',
          source_type: 'manual',
        }),
      })
    }

    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([]),
    })
  })

  await page.goto('/configs')
  await page.getByRole('tab', { name: 'New config' }).click()
  await page.getByLabel('Name').fill('collector-prod-baseline')
  await page.locator('.cm-content').first().click()
  await page.keyboard.type('receivers:\n  otlp: {}\n')
  await page.getByRole('button', { name: 'Create' }).click()

  await expect
    .poll(() => createdPayload)
    .toMatchObject({
      name: 'collector-prod-baseline',
      content: expect.stringContaining('receivers:'),
      kind: 'saved',
      status: 'ready',
      source_type: 'manual',
    })
  await expect(page.getByRole('tab', { name: 'Saved configs' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
})
