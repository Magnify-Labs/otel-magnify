import { test, expect } from './fixtures'

const createdAt = '2026-01-01T00:00:00Z'

const requiredTemplates = [
  'Kubernetes OTLP to Grafana Tempo/Loki/Mimir',
  'Kubernetes OTLP to Datadog',
  'Logs to Loki',
  'Traces to Tempo',
  'Metrics to Prometheus remote write',
  'JVM services',
  'NGINX',
  'PostgreSQL',
  'Redis',
]

const templateVariables = [
  { name: 'endpoint', label: 'Endpoint', type: 'url', required: true, placeholder: '${OTLP_ENDPOINT}' },
  { name: 'headers', label: 'Headers', type: 'headers', required: false, placeholder: '${OTLP_AUTH_HEADER}' },
  { name: 'environment', label: 'Environment', type: 'string', required: true, placeholder: '${ENVIRONMENT}' },
  {
    name: 'resource_attributes',
    label: 'Resource attributes',
    type: 'map',
    required: false,
    placeholder: '${RESOURCE_ATTRIBUTES}',
  },
  { name: 'tls', label: 'TLS', type: 'boolean', required: false, placeholder: '${TLS_INSECURE}' },
]

function templateRow(id: string, name: string, category: string, stack: string) {
  const authPlaceholder = stack === 'datadog' ? '${DATADOG_API_KEY}' : '${OTLP_AUTH_HEADER}'

  return {
    id,
    name,
    content: `receivers:\n  otlp:\n    protocols:\n      grpc:\n        endpoint: \${OTLP_RECEIVER_ENDPOINT}\nexporters:\n  otlp:\n    endpoint: \${OTLP_ENDPOINT}\n    headers:\n      Authorization: ${authPlaceholder}\n    tls:\n      insecure: \${TLS_INSECURE}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [otlp]\n`,
    created_at: createdAt,
    created_by: 'otel-magnify',
    kind: 'template',
    status: 'ready',
    category,
    stack,
    description: `${name} starter template with safe placeholders only.`,
    variables: templateVariables,
    tags: [category, stack],
    built_in: true,
  }
}

test('Config Library exposes templates and can seed a new saved config without secret literals', async ({
  loggedInPage: page,
}) => {
  const oldSavedConfig = {
    id: 'cfg-old-row',
    name: 'legacy saved collector config',
    content: 'receivers:\n  otlp: {}\n',
    created_at: createdAt,
    created_by: 'operator@example.com',
  }
  const templateMetadata = [
    ['kubernetes', 'grafana'],
    ['kubernetes', 'datadog'],
    ['logs', 'loki'],
    ['traces', 'tempo'],
    ['metrics', 'prometheus'],
    ['services', 'jvm'],
    ['edge', 'nginx'],
    ['database', 'postgresql'],
    ['cache', 'redis'],
  ] as const
  const templates = requiredTemplates.map((name, index) =>
    templateRow(`tpl-${index}`, name, templateMetadata[index][0], templateMetadata[index][1]),
  )

  let createdPayload: unknown
  await page.route('**/api/configs', async (route) => {
    if (route.request().method() === 'POST') {
      createdPayload = route.request().postDataJSON()
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'cfg-created-from-template',
          name: 'Draft from Kubernetes OTLP to Datadog',
          content: templates[1].content,
          created_at: createdAt,
          created_by: 'test@example.com',
          kind: 'draft',
          status: 'draft',
        }),
      })
    }

    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([oldSavedConfig, ...templates]),
    })
  })

  await page.goto('/configs')

  await expect(page.getByRole('heading', { name: 'Config Library' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Saved configs/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /Templates/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /Drafts/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /Known-good references/ })).toBeVisible()
  await expect(page.getByText('legacy saved collector config')).toBeVisible()

  for (const templateName of requiredTemplates) {
    await expect(page.getByRole('heading', { name: templateName })).toBeVisible()
  }

  await expect(page.getByText('Endpoint', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Headers', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Environment', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Resource attributes', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('TLS', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('${OTLP_AUTH_HEADER}').first()).toBeVisible()

  const body = await page.locator('body').innerText()
  expect(body).not.toContain('super-secret-token')
  expect(body).not.toContain('dd_api_key_')
  expect(body).not.toContain('Bearer real')

  await page.getByRole('button', { name: 'Use template: Kubernetes OTLP to Datadog' }).click()
  await expect(page.getByLabel('Name')).toHaveValue('Draft from Kubernetes OTLP to Datadog')
  const datadogApiKeyPlaceholder = '${' + 'DATADOG_API_KEY' + '}'
  await expect(page.locator('.configs-form .cm-content')).toContainText(
    `Authorization: ${datadogApiKeyPlaceholder}`,
  )

  await page.getByRole('button', { name: 'Save draft config' }).click()
  await expect.poll(() => createdPayload).toMatchObject({
    name: 'Draft from Kubernetes OTLP to Datadog',
    content: expect.stringContaining('${DATADOG_API_KEY}'),
  })
})

