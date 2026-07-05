import {
  buildCollectorWorkload,
  mockPushGroups,
  mockPushPreview,
  pushPreviewScenarios,
  test,
  expect,
  mockMe,
  mockFeatures,
} from './fixtures'
import type { Page } from '@playwright/test'
import { safeRemoteErrorText } from '../../src/lib/safeRemoteErrorText'

const WORKLOAD_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const ACTIVE_CONFIG_ID = 'abc123'

const editorGroup = {
  id: 'grp_system_editor',
  name: 'editor' as const,
  role: 'editor' as const,
  is_system: true,
  created_at: new Date().toISOString(),
}

const viewerGroup = {
  id: 'grp_system_viewer',
  name: 'viewer' as const,
  role: 'viewer' as const,
  is_system: true,
  created_at: new Date().toISOString(),
}

const PRO_CONFIG_SAFETY_FEATURES = {
  'config_safety.approvals': true,
  'config_safety.break_glass': true,
  'config_safety.canary_rollout': true,
  'config_safety.scoped_push': true,
  'config_safety.gitops_export': true,
  'config_safety.policy_preview': true,
}

async function mockProConfigSafetyFeatures(
  page: Page,
  overrides: Record<string, boolean> = {},
) {
  await mockFeatures(page, { ...PRO_CONFIG_SAFETY_FEATURES, ...overrides })
}

function mockWorkload(page: Page, overrides: Record<string, unknown> = {}) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(buildCollectorWorkload({ id: WORKLOAD_ID, ...overrides })),
    }),
  )
}

function mockWorkloadsList(page: Page) {
  return page.route(/\/api\/workloads(?:\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([buildCollectorWorkload({ id: WORKLOAD_ID })]),
    }),
  )
}

function mockConfigDiff(page: Page) {
  return page.route('**/api/configs/diff', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 'otel-config-diff.v1',
        valid: true,
        summary: {
          overall_risk: 'low',
          headline: 'No risky changes detected',
          counts: {
            components_added: 0,
            components_removed: 0,
            components_modified: 0,
            pipelines_added: 0,
            pipelines_removed: 0,
            pipelines_modified: 0,
            endpoints_added: 0,
            endpoints_removed: 0,
            endpoints_modified: 0,
            high_risk: 0,
            medium_risk: 0,
            low_risk: 0,
          },
        },
        blast_radius: {
          schema_version: 'otel-config-blast-radius.v1',
          affected_signals: [],
          touched_exporters: [],
          impacted_services: [],
          impacted_clusters: [],
          critical_collectors: [],
        },
        components: [],
        pipelines: [],
        endpoints: [],
        security: [],
        risk_items: [],
        diagnostics: [],
        normalized: {
          base_hash: 'base',
          target_hash: 'target',
          base_component_count: 0,
          target_component_count: 0,
          base_pipeline_count: 0,
          target_pipeline_count: 0,
        },
      }),
    }),
  )
}

function mockConfig(page: Page, content: string, overrides: Record<string, unknown> = {}) {
  return page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: ACTIVE_CONFIG_ID,
        name: 'current',
        content,
        created_at: new Date().toISOString(),
        created_by: 'test',
        ...overrides,
      }),
    }),
  )
}

function mockHistory(page: Page, rows: unknown[]) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/configs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(rows),
    }),
  )
}

function mockKnownGoodMissing(page: Page) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/known-good`, (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'known-good config not found' }),
    }),
  )
}

function mockValidate(page: Page, result: Record<string, unknown> & { valid: boolean }) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/config/validate`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(result),
    }),
  )
}

function buildPolicy(overrides: Record<string, unknown> = {}) {
  return {
    schema_version: 'config-policy.v1',
    valid: true,
    allowed: true,
    decision: 'pass',
    severity: 'info',
    target: { workload_id: WORKLOAD_ID, environment: 'production' },
    settings: {},
    findings: [],
    summary: { pass_count: 0, warn_count: 0, block_count: 0 },
    audit: { persisted: false },
    ...overrides,
  }
}

function buildPlan(overrides: Record<string, unknown> = {}) {
  return {
    schema_version: 'config_application_plan.v1',
    workload_id: WORKLOAD_ID,
    config_hash: 'feedfacefeedface',
    summary: {
      target_count: 1,
      collector_target_count: 1,
      remote_config_capable_count: 1,
      read_only_count: 0,
      validation_ok_count: 1,
      validation_failed_count: 0,
      components_missing_count: 0,
      high_risk_change_count: 0,
      excluded_count: 0,
    },
    targets: [
      {
        workload_id: WORKLOAD_ID,
        display_name: 'test-collector',
        type: 'collector',
        accepts_remote_config: true,
        read_only: false,
        validation_status: 'ok',
        validation_errors: [],
        components_missing_count: 0,
        high_risk_change_count: 0,
        excluded: false,
        exclusion_reasons: [],
        hard_failures: [],
        active_config_hash: 'abc123',
        active_config_unavailable: false,
      },
    ],
    hard_failures: [],
    can_push: true,
    apply_allowed: true,
    policy: buildPolicy(),
    export: {
      supported: true,
      formats: ['json', 'markdown'],
      json_endpoint: `/api/workloads/${WORKLOAD_ID}/config/plan/export?format=json`,
      markdown_endpoint: `/api/workloads/${WORKLOAD_ID}/config/plan/export?format=markdown`,
      persisted_rollout: 'not_persisted',
    },
    ...overrides,
  }
}

function mockPlan(page: Page, plan: ReturnType<typeof buildPlan>) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/config/plan`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(plan),
    }),
  )
}

function mockPlanExport(page: Page) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/config/plan/export?format=markdown`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/markdown',
      headers: { 'Content-Disposition': 'attachment; filename="config-safety-plan.md"' },
      body: '# Config Safety Plan\n\n- Targets: 1\n',
    }),
  )
}

async function generateSafetyPlan(page: Page) {
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()
  await expect(page.locator('.config-application-plan')).toContainText('Ready to push')
}

const VALIDATION_CHECKS_SUCCESS = [
  {
    id: 'yaml_static',
    label: 'YAML syntax',
    source: 'server.static_yaml',
    status: 'passed',
    required: true,
    messages: [{ code: 'yaml_parse_ok', severity: 'info', message: 'YAML parsed successfully.' }],
  },
  {
    id: 'collector_structure',
    label: 'Collector structure',
    source: 'server.structure',
    status: 'passed',
    required: true,
    messages: [
      {
        code: 'collector_structure_ok',
        severity: 'info',
        message: 'Pipelines reference defined components.',
      },
    ],
  },
  {
    id: 'component_availability',
    label: 'Components available on workload',
    source: 'workload.available_components',
    status: 'passed',
    required: true,
    messages: [
      {
        code: 'components_available',
        severity: 'info',
        message: 'All referenced components are available on this workload.',
      },
    ],
  },
  {
    id: 'otelcol_runtime',
    label: 'Collector runtime validation',
    source: 'otelcol.binary',
    status: 'passed',
    required: false,
    metadata: {
      binary_version: '0.150.1',
      target_version: '0.150.1',
      binary_path: '/usr/local/bin/otelcol',
    },
    messages: [
      { code: 'otelcol_validate_ok', severity: 'info', message: 'otelcol validate succeeded.' },
    ],
  },
]

test.beforeEach(async ({ loggedInPage: page }) => {
  await mockMe(page, { groups: [editorGroup] })
  await mockWorkloadsList(page)
  await mockConfigsList(page, [])
  await mockConfigDiff(page)
  await mockKnownGoodMissing(page)
  await mockApprovalList(page)
})

const PUSH_STATUS_LABEL_CASES = [
  ['pending', 'Pending'],
  ['validated', 'Validated'],
  ['submitted', 'Submitted'],
  ['sent', 'Sent via OpAMP'],
  ['applying', 'Applying'],
  ['applied', '✓ Applied'],
  ['failed', '✗ Failed'],
  ['rollback_started', 'Rolling back'],
  ['rollback_applied', 'Rolled back'],
  ['rollback_failed', 'Rollback failed'],
] as const

test('imports a config from Git and displays sanitized provenance', async ({ loggedInPage: page }) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  let importPayload: unknown = null
  await page.route('**/api/configs/import/git', async (route, request) => {
    importPayload = request.postDataJSON()
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        config: {
          id: 'git-config-hash',
          name: 'prod collector from git',
          content: 'receivers:\n  otlp: {}\nexporters:\n  debug: {}\n',
          created_at: '2026-07-01T12:00:00Z',
          created_by: 'test@example.com',
          source_type: 'git',
          git_url: 'https://github.com/acme/collectors.git',
          git_provider: 'github',
          git_ref: 'main',
          git_path: 'otel/prod.yaml',
          commit_sha: '1234567890abcdef1234567890abcdef12345678',
          imported_at: '2026-07-01T12:34:56Z',
        },
        validation: { valid: true },
      }),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Import from Git' }).click()
  await page.getByLabel('Git config name').fill('prod collector from git')
  await page.getByLabel('Git repository URL').fill('https://token:secret@github.com/acme/collectors.git')
  await page.getByLabel('Git ref').fill('main')
  await page.getByLabel('Git file path').fill('otel/prod.yaml')
  await page.getByRole('button', { name: 'Import Git config' }).click()

  await expect.poll(() => importPayload).toEqual({
    name: 'prod collector from git',
    git_url: 'https://token:secret@github.com/acme/collectors.git',
    git_ref: 'main',
    git_path: 'otel/prod.yaml',
  })
  const provenance = page.locator('.config-provenance-card').first()
  await expect(provenance).toContainText('Git provenance')
  await expect(provenance).toContainText('github')
  await expect(provenance).toContainText('https://github.com/acme/collectors.git')
  await expect(provenance).not.toContainText('token:secret')
  await expect(provenance).toContainText('main')
  await expect(provenance).toContainText('otel/prod.yaml')
  await expect(provenance.getByTitle('1234567890abcdef1234567890abcdef12345678')).toContainText(
    '12345678',
  )
  await expect(page.locator('.validation-ok')).toContainText('valid')
})

test('export as PR stays disabled until Git provider and validation gates pass', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n', {
    source_type: 'git',
    git_url: 'https://github.com/acme/collectors.git',
    git_provider: 'github',
    git_ref: 'main',
    git_path: 'otel/prod.yaml',
    commit_sha: '1234567890abcdef1234567890abcdef12345678',
    imported_at: '2026-07-01T12:34:56Z',
  })
  await mockHistory(page, [])
  await mockValidate(page, { valid: true, checks: VALIDATION_CHECKS_SUCCESS })
  await mockPlan(page, buildPlan())

  let exportPayload: unknown = null
  await page.route('**/api/configs/abc123/export/git', async (route, request) => {
    exportPayload = request.postDataJSON()
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        result: {
          provider: 'github',
          url: 'https://github.com/acme/collectors/pull/42',
          number: 42,
          branch: 'otel-magnify/prod-update',
          commit_sha: 'abcdefabcdefabcdefabcdefabcdefabcdefabcd',
        },
        comment: {
          provider: 'github',
          url: 'https://github.com/acme/collectors/pull/42#issuecomment-1',
          comment_id: '1',
        },
        validation: { valid: true },
      }),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Export as PR/MR' })).toBeDisabled()
  await expect(page.getByText('Validate and generate a safety plan before exporting to Git.')).toBeVisible()

  await generateSafetyPlan(page)
  await page.getByLabel('Provider').selectOption('github')
  await page.getByLabel('Repository').fill('acme/collectors')
  await page.getByLabel('Target path').fill('otel/prod.yaml')
  await page.getByLabel('Base branch').fill('main')
  await page.getByLabel('Export branch').fill('otel-magnify/prod-update')
  await page.getByLabel('PR/MR title').fill('Update collector config')
  await page.getByRole('button', { name: 'Export as PR/MR' }).click()

  await expect.poll(() => exportPayload).toEqual({
    provider: 'github',
    repository: 'acme/collectors',
    path: 'otel/prod.yaml',
    base_branch: 'main',
    branch: 'otel-magnify/prod-update',
    title: 'Update collector config',
    body: '',
  })
  await expect(page.getByRole('link', { name: 'Open PR/MR' })).toHaveAttribute(
    'href',
    'https://github.com/acme/collectors/pull/42',
  )
})

test('read-only collectors compare Git expected provenance with OpAMP effective config', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page, { accepts_remote_config: false, active_config_hash: 'effec7edcafebabe' })
  await mockConfig(page, 'receivers:\n  otlp: {}\n', {
    source_type: 'git',
    git_url: 'https://gitlab.com/acme/collectors.git',
    git_provider: 'gitlab',
    git_ref: 'release/2026-07',
    git_path: 'otel/readonly.yaml',
    commit_sha: 'fedcba9876543210fedcba9876543210fedcba98',
    imported_at: '2026-07-02T08:15:00Z',
  })
  await mockHistory(page, [])
  await mockPlan(page, buildPlan({ summary: { ...buildPlan().summary, read_only_count: 1 } }))

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.getByText('Git expected vs OpAMP effective')).toBeVisible()
  await expect(page.locator('.config-provenance-card')).toContainText('gitlab')
  await expect(page.locator('.config-provenance-card')).toContainText('fedcba98')
  await expect(page.getByText('OpAMP effective hash')).toBeVisible()
  await expect(page.getByText('effec7ed')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Push config' })).toHaveCount(0)
})

test('push status labels render every backend status', async ({ loggedInPage: page }) => {
  let currentStatus: (typeof PUSH_STATUS_LABEL_CASES)[number][0] = 'pending'
  await mockWorkload(page, {
    get current_config_push() {
      return {
        push_id: `push_${currentStatus}`,
        workload_id: WORKLOAD_ID,
        config_id: 'feedfacefeedface',
        config_hash: 'feedfacefeedface',
        status: currentStatus,
        submitted_at: '2026-06-30T10:12:00Z',
        sent_at: '2026-06-30T10:12:01Z',
        updated_at: '2026-06-30T10:12:05Z',
        target_count: 1,
        applied_count: currentStatus === 'applied' || currentStatus === 'rollback_applied' ? 1 : 0,
        failed_count: currentStatus === 'failed' || currentStatus === 'rollback_failed' ? 1 : 0,
        pending_count:
          currentStatus === 'applied' ||
          currentStatus === 'failed' ||
          currentStatus.startsWith('rollback_')
            ? 0
            : 1,
      }
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  for (const [status, label] of PUSH_STATUS_LABEL_CASES) {
    currentStatus = status
    await page.goto('/login')
    await page.goto(`/workloads/${WORKLOAD_ID}`)
    await expect(page.locator('.push-banner-label')).toHaveText(label)
  }
})

test('edit button enables YAML editing (regression)', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Validate for this collector' })).toBeVisible()

  // The draft editor is the second `.cm-content` after Edit is clicked? Actually
  // when entering edit mode, only the draft editor remains (readOnly one unmounts).
  const editor = page.locator('.cm-content').first()
  await editor.click()
  await page.keyboard.type('# edited')
  await expect(editor).toContainText('# edited')
})

test('config safety explains the supervised collector flow before the editor', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const safety = page.locator('.config-safety-card')
  await expect(safety).toBeVisible()
  await expect(safety).toContainText('Know what will change before it reaches this workload.')
  await expect(safety.locator('.config-safety-step-title')).toHaveText([
    'Validate before push',
    'Compare with active config',
    'Push safely',
    'Roll back to known-good',
  ])
  await expect(safety).toContainText('Validation required')
  await expect(safety).toContainText('Diff available')
  await expect(safety).toContainText('Validate the configuration first.')
  await expect(safety).toContainText('History available')

  const safetyTop = await safety.boundingBox()
  const configTop = await page.getByText('Configuration', { exact: true }).boundingBox()
  expect(safetyTop?.y).toBeLessThan(configTop?.y ?? Number.POSITIVE_INFINITY)
})

test('viewer sees restricted config content while metadata and history remain available', async ({
  loggedInPage: page,
}) => {
  await mockMe(page, { groups: [viewerGroup] })
  await mockWorkload(page)
  await page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) =>
    route.fulfill({
      status: 403,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'forbidden: secret-yaml SECRET_TOKEN' }),
    }),
  )
  await mockHistory(page, [
    {
      workload_id: WORKLOAD_ID,
      config_id: 'hash-viewer-safe-metadata',
      applied_at: '2026-07-04T12:00:00Z',
      status: 'applied',
      pushed_by: 'admin@example.com',
      label: 'safe metadata',
      content_available: false,
    },
  ])
  await mockConfigsList(page, [{ id: 'cfg-safe', name: 'metadata-only saved config' }])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(
    page.getByText('Config content is restricted by role. Metadata and history remain available.'),
  ).toBeVisible()
  await expect(page.getByText('Content restricted')).toBeVisible()
  await expect(page.getByText('Push history', { exact: true })).toBeVisible()
  await expect(page.getByText('safe metadata')).toBeVisible()
  await expect(page.getByText('hash-vie')).toBeVisible()
  await expect(page.locator('select.apply-config-select')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Compare revisions' })).toBeDisabled()
  await expect(page.locator('body')).not.toContainText('secret-yaml')
  await expect(page.locator('body')).not.toContainText('SECRET_TOKEN')
})

test('workload detail stays readable without horizontal overflow on mobile', async ({
  loggedInPage: page,
}) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const viewport = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }))
  expect(viewport.scrollWidth).toBeLessThanOrEqual(viewport.clientWidth)

  const safety = page.locator('.config-safety-card')
  await expect(safety).toBeVisible()
  const safetyBox = await safety.boundingBox()
  expect(safetyBox?.width).toBeGreaterThan(320)

  const firstStep = await safety.locator('.config-safety-step').nth(0).boundingBox()
  const secondStep = await safety.locator('.config-safety-step').nth(1).boundingBox()
  expect(secondStep?.y).toBeGreaterThan((firstStep?.y ?? 0) + 20)
})

test('config safety shows read-only collector caveat and no enabled push CTA', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, { accepts_remote_config: false })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])
  await mockPlan(page, buildPlan({ can_push: false, apply_allowed: false }))

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const safety = page.locator('.config-safety-card')
  await expect(safety).toBeVisible()
  await expect(safety).toContainText('Read-only collector')
  await expect(safety).toContainText(
    'Push is unavailable because this collector only reports config.',
  )
  await expect(safety).not.toContainText('Push validated config')
})

test('config safety is hidden for SDK workload detail pages', async ({ loggedInPage: page }) => {
  await mockWorkload(page, {
    type: 'sdk',
    active_config_id: undefined,
    accepts_remote_config: false,
    available_components: undefined,
    labels: { 'service.name': 'demo-app' },
  })
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('.config-safety-card')).toHaveCount(0)
})

test('validate exposes errors and blocks push', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: false,
    errors: [
      {
        code: 'undefined_component',
        message: 'pipeline "traces" references exporter "nope"',
        path: 'service.pipelines.traces.exporters[0]',
      },
    ],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type('bad: yaml')
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()

  await expect(page.locator('.validation-errors')).toContainText('undefined_component')
  // Approval flow remains unavailable until validation succeeds and a plan is generated.
  await expect(page.getByRole('button', { name: 'Request approval' })).toHaveCount(0)
})