test('Migration assistant previews vendor config and saves a draft without applying it', async ({
  loggedInPage: page,
}) => {
  const previewResponse = {
    schema_version: 'config_migration_preview.v1',
    vendor: 'datadog_agent',
    source_format: 'yaml',
    draft_yaml:
      'receivers:\n  otlp:\n    protocols:\n      grpc: {}\nexporters:\n  otlp:\n    endpoint: ${OTLP_ENDPOINT}\nservice:\n  pipelines:\n    logs:\n      receivers: [otlp]\n      exporters: [otlp]\n',
    draft_name: 'Migrated Datadog Agent draft',
    confidence: 'medium',
    summary: 'Mapped Datadog logs to an OTLP Collector draft.',
    warnings: [
      { code: 'partial', severity: 'warning', message: 'Review vendor-specific processors.' },
    ],
    unsupported_keys: [
      { path: 'apm_config.enabled', reason: 'APM settings need manual migration.' },
    ],
    evidence: [
      {
        source_path: 'logs_enabled',
        target_path: 'service.pipelines.logs',
        rule_id: 'datadog.logs.enabled',
        explanation: 'Datadog log collection maps to a Collector logs pipeline.',
      },
    ],
    redactions: [
      { path: 'api_key', placeholder: '${DATADOG_API_KEY}', reason: 'secret-like source value' },
    ],
    validation: {
      valid: true,
      overall_status: 'ok',
      summary: 'Collector YAML is syntactically valid.',
      validated_at: '2026-07-04T12:00:00Z',
    },
    save_hint: {
      kind: 'draft',
      source_type: 'migration_assistant',
      tags: ['migration', 'datadog_agent'],
      category: 'migration',
      stack: 'datadog',
    },
  }

  const configs = [
    {
      id: 'cfg-existing',
      name: 'existing saved config',
      content: 'receivers:\n  otlp: {}\n',
      created_at: createdAt,
      created_by: 'operator@example.com',
      kind: 'saved',
      status: 'ready',
    },
  ]
  let previewPayload: unknown
  let createdPayload: unknown

  await page.route('**/api/configs/migration-assistant/preview', async (route) => {
    previewPayload = route.request().postDataJSON()
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(previewResponse),
    })
  })
  await page.route('**/api/configs', async (route) => {
    if (route.request().method() === 'POST') {
      createdPayload = route.request().postDataJSON()
      configs.push({
        id: 'cfg-migration-draft',
        name: previewResponse.draft_name,
        content: previewResponse.draft_yaml,
        created_at: createdAt,
        created_by: 'test@example.com',
        kind: 'draft',
        status: 'draft',
      })
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify(configs[configs.length - 1]),
      })
    }

    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(configs),
    })
  })

  await page.goto('/configs')

  await expect(page.getByRole('heading', { name: 'Convert vendor snippets into Collector drafts' })).toBeVisible()
  await expect(page.getByText('Preview only · no auto-apply')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Datadog Agent' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Fluent Bit' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Splunk forwarder' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'New Relic infra' })).toBeVisible()

  await page.getByRole('button', { name: 'Preview migration' }).click()

  await expect.poll(() => previewPayload).toMatchObject({
    schema_version: 'config_migration_preview_request.v1',
    vendor: 'datadog_agent',
    source_format: 'yaml',
    context: { target_exporter: 'otlp', otlp_endpoint: '${OTLP_ENDPOINT}' },
  })
  await expect(page.getByRole('heading', { name: 'Migrated Datadog Agent draft' })).toBeVisible()
  await expect(page.getByText('Medium confidence')).toBeVisible()
  await expect(page.getByText('Collector YAML is syntactically valid.')).toBeVisible()
  await expect(page.getByText('Review vendor-specific processors.')).toBeVisible()
  await expect(page.getByText('apm_config.enabled: APM settings need manual migration.')).toBeVisible()
  await expect(page.getByText('api_key: secret-like source value')).toBeVisible()
  await expect(page.getByText(/logs_enabled → service\.pipelines\.logs/)).toBeVisible()

  await page.getByRole('button', { name: 'Save as draft' }).click()
  await expect.poll(() => createdPayload).toMatchObject({
    name: 'Migrated Datadog Agent draft',
    content: expect.stringContaining('service:'),
    kind: 'draft',
    status: 'draft',
    source_type: 'migration_assistant',
    category: 'migration',
    stack: 'datadog',
    tags: ['migration', 'datadog_agent'],
  })
  await expect(
    page.getByText('Saved Migrated Datadog Agent draft as a migration-assistant draft.'),
  ).toBeVisible()
})