test('valid config shows plan counters before approval request', async ({ loggedInPage: page }) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type(' # touched')
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()

  await expect(page.locator('.validation-ok')).toContainText('valid')
  await expect(page.getByRole('button', { name: 'Generate safety plan' })).toBeEnabled()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()

  const plan = page.locator('.config-application-plan')
  await expect(plan).toBeVisible()
  await expect(plan).toContainText('Target collectors')
  await expect(plan).toContainText('Remote-config capable')
  await expect(plan).toContainText('Validation OK')
  await expect(plan).toContainText('High-risk changes')
  await expect(plan).toContainText('test-collector')
  await expect(plan).toContainText('Policy allowed')
  await expect(plan).toContainText('No policy findings')
  await expect(page.getByRole('button', { name: 'Request approval' })).toBeDisabled()
  await page.getByLabel('Approval request comment').fill('please review prod change')
  await page.getByLabel('I acknowledge this targets production').check()
  await expect(page.getByRole('button', { name: 'Request approval' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Push approved config' })).toHaveCount(0)
})

test('safety plan shows high risk score and redacted reasons before approval actions', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(
    page,
    buildPlan({
      summary: {
        ...buildPlan().summary,
        target_count: 48,
        collector_target_count: 48,
        high_risk_change_count: 3,
      },
      risk_score: {
        severity: 'high',
        applies_to_count: 48,
        reasons: [
          'Removes logs pipeline service.pipelines.logs.',
          'Changes OTLP endpoint from https://otel.example.com to https://secret-token@evil.example.com.',
          'Disables memory_limiter processor.',
        ],
      },
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()

  const riskPanel = page.getByRole('region', { name: 'Pre-push risk score' })
  await expect(riskPanel).toBeVisible()
  await expect(riskPanel).toContainText('Risk: High')
  await expect(riskPanel).toContainText('Applies to 48 target collectors')
  await expect(riskPanel.locator('li')).toHaveText([
    'Removes logs pipeline service.pipelines.logs.',
    'Changes OTLP endpoint from https://otel.example.com to https://••••masked••••@evil.example.com.',
    'Disables memory_limiter processor.',
  ])
  await expect(riskPanel).not.toContainText('secret-token')

  const approvalTop = await page.locator('.config-approval-panel').boundingBox()
  const riskTop = await riskPanel.boundingBox()
  expect(riskTop?.y).toBeLessThan(approvalTop?.y ?? Number.POSITIVE_INFINITY)
})

function approvalRequest(overrides: Record<string, unknown> = {}) {
  const now = '2026-06-30T10:15:00.000Z'
  return {
    id: 'approval-1',
    workload_id: WORKLOAD_ID,
    draft_yaml: 'receivers:\n  otlp: {}\n # touched',
    target_group: 'single',
    target_env: 'prod',
    status: 'pending',
    requested_by: 'operator@example.com',
    requested_at: now,
    request_comment: 'please review prod change',
    prod_confirmation: true,
    ...overrides,
  }
}

function mockApprovalList(page: Page, approvals: unknown[] = []) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals`, async (route, request) => {
    if (request.method() === 'GET') {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(approvals) })
      return
    }
    await route.continue()
  })
}

async function prepareApprovedDraft(page: Page) {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockApprovalList(page)

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type(' # touched')
  await page.getByRole('button', { name: 'Validate for this collector' }).click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()
}

async function requestAndApproveDraft(page: Page) {
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals`, async (route, request) => {
    if (request.method() !== 'POST') return route.continue()
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify(approvalRequest()),
    })
  })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/approve`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(
        approvalRequest({
          status: 'approved',
          approved_by: 'alice@example.com',
          approved_at: '2026-06-30T10:16:00.000Z',
          approval_comment: 'approved after review',
        }),
      ),
    }),
  )

  await page.getByLabel('Approval request comment').fill('please review prod change')
  await page.getByLabel('I acknowledge this targets production').check()
  await page.getByRole('button', { name: 'Request approval' }).click()
  await page.getByLabel('Approval comment').fill('approved after review')
  await page.getByRole('button', { name: 'Approve request' }).click()
  await expect(page.locator('.config-approval-panel')).toContainText('Approved by alice@example.com')
}

test('blocked policy finding is visible and gates approval request', async ({ loggedInPage: page }) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(
    page,
    buildPlan({
      hard_failures: ['config_policy_blocked'],
      can_push: false,
      apply_allowed: false,
      policy: buildPolicy({
        allowed: false,
        decision: 'block',
        severity: 'critical',
        summary: { pass_count: 0, warn_count: 0, block_count: 1 },
        findings: [
          {
            policy_id: 'community',
            policy_name: 'Community config policy',
            rule_id: 'community.production.insecure_tls',
            rule_code: 'production.insecure_tls',
            severity: 'critical',
            decision: 'block',
            target_scope: 'collector',
            environment: 'production',
            path: 'exporters.otlp.tls.insecure',
            paths: ['exporters.otlp.tls.insecure'],
            message: 'Production config enables insecure TLS.',
            remediation: 'Disable insecure TLS or move this config out of production.',
            packaging: 'community',
            tier: 'core',
          },
          {
            policy_id: 'community',
            policy_name: 'Community config policy',
            rule_id: 'community.processors.memory_limiter_missing',
            rule_code: 'processors.memory_limiter_missing',
            severity: 'warning',
            decision: 'warn',
            target_scope: 'collector',
            environment: 'production',
            path: 'service.pipelines.traces.processors',
            paths: ['processors.memory_limiter', 'service.pipelines.traces.processors'],
            message: 'Pipeline is missing memory_limiter.',
            remediation: 'Add memory_limiter before exporting telemetry.',
            packaging: 'community',
            tier: 'core',
          },
          {
            policy_id: 'community',
            policy_name: 'Community config policy',
            rule_id: 'community.processors.batch_missing',
            rule_code: 'processors.batch_missing',
            severity: 'warning',
            decision: 'warn',
            target_scope: 'collector',
            environment: 'production',
            path: 'service.pipelines.metrics.processors',
            paths: ['processors.batch', 'service.pipelines.metrics.processors'],
            message: 'Pipeline is missing batch.',
            remediation: 'Add the batch processor to improve exporter behavior.',
            packaging: 'community',
            tier: 'core',
          },
          {
            policy_id: 'pro-endpoints',
            policy_name: 'Pro endpoint allowlist',
            rule_id: 'pro.exporters.endpoint_allowlist',
            rule_code: 'exporters.endpoint_allowlist',
            severity: 'critical',
            decision: 'block',
            target_scope: 'collector',
            environment: 'production',
            path: 'exporters.otlp.endpoint',
            paths: ['exporters.otlp.endpoint'],
            message: 'Exporter endpoint is outside the allowlist.',
            remediation: 'Use an approved endpoint from the policy allowlist.',
            packaging: 'pro',
            tier: 'configurable',
          },
          {
            policy_id: 'enterprise-critical-exporters',
            policy_name: 'Enterprise tenant policy',
            rule_id: 'enterprise.exporters.critical_removal',
            rule_code: 'exporters.critical_removal',
            severity: 'critical',
            decision: 'block',
            target_scope: 'tenant',
            environment: 'production',
            path: 'exporters.vendor_audit',
            paths: ['exporters.vendor_audit', 'service.pipelines.logs.exporters'],
            message: 'Critical exporter would be removed.',
            remediation: 'Keep the exporter or adjust the tenant policy hook.',
            packaging: 'enterprise',
            tier: 'tenant_hook',
          },
          {
            policy_id: 'enterprise-resource-attrs',
            policy_name: 'Enterprise tenant policy',
            rule_id: 'enterprise.resource_attributes.required',
            rule_code: 'resource_attributes.required',
            severity: 'warning',
            decision: 'warn',
            target_scope: 'tenant',
            environment: 'production',
            path: 'processors.resource.attributes',
            paths: ['processors.resource.attributes'],
            message: 'Required resource attributes are missing.',
            remediation: 'Add service.name and deployment.environment resource attributes.',
            packaging: 'enterprise',
            tier: 'tenant_hook',
          },
          {
            policy_id: 'pro-sampling',
            policy_name: 'Pro sampling policy',
            rule_id: 'pro.sampling.unsafe_percentage',
            rule_code: 'sampling.unsafe_percentage',
            severity: 'warning',
            decision: 'warn',
            target_scope: 'collector',
            environment: 'production',
            path: 'processors.probabilistic_sampler.sampling_percentage',
            paths: ['processors.probabilistic_sampler.sampling_percentage'],
            message: 'Sampling percentage is outside the configured safe range.',
            remediation: 'Keep sampling within the configured Pro policy range.',
            packaging: 'pro',
            tier: 'configurable',
          },
        ],
      }),
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()

  const policy = page.locator('.config-policy-panel')
  await expect(policy).toContainText('Policy blocked')
  await expect(policy).toContainText('Community config policy')
  await expect(policy).toContainText('Community built-in rule')
  await expect(policy).toContainText('community.production.insecure_tls')
  await expect(policy).toContainText('Production config enables insecure TLS.')
  await expect(policy).toContainText('exporters.otlp.tls.insecure')
  await expect(policy).toContainText('Disable insecure TLS')
  await expect(policy).toContainText('community.processors.memory_limiter_missing')
  await expect(policy).toContainText('Pipeline is missing memory_limiter.')
  await expect(policy).toContainText('community.processors.batch_missing')
  await expect(policy).toContainText('Pipeline is missing batch.')
  await expect(policy).toContainText('Pro endpoint allowlist')
  await expect(policy).toContainText('Pro configurable policy')
  await expect(policy).toContainText('pro.exporters.endpoint_allowlist')
  await expect(policy).toContainText('Enterprise tenant policy')
  await expect(policy).toContainText('Enterprise tenant policy hook')
  await expect(policy).toContainText('enterprise.exporters.critical_removal')
  await expect(policy).toContainText('enterprise.resource_attributes.required')
  await expect(policy).toContainText('pro.sampling.unsafe_percentage')
  await expect(policy).toContainText('Sampling percentage is outside the configured safe range.')
  await expect(page.getByRole('button', { name: 'Request approval' })).toBeDisabled()
})

test('policy evaluation unavailable state is explicit and non-alarming', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan({ policy: null }))

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()

  const policy = page.locator('.config-policy-panel')
  await expect(policy).toContainText('Policy evaluation unavailable')
  await expect(policy).toContainText('The backend did not return policy findings for this plan.')
  await expect(page.getByRole('button', { name: 'Request approval' })).toBeDisabled()
  await page.getByLabel('Approval request comment').fill('please review prod change')
  await page.getByLabel('I acknowledge this targets production').check()
  await expect(page.getByRole('button', { name: 'Request approval' })).toBeEnabled()
})

test('approval request and production push require comments and double confirmation', async ({
  loggedInPage: page,
}) => {
  await prepareApprovedDraft(page)

  let requestBody: Record<string, unknown> | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals`, async (route, request) => {
    if (request.method() !== 'POST') return route.continue()
    requestBody = request.postDataJSON() as Record<string, unknown>
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify(approvalRequest()),
    })
  })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/approve`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(
        approvalRequest({
          status: 'approved',
          approved_by: 'alice@example.com',
          approved_at: '2026-06-30T10:16:00.000Z',
          approval_comment: 'approved after review',
        }),
      ),
    }),
  )

  await expect(page.getByRole('button', { name: 'Request approval' })).toBeDisabled()
  await page.getByLabel('Approval request comment').fill('please review prod change')
  await page.getByLabel('I acknowledge this targets production').check()
  await page.getByRole('button', { name: 'Request approval' }).click()
  await expect.poll(() => requestBody).toMatchObject({
    target_group: 'single',
    target_env: 'prod',
    comment: 'please review prod change',
    prod_confirmation: true,
  })

  await expect(page.locator('.config-approval-panel')).toContainText('Approval requested')
  await page.getByLabel('Approval comment').fill('approved after review')
  await page.getByRole('button', { name: 'Approve request' }).click()
  await expect(page.locator('.config-approval-panel')).toContainText('Approved by alice@example.com')
  await expect(page.getByRole('button', { name: 'Push approved config' })).toBeDisabled()

  let pushBody: Record<string, unknown> | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/push`, async (route, request) => {
    pushBody = request.postDataJSON() as Record<string, unknown>
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify(
        approvalRequest({
          status: 'pushed',
          config_hash: 'feedfacefeedface',
          pushed_at: '2026-06-30T10:17:00.000Z',
        }),
      ),
    })
  })
  await page.getByLabel('Production push comment').fill('roll out during approved window')
  await page.getByLabel('I understand this changes production telemetry').check()
  await page.getByLabel('I confirm the safety plan and approval are current').check()
  await page.getByRole('button', { name: 'Push approved config' }).click()

  await expect.poll(() => pushBody).toMatchObject({
    comment: 'roll out during approved window',
    prod_double_confirmed: true,
    break_glass: false,
  })
  await expect(page.locator('.config-approval-panel')).toContainText('Pushed')
})

test('break-glass push is visually distinct and requires an audited reason', async ({
  loggedInPage: page,
}) => {
  await prepareApprovedDraft(page)

  let pushBody: Record<string, unknown> | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals`, async (route, request) => {
    if (request.method() !== 'POST') return route.continue()
    await route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify(approvalRequest()),
    })
  })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/push`, async (route, request) => {
    pushBody = request.postDataJSON() as Record<string, unknown>
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify(
        approvalRequest({
          id: 'break-glass',
          status: 'pushed',
          break_glass: true,
          break_glass_reason: 'prod outage mitigation',
          config_hash: 'feedfacefeedface',
          pushed_at: '2026-06-30T10:18:00.000Z',
        }),
      ),
    })
  })

  await page.getByLabel('Approval request comment').fill('please review prod change')
  await page.getByLabel('I acknowledge this targets production').check()
  await page.getByRole('button', { name: 'Request approval' }).click()
  await page.getByLabel('Use break-glass emergency push').check()
  await expect(page.locator('.config-approval-panel')).toContainText('Break-glass emergency path')
  await expect(page.getByRole('button', { name: 'Break-glass push' })).toBeDisabled()
  await page.getByLabel('Break-glass reason').fill('prod outage mitigation')
  await page.getByLabel('I understand this changes production telemetry').check()
  await page.getByLabel('I confirm the safety plan and approval are current').check()
  await page.getByRole('button', { name: 'Break-glass push' }).click()

  await expect.poll(() => pushBody).toMatchObject({
    comment: 'prod outage mitigation',
    break_glass: true,
    break_glass_reason: 'prod outage mitigation',
    prod_double_confirmed: true,
  })
  await expect(page.locator('.config-approval-panel')).toContainText('Break-glass pushed')
})

test('viewer cannot request approval, approve, or push config', async ({ loggedInPage: page }) => {
  await mockMe(page, { groups: [viewerGroup] })
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockApprovalList(page, [approvalRequest({ status: 'approved', approved_by: 'alice@example.com' })])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Request approval' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Approve request' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /Push approved config|Break-glass push/ })).toHaveCount(0)
})

test('canary wizard validates a percentage target group before starting canary', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockEditorMe(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockInstances(page)
  await mockCanaryValidation(page, {
    valid: true,
    targets: [
      { instance_uid: 'inst-a', pod_name: 'otel-a', status: 'sent' },
      { instance_uid: 'inst-b', pod_name: 'otel-b', status: 'sent' },
    ],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await generateSafetyPlan(page)
  await page.getByRole('button', { name: 'Start canary' }).click()
  await page.getByLabel('Canary strategy').selectOption('percentage')
  await page.getByLabel('Percentage of collectors').fill('50')
  await page.getByRole('button', { name: 'Validate canary targets' }).click()

  await expect(page.locator('.canary-validation-panel')).toContainText('2 selected targets')
  await expect(page.locator('.canary-validation-panel')).toContainText('otel-a')
  await expect(page.getByRole('button', { name: 'Push canary' })).toBeEnabled()
})

test('canary wizard selects one eligible instance and explains disabled options', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockEditorMe(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockInstances(page, [
    {
      instance_uid: 'inst-offline',
      pod_name: 'otel-offline',
      version: '0.98.0',
      connected_at: new Date().toISOString(),
      last_message_at: '',
      effective_config_hash: 'abc123',
      healthy: true,
      accepts_remote_config: true,
    },
    {
      instance_uid: 'inst-unhealthy',
      pod_name: 'otel-unhealthy',
      version: '0.98.0',
      connected_at: new Date().toISOString(),
      last_message_at: new Date().toISOString(),
      effective_config_hash: 'abc123',
      healthy: false,
      accepts_remote_config: true,
      remote_config_status: {
        status: 'applied',
        config_hash: 'abc123',
        updated_at: new Date().toISOString(),
      },
    },
    {
      instance_uid: 'inst-readonly',
      pod_name: 'otel-readonly',
      version: '0.98.0',
      connected_at: new Date().toISOString(),
      last_message_at: new Date().toISOString(),
      effective_config_hash: 'abc123',
      healthy: true,
      remote_config_capability_known: true,
      accepts_remote_config: false,
    },
    {
      instance_uid: 'inst-ok',
      pod_name: 'otel-ok',
      version: '0.98.0',
      connected_at: new Date().toISOString(),
      last_message_at: new Date().toISOString(),
      effective_config_hash: 'abc123',
      healthy: true,
      accepts_remote_config: true,
      remote_config_status: {
        status: 'applied',
        config_hash: 'abc123',
        updated_at: new Date().toISOString(),
      },
    },
  ])

  let canaryValidationBody: unknown
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/canary/validate`, async (route) => {
    canaryValidationBody = route.request().postDataJSON()
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        valid: true,
        targets: [{ instance_uid: 'inst-ok', pod_name: 'otel-ok', status: 'sent' }],
      }),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await generateSafetyPlan(page)
  await page.getByRole('button', { name: 'Start canary' }).click()

  const picker = page.getByLabel('Collector instance')
  await expect(picker).toHaveValue('inst-ok')
  await expect(picker.locator('option[value="inst-offline"]')).toContainText('offline')
  await expect(picker.locator('option[value="inst-unhealthy"]')).toContainText('unhealthy')
  await expect(picker.locator('option[value="inst-readonly"]')).toContainText('read-only')
  await expect(picker.locator('option[value="inst-offline"]')).toHaveAttribute('disabled', '')
  await expect(picker.locator('option[value="inst-ok"]')).not.toHaveAttribute('disabled', '')

  await page.getByRole('button', { name: 'Validate canary targets' }).click()
  await expect.poll(() => canaryValidationBody).toMatchObject({
    selection: { strategy: 'one', instance_uid: 'inst-ok' },
  })
  await expect(page.getByRole('button', { name: 'Push canary' })).toBeEnabled()
})

test('canary wizard treats healthy remote-config capable instances without prior status as eligible', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockEditorMe(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockInstances(page, [
    {
      instance_uid: 'inst-new',
      pod_name: 'otel-new',
      version: '0.128.0',
      connected_at: new Date().toISOString(),
      last_message_at: new Date().toISOString(),
      effective_config_hash: 'abc123',
      healthy: true,
      accepts_remote_config: true,
    },
  ])

  let canaryValidationBody: unknown
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/canary/validate`, async (route) => {
    canaryValidationBody = route.request().postDataJSON()
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        valid: true,
        targets: [{ instance_uid: 'inst-new', pod_name: 'otel-new', status: 'sent' }],
      }),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await generateSafetyPlan(page)
  await page.getByRole('button', { name: 'Start canary' }).click()

  const picker = page.getByLabel('Collector instance')
  await expect(picker).toHaveValue('inst-new')
  await expect(picker.locator('option[value="inst-new"]')).not.toHaveAttribute('disabled', '')

  await page.getByRole('button', { name: 'Validate canary targets' }).click()
  await expect.poll(() => canaryValidationBody).toMatchObject({
    selection: { strategy: 'one', instance_uid: 'inst-new' },
  })
  await expect(page.getByRole('button', { name: 'Push canary' })).toBeEnabled()
})

test('canary wizard does not treat unknown remote-config capability as read-only', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockEditorMe(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockInstances(page, [
    {
      instance_uid: 'inst-legacy',
      pod_name: 'otel-legacy',
      version: '0.98.0',
      connected_at: new Date().toISOString(),
      last_message_at: new Date().toISOString(),
      effective_config_hash: 'abc123',
      healthy: true,
      accepts_remote_config: false,
    },
  ])

  let canaryValidationBody: unknown
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/canary/validate`, async (route) => {
    canaryValidationBody = route.request().postDataJSON()
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        valid: true,
        targets: [{ instance_uid: 'inst-legacy', pod_name: 'otel-legacy', status: 'sent' }],
      }),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await generateSafetyPlan(page)
  await page.getByRole('button', { name: 'Start canary' }).click()

  const picker = page.getByLabel('Collector instance')
  await expect(picker).toHaveValue('inst-legacy')
  await expect(picker.locator('option[value="inst-legacy"]')).not.toHaveAttribute('disabled', '')

  await page.getByRole('button', { name: 'Validate canary targets' }).click()
  await expect.poll(() => canaryValidationBody).toMatchObject({
    selection: { strategy: 'one', instance_uid: 'inst-legacy' },
  })
})

test('canary validation expires when the draft changes before push', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockEditorMe(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockInstances(page)
  await mockCanaryValidation(page, {
    valid: true,
    targets: [{ instance_uid: 'inst-a', pod_name: 'otel-a', status: 'sent' }],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await generateSafetyPlan(page)
  await page.getByRole('button', { name: 'Start canary' }).click()
  await page.getByRole('button', { name: 'Validate canary targets' }).click()
  await expect(page.getByRole('button', { name: 'Push canary' })).toBeEnabled()

  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type(' # changed after validation')

  await expect(page.getByRole('button', { name: 'Push canary' })).toBeDisabled()
})

test('canary validation failure blocks submit and explains stop reason', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockEditorMe(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockInstances(page)
  await mockCanaryValidation(
    page,
    {
      valid: false,
      errors: ['stale heartbeat: inst-c'],
      stop_reasons: ['no_heartbeat'],
      targets: [
        { instance_uid: 'inst-c', pod_name: 'otel-c', status: 'sent', stop_reason: 'no_heartbeat' },
      ],
    },
    409,
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await generateSafetyPlan(page)
  await page.getByRole('button', { name: 'Start canary' }).click()
  await page.getByRole('button', { name: 'Validate canary targets' }).click()

  await expect(page.locator('.canary-validation-panel')).toContainText(
    'No heartbeat from collector',
  )
  await expect(page.locator('.canary-validation-panel')).toContainText('stale heartbeat: inst-c')
  await expect(page.getByRole('button', { name: 'Push canary' })).toBeDisabled()
})

test('canary status panel shows stop reasons and action states', async ({ loggedInPage: page }) => {
  await mockProConfigSafetyFeatures(page)
  await mockEditorMe(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockInstances(page)
  await mockCanaryValidation(page, {
    valid: true,
    targets: [{ instance_uid: 'inst-a', pod_name: 'otel-a', status: 'sent' }],
  })
  await mockStartCanary(page, {
    id: 'canary_1234',
    workload_id: WORKLOAD_ID,
    config_hash: 'feedfacefeedface',
    status: 'stopped',
    selection: { strategy: 'one', instance_uid: 'inst-a' },
    counts: { pending: 0, applying: 0, applied: 0, failed: 1 },
    stop_reasons: ['remote_config_failed'],
    targets: [
      {
        instance_uid: 'inst-a',
        pod_name: 'otel-a',
        status: 'failed',
        stop_reason: 'remote_config_failed',
      },
    ],
    created_at: '2026-07-01T10:00:00Z',
    updated_at: '2026-07-01T10:01:00Z',
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await generateSafetyPlan(page)
  await page.getByRole('button', { name: 'Start canary' }).click()
  await page.getByRole('button', { name: 'Validate canary targets' }).click()
  await page.getByRole('button', { name: 'Push canary' }).click()

  const panel = page.locator('.canary-status-panel')
  await expect(panel).toContainText('Canary stopped')
  await expect(panel).toContainText('Remote config failed')
  await expect(panel).toContainText('feedface')
  await expect(page.getByRole('button', { name: 'Promote' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Abort' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Rollback', exact: true })).toBeEnabled()
})

test('canary start stays disabled until a safety plan is ready', async ({ loggedInPage: page }) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Start canary' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Start canary' })).toHaveAttribute(
    'title',
    /Generate a non-blocking Config Safety Plan before starting a canary/,
  )
})

test('plan blocks push for validation failure and read-only targets with reasons', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, { accepts_remote_config: false })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockPlan(
    page,
    buildPlan({
      summary: {
        target_count: 1,
        collector_target_count: 1,
        remote_config_capable_count: 0,
        read_only_count: 1,
        validation_ok_count: 0,
        validation_failed_count: 1,
        components_missing_count: 1,
        high_risk_change_count: 0,
        excluded_count: 1,
      },
      targets: [
        {
          workload_id: WORKLOAD_ID,
          display_name: 'test-collector',
          type: 'collector',
          accepts_remote_config: false,
          read_only: true,
          validation_status: 'failed',
          validation_errors: ['component_not_installed'],
          components_missing_count: 1,
          high_risk_change_count: 0,
          excluded: true,
          exclusion_reasons: ['read_only', 'validation_failed'],
          hard_failures: ['read_only', 'validation_failed'],
          active_config_hash: 'abc123',
          active_config_unavailable: false,
        },
      ],
      hard_failures: ['validation_failed', 'all_targets_excluded'],
      can_push: false,
      apply_allowed: false,
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const plan = page.locator('.config-application-plan')
  await expect(plan).toContainText('Push blocked')
  await expect(plan).toContainText('Read-only')
  await expect(plan).toContainText('component_not_installed')
  await expect(page.getByRole('button', { name: 'Push approved config' })).toHaveCount(0)
})

test('plan surfaces high-risk changes reported by backend', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(
    page,
    buildPlan({
      summary: {
        target_count: 1,
        collector_target_count: 1,
        remote_config_capable_count: 1,
        read_only_count: 0,
        validation_ok_count: 1,
        validation_failed_count: 0,
        components_missing_count: 0,
        high_risk_change_count: 2,
        excluded_count: 0,
      },
      targets: [
        {
          workload_id: WORKLOAD_ID,
          display_name: 'test-collector',
          type: 'collector',
          accepts_remote_config: true,
          read_only: false,
          validation_status: 'ok',
          validation_errors: [],
          components_missing_count: 0,
          high_risk_change_count: 2,
          excluded: false,
          exclusion_reasons: [],
          hard_failures: [],
          active_config_hash: 'abc123',
          active_config_unavailable: false,
        },
      ],
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()

  const plan = page.locator('.config-application-plan')
  await expect(plan).toContainText('High-risk changes')
  await expect(plan).toContainText('2')
  await expect(plan).toContainText('Review high-risk changes before pushing')
})

test('export plan action exposes an accessible markdown download affordance', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await mockPlanExport(page)

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()

  await expect(page.getByRole('button', { name: 'Export plan' })).toBeEnabled()
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export plan' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toContain('config-safety-plan')
  await expect(page.locator('.config-application-plan')).toContainText('Plan export ready')
})

test('export plan falls back to client-side JSON when backend export is unavailable', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(
    page,
    buildPlan({
      export: {
        supported: false,
        formats: [],
        json_endpoint: '',
        markdown_endpoint: '',
        persisted_rollout: 'not_persisted',
      },
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()

  await expect(page.locator('.config-application-plan')).toContainText(
    'JSON export will be generated in your browser',
  )
  await expect(page.getByRole('button', { name: 'Export plan' })).toBeEnabled()
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Export plan' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('config-safety-plan.json')
  await expect(page.locator('.config-application-plan')).toContainText('Plan export ready')
})

test('validation details show separated static component and runtime checks', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: true,
    overall_status: 'passed',
    summary: 'All validation checks passed.',
    target_collector_version: '0.150.1',
    checks: VALIDATION_CHECKS_SUCCESS,
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()

  const details = page.locator('.validation-details')
  await expect(details).toContainText('Validation passed')
  await expect(details).toContainText('Target 0.150.1')
  await expect(details).toContainText('otelcol 0.150.1')
  await expect(details.locator('.validation-check-card')).toHaveCount(4)
  await expect(details.locator('.validation-check-card').nth(0)).toContainText('YAML syntax')
  await expect(details.locator('.validation-check-card').nth(2)).toContainText(
    'Components available on workload',
  )
  await expect(details.locator('.validation-check-card').nth(3)).toContainText(
    'Collector runtime validation',
  )
})

test('validation details separate non-blocking warnings from blocking errors', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: false,
    overall_status: 'failed',
    summary: 'One blocking validation check failed.',
    target_collector_version: '0.150.1',
    errors: [
      {
        code: 'undefined_component',
        message: 'pipeline "traces" references exporter "nope"',
        path: 'service.pipelines.traces.exporters[0]',
        check_id: 'collector_structure',
      },
    ],
    warnings: [
      {
        code: 'otelcol_version_mismatch',
        severity: 'warning',
        message: 'Runtime validation used otelcol 0.149.0 while target version is 0.150.1.',
        check_id: 'otelcol_runtime',
      },
    ],
    checks: [
      VALIDATION_CHECKS_SUCCESS[0],
      {
        id: 'collector_structure',
        label: 'Collector structure',
        source: 'server.structure',
        status: 'failed',
        required: true,
        messages: [
          {
            code: 'undefined_component',
            severity: 'error',
            message: 'pipeline "traces" references exporter "nope"',
            path: 'service.pipelines.traces.exporters[0]',
          },
        ],
      },
      {
        id: 'otelcol_runtime',
        label: 'Collector runtime validation',
        source: 'otelcol.binary',
        status: 'warning',
        required: false,
        metadata: { binary_version: '0.149.0', target_version: '0.150.1' },
        messages: [
          {
            code: 'otelcol_version_mismatch',
            severity: 'warning',
            message: 'Runtime validation used otelcol 0.149.0 while target version is 0.150.1.',
          },
        ],
      },
    ],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()

  const details = page.locator('.validation-details')
  await expect(details).toContainText('Blocking errors')
  await expect(details).toContainText('Warnings')
  await expect(details.locator('.validation-check-card-failed')).toContainText('Required')
  await expect(details.locator('.validation-check-card-warning')).toContainText('Advisory')
  await expect(details.locator('.validation-check-card-failed')).toContainText(
    'undefined_component',
  )
  await expect(details.locator('.validation-check-card-warning')).toContainText(
    'otelcol_version_mismatch',
  )
  await expect(page.getByRole('button', { name: 'Request approval' })).toHaveCount(0)
})

test('validation details explain when otelcol runtime check is skipped', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, {
    valid: true,
    overall_status: 'warning',
    summary: 'Static validation passed; runtime validation skipped.',
    target_collector_version: '0.150.1',
    warnings: [
      {
        code: 'otelcol_not_found',
        severity: 'warning',
        message: 'otelcol binary "otelcol" was not found on the server.',
        check_id: 'otelcol_runtime',
      },
    ],
    checks: [
      ...VALIDATION_CHECKS_SUCCESS.slice(0, 3),
      {
        id: 'otelcol_runtime',
        label: 'Collector runtime validation',
        source: 'otelcol.binary',
        status: 'skipped',
        required: false,
        metadata: { target_version: '0.150.1' },
        messages: [
          {
            code: 'otelcol_not_found',
            severity: 'warning',
            message: 'otelcol binary "otelcol" was not found on the server.',
          },
        ],
      },
    ],
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()

  const details = page.locator('.validation-details')
  await expect(details).toContainText('Validation passed with warnings')
  await expect(details).toContainText('Skipped')
  await expect(details).toContainText('otelcol binary "otelcol" was not found on the server.')
  await expect(page.getByRole('button', { name: 'Generate safety plan' })).toBeEnabled()
  await expect(page.getByRole('button', { name: 'Request approval' })).toHaveCount(0)
})

test('push timeline renders aggregate progress and per-instance details', async ({
  loggedInPage: page,
}) => {
  const submittedAt = '2026-06-30T10:12:00Z'
  const sentAt = '2026-06-30T10:12:01Z'
  await mockWorkload(page, {
    current_config_push: {
      push_id: 'push_01JVISIBLE',
      workload_id: WORKLOAD_ID,
      config_id: 'feedfacefeedface',
      config_hash: 'feedfacefeedface',
      status: 'applying',
      submitted_at: submittedAt,
      sent_at: sentAt,
      updated_at: '2026-06-30T10:12:05Z',
      target_count: 3,
      applied_count: 1,
      failed_count: 0,
      pending_count: 2,
      timeline: [
        { state: 'submitted', at: submittedAt, terminal: false },
        { state: 'sent', at: sentAt, terminal: false },
        { state: 'applying', at: '2026-06-30T10:12:05Z', terminal: false },
      ],
      instance_statuses: [
        {
          instance_uid: 'inst-a',
          pod_name: 'otel-a',
          required: true,
          status: 'applied',
          config_hash: 'feedfacefeedface',
          updated_at: '2026-06-30T10:12:06Z',
        },
        {
          instance_uid: 'inst-b',
          pod_name: 'otel-b',
          required: true,
          status: 'applying',
          config_hash: 'feedfacefeedface',
          updated_at: '2026-06-30T10:12:05Z',
        },
        {
          instance_uid: 'inst-c',
          pod_name: 'otel-c',
          required: true,
          status: 'no_status',
          config_hash: 'feedfacefeedface',
          error_cause: 'apply_timeout',
          error_message: 'No OpAMP status after 30s',
          timed_out: true,
        },
      ],
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const panel = page.locator('.push-status-panel')
  await expect(panel).toContainText('Applying')
  await expect(panel).toContainText('1/3 applied')
  await expect(panel).toContainText('2 pending')
  await expect(panel.locator('.push-timeline-step')).toContainText([
    'Validated',
    'Submitted',
    'Sent via OpAMP',
    'Applying',
    'Applied',
  ])

  await page.getByRole('button', { name: 'View instance details' }).click()
  await expect(page.locator('.push-instance-table tbody tr')).toHaveCount(3)
  await expect(page.locator('.push-instance-table')).toContainText('inst-a')
  await expect(page.locator('.push-timeout-banner')).toContainText('No OpAMP status after 30s')
  await expect(page.locator('.push-instance-table')).toContainText('otel-c')
  await expect(page.locator('.push-instance-table')).toContainText('No status yet')
  await expect(page.locator('.push-instance-table')).toContainText('No OpAMP status after 30s')
})

test('push status shows the explicit OpAMP timeout message from the API', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, {
    current_config_push: {
      push_id: 'push_timeout',
      workload_id: WORKLOAD_ID,
      config_id: 'deadbeefdeadbeef',
      config_hash: 'deadbeefdeadbeef',
      status: 'sent',
      submitted_at: '2026-06-30T10:12:00Z',
      sent_at: '2026-06-30T10:12:01Z',
      updated_at: '2026-06-30T10:12:30Z',
      timed_out_waiting_for_opamp_status: true,
      timeout_message: 'No OpAMP status after 30s',
      target_count: 1,
      applied_count: 0,
      failed_count: 0,
      pending_count: 1,
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('.push-timeout-banner')).toContainText('No OpAMP status after 30s')
  await expect(page.locator('.push-timeout-banner')).toContainText(
    'otel-magnify sent the config but has not received an OpAMP status from the workload yet.',
  )
})

test('remote config errors are grouped by cause with affected instances', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, {
    current_config_push: {
      push_id: 'push_failed',
      workload_id: WORKLOAD_ID,
      config_id: 'badbadbadbad',
      config_hash: 'badbadbadbad',
      status: 'failed',
      submitted_at: '2026-06-30T10:12:00Z',
      updated_at: '2026-06-30T10:12:08Z',
      target_count: 3,
      applied_count: 0,
      failed_count: 3,
      pending_count: 0,
      error_message:
        'collector failed with authorization: Bearer *** and endpoint https://collector.internal/v1/traces',
      instance_statuses: [
        {
          instance_uid: 'inst-a',
          required: true,
          status: 'failed',
          error_cause: 'collector_validation',
          error_message: 'password: raw-instance-password',
        },
      ],
      error_groups: [
        {
          cause: 'collector_validation',
          title: 'Collector rejected the config',
          severity: 'high',
          count: 2,
          affected_instances: ['inst-a', 'inst-b'],
          sample_message:
            "unknown exporter 'othttp' token=raw-group-secret endpoint https://collector.internal/v1/traces",
          sample_path: 'service.pipelines.traces.exporters[0]',
          config_hash: 'badbadbadbad',
          retryable: true,
        },
        {
          cause: 'opamp_send_failed',
          title: 'OpAMP delivery failed',
          severity: 'high',
          count: 1,
          affected_instances: ['inst-c'],
          sample_message: 'connection lost',
          config_hash: 'badbadbadbad',
          retryable: true,
        },
      ],
    },
  })
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const errors = page.locator('.push-error-groups')
  await expect(errors).toContainText('Collector rejected the config')
  await expect(errors).toContainText('2 instances')
  await expect(errors).toContainText("unknown exporter 'othttp'")
  await expect(errors).toContainText('redacted credential; redacted endpoint')
  await expect(errors).toContainText('Severity: high')
  await expect(errors).toContainText('collector_validation')
  await expect(errors).toContainText('inst-a')
  await expect(errors).toContainText('OpAMP delivery failed')
  await page.getByRole('button', { name: 'View instance details' }).click()
  await expect(page.locator('body')).not.toContainText('raw-top-secret-token')
  await expect(page.locator('body')).not.toContainText('raw-group-secret')
  await expect(page.locator('body')).not.toContainText('raw-instance-password')
  await expect(page.locator('body')).not.toContainText('https://collector.internal/v1/traces')
})

test('push failed shows error banner and preserves draft', async ({ loggedInPage: page }) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/push`, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify(
        approvalRequest({
          status: 'pushed',
          config_hash: 'deadbeefdeadbeef',
          pushed_at: '2026-06-30T10:17:00.000Z',
        }),
      ),
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type(' # touched')
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await expect(page.locator('.validation-ok')).toBeVisible()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()
  await expect(page.locator('.config-application-plan')).toContainText('Ready to push')
  await requestAndApproveDraft(page)
  await page.getByLabel('Production push comment').fill('roll out during approved window')
  await page.getByLabel('I understand this changes production telemetry').check()
  await page.getByLabel('I confirm the safety plan and approval are current').check()
  await page.getByRole('button', { name: 'Push approved config' }).click()

  // Simulate FAILED WS event
  await page.evaluate(() => {
    const evt = {
      type: 'workload_config_status',
      workload_id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
      status: {
        status: 'failed',
        config_hash: 'deadbeefdeadbeef',
        error_message: "unknown exporter 'othttp'",
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  })

  await expect(page.locator('.push-banner-failed')).toContainText("unknown exporter 'othttp'")
  // Draft preserved — editor still shows our addition
  await expect(page.locator('.cm-content').first()).toContainText('# touched')
})

test('push and rollback banners redact sensitive remote error text', () => {
  const rawError =
    'collector failed: SECRET_TOKEN=abc123 authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces'

  const rendered = safeRemoteErrorText(rawError)

  expect(rendered).toBe('redacted credential; redacted endpoint; redacted tenant')
  expect(rendered).not.toContain('SECRET_TOKEN')
  expect(rendered).not.toContain('authorization=Bearer')
  expect(rendered).not.toContain('abc123')
  expect(rendered).not.toContain('super-secret')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('/v1/traces')
})

test('rollback reasons keep a safe short cause while redacting legacy remote details', () => {
  const rawReason =
    "collector rejected config for tenant-a.internal: unknown exporter 'othttp' SECRET_TOKEN=abc123 authorization=Bearer super-secret exporters:\n  otlphttp:\n    endpoint: https://tenant-a.internal:4318/v1/traces"

  const rendered = safeRemoteErrorText(rawReason)

  expect(rendered).toBe(
    "unknown exporter 'othttp' — redacted credential; redacted endpoint; redacted tenant; configuration error",
  )
  expect(rendered).not.toContain('SECRET_TOKEN')
  expect(rendered).not.toContain('authorization=Bearer')
  expect(rendered).not.toContain('abc123')
  expect(rendered).not.toContain('super-secret')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('exporters:')
  expect(rendered).not.toContain('otlphttp')
})

test('rollback reasons do not preserve sensitive unknown component names as causes', () => {
  const rawReason =
    "unknown exporter 'tenant-a.internal' authorization=Bearer super-secret endpoint=https://tenant-a.internal:4318/v1/traces"

  const rendered = safeRemoteErrorText(rawReason)

  expect(rendered).toBe('redacted credential; redacted endpoint; redacted tenant')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('authorization=Bearer')
  expect(rendered).not.toContain('super-secret')
  expect(rendered).not.toContain('/v1/traces')
})

test('rollback reasons map config snippets and tenant identifiers to concise safe labels', () => {
  const rawReason =
    'tenant-a failed rollback validation: service:\n  pipelines:\n    traces:\n      exporters: [otlp]\nexporters:\n  otlp:\n    endpoint: tenant-a.internal:4317'

  const rendered = safeRemoteErrorText(rawReason)

  expect(rendered).toBe('redacted endpoint; redacted tenant; configuration error')
  expect(rendered).not.toContain('tenant-a')
  expect(rendered).not.toContain('tenant-a.internal')
  expect(rendered).not.toContain('service:')
  expect(rendered).not.toContain('exporters:')
  expect(rendered).not.toContain('otlp')
})

test('push applied closes edit mode, clears draft, shows applied banner', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/push`, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify(
        approvalRequest({
          status: 'pushed',
          config_hash: 'feedfacefeedface',
          pushed_at: '2026-06-30T10:17:00.000Z',
        }),
      ),
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.type(' # applied-flow')
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await expect(page.locator('.validation-ok')).toBeVisible()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()
  await expect(page.locator('.config-application-plan')).toContainText('Ready to push')
  await requestAndApproveDraft(page)
  await page.getByLabel('Production push comment').fill('roll out during approved window')
  await page.getByLabel('I understand this changes production telemetry').check()
  await page.getByLabel('I confirm the safety plan and approval are current').check()
  await page.getByRole('button', { name: 'Push approved config' }).click()

  // Simulate APPLIED WS event matching our pending hash
  await page.evaluate(() => {
    const evt = {
      type: 'workload_config_status',
      workload_id: 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
      status: {
        status: 'applied',
        config_hash: 'feedfacefeedface',
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  })

  // Edit mode closed — the editor toolbar buttons are gone, the Edit entry-point is back
  await expect(page.getByRole('button', { name: 'Push approved config' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Validate for this collector' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeVisible()

  // Banner reflects the applied status
  await expect(page.locator('.push-banner-applied')).toBeVisible()
  await expect(page.locator('.push-banner-applied')).toContainText('feedface')

  // Re-entering edit mode starts from the active config (not the previous draft)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.locator('.cm-content').first()).not.toContainText('# applied-flow')
})

test('diff tab shows two editor panels', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type('a: 2\n')
  await page.getByRole('button', { name: 'Diff' }).click()

  await expect(page.locator('.cm-mergeView .cm-editor')).toHaveCount(2)
})

test('history refreshes when WS workload_config_status arrives from another session', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')

  let rows: unknown[] = []
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs`, (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(rows),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  // No history yet: table not rendered
  await expect(page.locator('.history-table')).toHaveCount(0)

  // Simulate a config applied event from another session (not our local push)
  rows = [
    {
      workload_id: WORKLOAD_ID,
      config_id: 'ccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
      applied_at: new Date().toISOString(),
      status: 'applied',
      pushed_by: 'other@user',
      error_message: '',
    },
  ]
  await page.evaluate((workloadId) => {
    const evt = {
      type: 'workload_config_status',
      workload_id: workloadId,
      status: {
        status: 'applied',
        config_hash: 'cccccccc',
        updated_at: new Date().toISOString(),
      },
    }
    ;(window as unknown as { __testWsInject?: (ev: unknown) => void }).__testWsInject?.(evt)
  }, WORKLOAD_ID)

  // Table appears because the query was invalidated and refetched
  await expect(page.locator('.history-table tbody tr')).toHaveCount(1)
  await expect(page.locator('.history-table')).toContainText('other@user')
})

test('history table renders with rollback action', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [
    {
      workload_id: WORKLOAD_ID,
      config_id: '1111111111111111',
      applied_at: new Date().toISOString(),
      status: 'applied',
      pushed_by: 'me@x',
      content: 'old: true',
    },
    {
      workload_id: WORKLOAD_ID,
      config_id: '2222222222222222',
      applied_at: new Date().toISOString(),
      status: 'failed',
      error_message: 'boom',
      pushed_by: 'me@x',
      content: 'bad',
    },
  ])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await expect(page.locator('.history-table tbody tr')).toHaveCount(2)
  await expect(page.locator('.history-error').first()).toHaveText('')
  await expect(page.locator('.history-error').nth(1)).toContainText('boom')
  // Rollback affordance was renamed (config-versioning v0.5.0): each row with
  // recoverable content shows a "Rollback" button that opens a confirm dialog.
  await expect(page.getByRole('button', { name: 'Rollback' }).first()).toBeVisible()
})

test('YAML keys are colored via Signal Deck theme', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockConfig(page, 'key: value\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  // Wait for the editor to render
  await expect(page.locator('.cm-content')).toBeVisible()

  // Find the span that carries the color attribution (Lezer highlight uses
  // generated class names, so we match on computed style instead of class).
  const firstSpan = page.locator('.cm-line span').first()
  const color = await firstSpan.evaluate((el) => getComputedStyle(el).color)
  expect(color).toBe('rgb(212, 168, 74)')
})

async function mockEditorMe(page: Page) {
  await mockMe(page, {
    groups: [
      {
        id: 'grp_system_editor',
        name: 'editor',
        role: 'editor',
        is_system: true,
        created_at: new Date().toISOString(),
      },
    ],
  })
}

function mockCanaryValidation(
  page: Page,
  result: Record<string, unknown> & { valid: boolean },
  status = 200,
) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/config/canary/validate`, (route) =>
    route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify(result),
    }),
  )
}

function mockStartCanary(page: Page, result: Record<string, unknown>) {
  return page.route(`**/api/workloads/${WORKLOAD_ID}/config/canary`, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify(result),
    }),
  )
}

function mockInstances(page: Page, instances?: Array<Record<string, unknown>>) {
  const body = instances ?? [
    {
      instance_uid: 'inst-a',
      pod_name: 'otel-a',
      version: '0.98.0',
      connected_at: new Date().toISOString(),
      last_message_at: new Date().toISOString(),
      effective_config_hash: 'abc123',
      healthy: true,
      accepts_remote_config: true,
      remote_config_status: {
        status: 'applied',
        config_hash: 'abc123',
        updated_at: new Date().toISOString(),
      },
    },
  ]
  void page.route(`**/api/workloads/${WORKLOAD_ID}/instances`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    }),
  )
  return page.route(`**/api/workloads/${WORKLOAD_ID}/topology`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 'workload-topology.v1',
        workload_id: WORKLOAD_ID,
        summary: {
          connected_count: body.length,
          healthy_count: body.filter((instance) => instance.healthy).length,
          unhealthy_count: body.filter((instance) => !instance.healthy).length,
          drifted_count: 0,
          heterogeneous: false,
          version_diversity: [],
          config_hash_diversity: [],
          remote_config_status_counts: {},
          heterogeneity: {},
          heterogeneity_reasons: [],
        },
        instances: body,
      }),
    }),
  )
}

function mockConfigsList(page: Page, configs: Array<{ id: string; name: string }>) {
  return page.route(`**/api/configs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(
        configs.map((c) => ({
          id: c.id,
          name: c.name,
          content: '',
          created_at: new Date().toISOString(),
          created_by: 'tester',
        })),
      ),
    }),
  )
}

function mockConfigDetail(page: Page, id: string, name: string, content: string) {
  return page.route(`**/api/configs/${id}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id,
        name,
        content,
        created_at: new Date().toISOString(),
        created_by: 'tester',
      }),
    }),
  )
}

test('selecting a saved config loads YAML into editor and switches to Diff tab', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'old: true\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])
  await mockConfigDetail(page, 'cfg-eu', 'collector-prod-eu', 'new: true\n')

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.locator('select.apply-config-select').selectOption('cfg-eu')

  // Diff tab should be the active one (workload has active_config_id)
  await expect(page.locator('.tab-active')).toHaveText('Diff')
  // Two editor panels visible (the MergeView)
  await expect(page.locator('.cm-mergeView .cm-editor')).toHaveCount(2)
  // The right-hand (newYaml) editor contains the selected config's content
  await expect(page.locator('.cm-mergeView .cm-content').nth(1)).toContainText('new: true')
})

test('apply-saved-config selector renders in supervised collector branch', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [
    { id: 'cfg-eu', name: 'collector-prod-eu' },
    { id: 'cfg-us', name: 'collector-prod-us' },
  ])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const selector = page.locator('select.apply-config-select')
  await expect(selector).toBeVisible()
  await expect(selector.locator('option')).toHaveCount(3) // placeholder + 2 configs
  await expect(selector.locator('option').nth(1)).toContainText('collector-prod-eu')
  await expect(selector.locator('option').nth(2)).toContainText('collector-prod-us')
})

test('bootstrap workload (no active config): selecting falls back to Edit tab', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page, { active_config_id: undefined })
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])
  await mockConfigDetail(page, 'cfg-eu', 'collector-prod-eu', 'fresh: true\n')

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.locator('select.apply-config-select').selectOption('cfg-eu')

  // Edit tab is the only navigable one (Diff is disabled when no active_config_id)
  await expect(page.locator('.tab-active')).toHaveText('Edit')
  await expect(page.getByRole('button', { name: 'Diff' })).toBeDisabled()
  // Editor draft contains the selected config's content
  await expect(page.locator('.cm-content').first()).toContainText('fresh: true')
})

test('selector annotates the currently applied config', async ({ loggedInPage: page }) => {
  await mockWorkload(page) // active_config_id = ACTIVE_CONFIG_ID = 'abc123'
  await mockConfig(page, 'old: true\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [
    { id: 'abc123', name: 'collector-prod-eu' },
    { id: 'cfg-us', name: 'collector-prod-us' },
  ])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const eu = page.locator('select.apply-config-select option').nth(1)
  await expect(eu).toContainText('collector-prod-eu')
  await expect(eu).toContainText('(currently applied)')

  const us = page.locator('select.apply-config-select option').nth(2)
  await expect(us).toContainText('collector-prod-us')
  await expect(us).not.toContainText('(currently applied)')
})

test('empty configs list disables selector with explanatory text', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const selector = page.locator('select.apply-config-select')
  await expect(selector).toBeDisabled()
  await expect(selector).toHaveValue('')
  await expect(selector.locator('option')).toHaveCount(1)
  await expect(selector.locator('option').first()).toContainText('No saved configs')

  // Editor copy-paste flow still functional: Edit button visible
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeVisible()
})

test('configs list fetch error shows disabled selector with retry', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await page.route('**/api/configs', (route) =>
    route.fulfill({ status: 500, body: '{"error":"boom"}' }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  const selector = page.locator('select.apply-config-select')
  await expect(selector).toBeDisabled()
  await expect(selector.locator('option').first()).toContainText('Failed to load configs')

  // Editor copy-paste flow still works
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeVisible()
})

test('selector is absent in read-only collector branch', async ({ loggedInPage: page }) => {
  await mockWorkload(page, { accepts_remote_config: false })
  await mockConfig(page, 'a: 1\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])
  await mockPlan(
    page,
    buildPlan({
      summary: {
        target_count: 1,
        collector_target_count: 1,
        remote_config_capable_count: 0,
        read_only_count: 1,
        validation_ok_count: 1,
        validation_failed_count: 0,
        components_missing_count: 0,
        high_risk_change_count: 0,
        excluded_count: 1,
      },
      targets: [
        {
          workload_id: WORKLOAD_ID,
          display_name: 'test-collector',
          type: 'collector',
          accepts_remote_config: false,
          read_only: true,
          validation_status: 'ok',
          validation_errors: [],
          components_missing_count: 0,
          high_risk_change_count: 0,
          excluded: true,
          exclusion_reasons: ['read_only'],
          hard_failures: ['read_only'],
          active_config_hash: 'abc123',
          active_config_unavailable: false,
        },
      ],
      hard_failures: ['all_targets_excluded'],
      can_push: false,
      apply_allowed: false,
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('select.apply-config-select')).toHaveCount(0)
  // Read-only message still shown
  await expect(page.locator('.config-readonly-note')).toContainText('Read-only')
})

test('selector is absent for SDK workloads', async ({ loggedInPage: page }) => {
  await mockWorkload(page, {
    type: 'sdk',
    active_config_id: undefined,
    accepts_remote_config: false,
    available_components: undefined,
    labels: { 'service.name': 'demo-app' },
  })
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.locator('select.apply-config-select')).toHaveCount(0)
  // SDK label chips visible (page shows labels in both the Labels section and the
  // Configuration section; assert at least one chip carries the expected text)
  await expect(page.locator('.label-chip').first()).toContainText('demo-app')
})

test('selecting a config overwrites in-progress draft silently (no confirm)', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockConfig(page, 'old: true\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [{ id: 'cfg-eu', name: 'collector-prod-eu' }])
  await mockConfigDetail(page, 'cfg-eu', 'collector-prod-eu', 'replaced: true\n')

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  // Enter edit mode and type something
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.type('user-typed-mess: yes\n')

  // Now select a saved config
  await page.locator('select.apply-config-select').selectOption('cfg-eu')

  // The draft should now contain the saved config's content, not the typed mess.
  // Editor visible is the right-hand panel of the MergeView (Diff tab is auto-active).
  await expect(page.locator('.cm-mergeView .cm-content').nth(1)).toContainText('replaced: true')
  await expect(page.locator('.cm-mergeView .cm-content').nth(1)).not.toContainText(
    'user-typed-mess',
  )
})

test('capable user sees enabled push scope selector and preview buckets', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page)
  await mockPushPreview(page)

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await expect(page.locator('.validation-ok')).toBeVisible()

  await expect(page.locator('.push-scope-panel')).toContainText('Push target scope')
  await expect(page.locator('select.push-scope-mode-select')).toBeEnabled()
  await expect(page.locator('select.push-scope-mode-select')).toContainText('Current workload')
  await expect(page.locator('select.push-scope-mode-select')).toContainText('Saved group')
  await expect(page.locator('select.push-scope-mode-select')).toContainText('Dynamic selector')
  await page.locator('select.push-scope-mode-select').selectOption('saved')
  await expect(page.locator('.push-scope-mode-badge')).toContainText('Preview only')
  await expect(page.locator('select.push-saved-group-select')).toBeEnabled()
  await expect(page.locator('select.push-saved-group-select option[value="payments"]')).toHaveText(
    'Payments collectors',
  )
  await page.locator('select.push-saved-group-select').selectOption('payments')
  await page.getByRole('button', { name: 'Preview targets' }).click()

  await expect(page.locator('.push-preview-panel')).toContainText('8 targeted')
  await expect(page.locator('.push-preview-panel')).toContainText('5 capable')
  await expect(page.locator('.push-preview-panel')).toContainText('1 read-only')
  await expect(page.locator('.push-preview-panel')).toContainText('1 incompatible')
  await expect(page.locator('.push-preview-panel')).toContainText('1 offline')
  await expect(page.locator('.push-preview-blocked')).toContainText('payments-ro')
  await expect(page.locator('.push-preview-blocked')).toContainText('payments-incompatible')
  await expect(page.locator('.push-preview-warning')).toContainText(
    'Blocked targets must be excluded',
  )
  await expect(page.getByRole('button', { name: 'Request approval' })).toHaveCount(0)
})

test('saved group blocked preview cannot submit an accidental config push', async ({
  loggedInPage: page,
}) => {
  let pushRequestCount = 0
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page)
  await mockPushPreview(page)
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config`, (route) => {
    pushRequestCount += 1
    return route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'config push initiated', config_hash: 'should-not-push' }),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.locator('select.push-scope-mode-select').selectOption('saved')
  await page.locator('select.push-saved-group-select').selectOption('payments')
  await page.getByRole('button', { name: 'Preview targets' }).click()

  await expect(page.locator('.push-preview-panel')).toContainText('8 targeted')
  await expect(page.locator('.push-preview-warning')).toContainText(
    'Blocked targets must be excluded',
  )
  await expect(page.getByRole('button', { name: 'Request approval' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Push approved config' })).toHaveCount(0)
  expect(pushRequestCount).toBe(0)
})

test('dynamic all-capable preview stays preview-only and does not push bulk targets', async ({
  loggedInPage: page,
}) => {
  let pushRequestCount = 0
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page)
  await mockPushPreview(page, {
    dynamicPreview: pushPreviewScenarios.dynamicCapableCollectors,
  })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config`, (route) => {
    pushRequestCount += 1
    return route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'config push initiated', config_hash: 'should-not-push' }),
    })
  })

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.locator('select.push-scope-mode-select').selectOption('dynamic')
  await page.getByLabel('Cluster').fill('prod-eu')
  await page.getByRole('button', { name: 'Preview targets' }).click()

  await expect(page.locator('.push-preview-panel')).toContainText('3 targeted')
  await expect(page.locator('.push-preview-panel')).toContainText('3 capable')
  await expect(page.locator('.push-preview-ready')).toContainText('Ready to push')
  await expect(page.getByRole('button', { name: 'Request approval' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Push approved config' })).toHaveCount(0)
  expect(pushRequestCount).toBe(0)
})

test('single collector scope still generates a plan and submits the workload config push', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPlan(page, buildPlan())

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page.locator('.cm-content').first().click()
  await page.keyboard.press('End')
  await page.keyboard.type(' # single-scope-regression')
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.getByRole('button', { name: 'Generate safety plan' }).click()
  await expect(page.locator('.config-application-plan')).toContainText('Ready to push')

  await requestAndApproveDraft(page)
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/push`, (route) =>
    route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify(
        approvalRequest({
          status: 'pushed',
          config_hash: 'feedfacefeedface',
          pushed_at: '2026-06-30T10:17:00.000Z',
        }),
      ),
    }),
  )
  const pushRequest = page.waitForRequest(`**/api/workloads/${WORKLOAD_ID}/config/approvals/approval-1/push`)
  await page.getByLabel('Production push comment').fill('roll out during approved window')
  await page.getByLabel('I understand this changes production telemetry').check()
  await page.getByLabel('I confirm the safety plan and approval are current').check()
  await page.getByRole('button', { name: 'Push approved config' }).click()
  const request = await pushRequest

  expect(request.postDataJSON()).toMatchObject({ comment: 'roll out during approved window' })
  await expect(page.locator('.config-approval-panel')).toContainText('Pushed')
})

test('viewer permission keeps config push controls read-only', async ({ loggedInPage: page }) => {
  await mockMe(page, { groups: [viewerGroup] })
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockConfigsList(page, [
    { id: ACTIVE_CONFIG_ID, name: 'collector-prod-eu' },
    { id: 'cfg-us', name: 'collector-prod-us' },
  ])

  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.getByText('Configuration', { exact: true })).toBeVisible()
  await expect(page.locator('.cm-content').first()).toContainText('receivers')
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveAttribute(
    'title',
    /don't have permission to push workload configurations/,
  )
  await expect(page.getByRole('button', { name: 'Edit', exact: true })).toHaveAttribute(
    'aria-describedby',
    'config-permission-note',
  )
  await expect(page.locator('select.apply-config-select')).toBeDisabled()
  await expect(page.locator('select.apply-config-select')).toHaveAttribute(
    'title',
    /don't have permission to push workload configurations/,
  )
  await expect(page.locator('select.apply-config-select')).toHaveAttribute(
    'aria-describedby',
    'config-permission-note',
  )
  await expect(page.locator('select.apply-config-select option').nth(1)).toContainText(
    'collector-prod-eu (currently applied)',
  )
  await expect(page.locator('select.apply-config-select option').nth(2)).toContainText(
    'collector-prod-us',
  )
  await expect(page.locator('.config-permission-note')).toContainText('permission')
  await expect(page.locator('.push-scope-panel')).toHaveCount(0)
  await expect(page.locator('.push-preview-panel')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Validate for this collector' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Push approved config' })).toHaveCount(0)
})

test('French scope UX renders translated labels and blocked preview copy', async ({
  loggedInPage: page,
}) => {
  await page.addInitScript(() => window.localStorage.setItem('lang', 'fr'))
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page)
  await mockPushPreview(page)

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Modifier' }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()

  await page.locator('select.push-scope-mode-select').selectOption('saved')
  await expect(page.locator('select.push-saved-group-select option[value="payments"]')).toHaveText(
    'Collecteurs paiements',
  )
  await page.locator('select.push-saved-group-select').selectOption('payments')
  await page.getByRole('button', { name: 'Prévisualiser les cibles' }).click()

  await expect(page.locator('.push-scope-panel')).toContainText('Portée cible du push')
  await expect(page.locator('.push-scope-mode-badge')).toContainText('Prévisualisation seule')
  await expect(page.locator('.push-preview-panel')).toContainText('8 ciblées')
  await expect(page.locator('.push-preview-panel')).toContainText('1 lecture seule')
  await expect(page.locator('.push-preview-blocked')).toContainText('connecté')
  await expect(page.locator('.push-preview-blocked')).toContainText(
    'Le workload n’accepte pas la config distante.',
  )
  await expect(page.locator('.push-preview-warning')).toContainText('cibles bloquées')
  await expect(page.locator('body')).not.toContainText('workloads.config.scope')
})

test('saved scope selector reports empty and failed group states', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.locator('select.push-scope-mode-select').selectOption('saved')

  await expect(page.locator('select.push-saved-group-select option').first()).toHaveText(
    '— No saved groups available —',
  )
  await expect(page.getByRole('button', { name: 'Preview targets' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Preview targets' })).toHaveAttribute(
    'title',
    'Select a saved group before previewing targets.',
  )

  await page.route('**/api/push-groups', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }),
  )
  await page.locator('select.push-scope-mode-select').selectOption('dynamic')
  await page.locator('select.push-scope-mode-select').selectOption('saved')

  await expect(page.locator('select.push-saved-group-select')).toBeDisabled()
  await expect(page.locator('select.push-saved-group-select option').first()).toHaveText(
    '— Failed to load groups —',
  )
})

test('French saved scope selector reports localized empty and failed group states', async ({
  loggedInPage: page,
}) => {
  await page.addInitScript(() => window.localStorage.setItem('lang', 'fr'))
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page, [])

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Modifier' }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.locator('select.push-scope-mode-select').selectOption('saved')

  await expect(page.locator('select.push-saved-group-select option').first()).toHaveText(
    '— Aucun groupe sauvegardé disponible —',
  )
  await expect(page.getByRole('button', { name: 'Prévisualiser les cibles' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Prévisualiser les cibles' })).toHaveAttribute(
    'title',
    'Choisissez un groupe sauvegardé avant de prévisualiser les cibles.',
  )

  await page.route('**/api/push-groups', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom"}' }),
  )
  await page.locator('select.push-scope-mode-select').selectOption('dynamic')
  await page.locator('select.push-scope-mode-select').selectOption('saved')

  await expect(page.locator('select.push-saved-group-select')).toBeDisabled()
  await expect(page.locator('select.push-saved-group-select option').first()).toHaveText(
    '— Échec du chargement des groupes —',
  )
  await expect(page.locator('body')).not.toContainText('workloads.config.scope')
})

test('saved scope preview failure shows inline error and clears preview panel', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page)
  await page.route('**/api/pushes/preview', (route) =>
    route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: '{"error":"preview exploded"}',
    }),
  )

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await page.locator('select.push-scope-mode-select').selectOption('saved')
  await page.locator('select.push-saved-group-select').selectOption('payments')
  await page.getByRole('button', { name: 'Preview targets' }).click()

  await expect(page.locator('.error-text-push')).toContainText('preview exploded')
  await expect(page.locator('.push-preview-panel')).toHaveCount(0)
})

test('dynamic push selector posts labels version and capability for safe preview', async ({
  loggedInPage: page,
}) => {
  await mockProConfigSafetyFeatures(page)
  await mockWorkload(page)
  await mockConfig(page, 'receivers:\n  otlp: {}\n')
  await mockHistory(page, [])
  await mockValidate(page, { valid: true })
  await mockPushGroups(page)
  await mockPushPreview(page)

  await page.goto(`/workloads/${WORKLOAD_ID}`)
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await page
    .getByRole('button', { name: /Validate for this collector|Valider pour ce collecteur/ })
    .click()
  await expect(page.locator('.validation-ok')).toBeVisible()

  await page.locator('select.push-scope-mode-select').selectOption('dynamic')
  await page.getByLabel('Cluster').fill('prod-eu')
  await page.getByLabel('Namespace').fill('observability')
  await page.getByLabel('Environment').fill('prod')
  await page.getByLabel('Team').fill('platform')
  await page.getByLabel('Workload type').fill('daemonset')
  await page.getByLabel('Collector version').fill('0.98.0')
  await page.getByLabel('Capabilities').fill('otlp, debug')
  const previewRequest = page.waitForRequest('**/api/pushes/preview')
  await page.getByRole('button', { name: 'Preview targets' }).click()

  const request = await previewRequest
  expect(request.postDataJSON()).toMatchObject({
    selector: {
      match_labels: {
        cluster: 'prod-eu',
        namespace: 'observability',
        env: 'prod',
        team: 'platform',
        workload_type: 'daemonset',
      },
      types: ['collector'],
      versions: ['0.98.0'],
      capabilities: ['otlp', 'debug'],
    },
  })
  await expect(page.locator('.push-preview-panel')).toContainText('3 targeted')
  await expect(page.locator('.push-preview-panel')).toContainText('Ready to push')
})
