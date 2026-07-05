import { test, expect, mockFeatures, mockMe } from './fixtures'
import type { Page, Route } from '@playwright/test'

const WORKLOAD_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
const ACTIVE_CONFIG_ID = 'hash-current'

const HASH_OLD = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
const HASH_NEW = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'

const YAML_OLD = `receivers:
  otlp: {}
exporters:
  logging: {}
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]
`
const YAML_NEW = `${YAML_OLD}# revision-new\n`

const SECRET_LITERAL = 'Bearer super-secret-token'

const BLOCKING_POLICY = {
  schema_version: 'config-policy.v1',
  valid: true,
  allowed: false,
  decision: 'block',
  severity: 'critical',
  target: { workload_id: WORKLOAD_ID, environment: 'production' },
  settings: {},
  summary: { pass_count: 0, warn_count: 0, block_count: 1 },
  audit: { persisted: false },
  findings: [
    {
      policy_id: 'community',
      policy_name: 'Community config policy',
      rule_id: 'community.exporters.critical_removal',
      rule_code: 'exporters.critical_removal',
      severity: 'critical',
      decision: 'block',
      target_scope: 'collector',
      environment: 'production',
      path: 'exporters.logging',
      paths: ['exporters.logging', 'service.pipelines.traces.exporters'],
      message: 'A critical exporter was removed from the traces pipeline.',
      remediation: 'Keep the critical exporter or update the policy before rollout.',
      packaging: 'community',
      tier: 'core',
    },
  ],
}

const HIGH_OTEL_DIFF = {
  schema_version: 'otel-config-diff.v1',
  valid: true,
  summary: {
    overall_risk: 'high',
    headline: 'High risk: memory_limiter removed and exporter auth changed',
    counts: {
      components_added: 0,
      components_removed: 2,
      components_modified: 1,
      pipelines_added: 0,
      pipelines_removed: 0,
      pipelines_modified: 1,
      endpoints_added: 0,
      endpoints_removed: 0,
      endpoints_modified: 1,
      high_risk: 4,
      medium_risk: 1,
      low_risk: 0,
    },
  },
  blast_radius: {
    schema_version: 'otel-config-blast-radius.v1',
    affected_signals: ['traces', 'metrics'],
    touched_exporters: ['otlp/prod', 'debug'],
    impacted_services: [
      {
        service_name: 'checkout-api',
        workload_id: WORKLOAD_ID,
        display_name: 'checkout collector',
        type: 'collector',
        status: 'degraded',
      },
    ],
    impacted_clusters: ['prod-eu-1'],
    critical_collectors: [
      {
        workload_id: WORKLOAD_ID,
        display_name: 'checkout collector',
        status: 'degraded',
        reasons: ['critical=true', 'degraded'],
      },
    ],
  },
  human_summary: [
    {
      kind: 'added',
      category: 'component',
      component_id: 'exporters/loki',
      risk: 'low',
      text: 'Adds Loki exporter.',
    },
    {
      kind: 'modified',
      category: 'pipeline',
      pipeline_key: 'logs',
      signal: 'logs',
      risk: 'low',
      text: 'Routes logs to Loki.',
    },
    {
      kind: 'unchanged',
      category: 'unchanged',
      signal: 'traces',
      risk: 'none',
      text: 'Keeps traces unchanged.',
    },
    {
      kind: 'removed',
      category: 'component',
      component_id: 'exporters/debug',
      risk: 'medium',
      text: 'Removes debug exporter.',
    },
    {
      kind: 'modified',
      category: 'field',
      path: 'processors.batch.timeout',
      risk: 'low',
      text: 'Changes batch timeout.',
    },
  ],
  components: [
    {
      id: 'processors:memory_limiter',
      kind: 'removed',
      component: {
        category: 'processors',
        id: 'memory_limiter',
        type: 'memory_limiter',
        path: 'processors.memory_limiter',
      },
      risk: 'high',
      title: 'Processor memory_limiter removed',
      changed_fields: [],
      impacted_pipelines: ['traces'],
      rules: ['memory_limiter_removed_from_pipeline'],
    },
  ],
  pipelines: [
    {
      id: 'pipeline:traces',
      kind: 'modified',
      pipeline_key: 'traces',
      signal: 'traces',
      risk: 'high',
      component_ref_changes: [
        {
          section: 'processors',
          component_id: 'memory_limiter',
          kind: 'removed',
          from_index: 0,
          risk: 'high',
          reason: 'memory limiter removed',
        },
      ],
      rules: ['memory_limiter_removed_from_pipeline'],
    },
  ],
  endpoints: [
    {
      id: 'endpoint:exporters.otlp.endpoint',
      kind: 'modified',
      component: { category: 'exporters', id: 'otlp', type: 'otlp', path: 'exporters.otlp' },
      endpoint_kind: 'otlp_grpc',
      field_path: 'exporters.otlp.endpoint',
      before: { raw: 'https://otel-old.example:4317', normalized: 'https://otel-old.example:4317' },
      after: { raw: 'https://otel-new.example:4317', normalized: 'https://otel-new.example:4317' },
      risk: 'high',
      rules: ['otlp_endpoint_changed'],
    },
  ],
  security: [
    {
      id: 'security:auth-header',
      kind: 'modified',
      component: { category: 'exporters', id: 'otlp', type: 'otlp', path: 'exporters.otlp' },
      path: 'exporters.otlp.headers.Authorization',
      field: 'headers',
      before: { redaction_state: 'redacted', display: '••••masked••••', raw: SECRET_LITERAL },
      after: { redaction_state: 'redacted', display: '••••masked••••' },
      risk: 'high',
      rules: ['auth_removed'],
      message: 'Header authorization modified',
    },
  ],
  risk_items: [
    {
      id: 'risk:memory-limiter',
      risk: 'high',
      category: 'availability',
      rule: 'memory_limiter_removed_from_pipeline',
      title: 'Memory limiter removed',
      description: 'The traces pipeline no longer has memory protection.',
      affected_paths: ['processors.memory_limiter', 'service.pipelines.traces.processors'],
      affected_pipelines: ['traces'],
    },
    {
      id: 'risk:tls',
      risk: 'high',
      category: 'security',
      rule: 'transport_security_weakened',
      title: 'Transport security weakened',
      description: 'Exporter transport security changed.',
      affected_paths: ['exporters.otlp.tls.insecure'],
      affected_pipelines: ['traces'],
    },
  ],
  diagnostics: [],
  normalized: {
    base_hash: 'sha256:old',
    target_hash: 'sha256:new',
    base_component_count: 4,
    target_component_count: 3,
    base_pipeline_count: 1,
    target_pipeline_count: 1,
  },
}

function mockWorkload(page: Page) {
  void page.route(`**/api/workloads/${WORKLOAD_ID}/known-good`, (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'known-good config not found' }),
    }),
  )
  return page.route(`**/api/workloads/${WORKLOAD_ID}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: WORKLOAD_ID,
        fingerprint_source: 'k8s',
        fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
        display_name: 'test-collector',
        type: 'collector',
        version: '0.98.0',
        status: 'connected',
        last_seen_at: new Date().toISOString(),
        labels: {},
        active_config_id: ACTIVE_CONFIG_ID,
        accepts_remote_config: true,
        available_components: { components: { receivers: ['otlp'], exporters: ['logging'] } },
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
        content: YAML_NEW,
        created_at: new Date().toISOString(),
        created_by: 'test',
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

function mockConfigsList(page: Page) {
  return page.route(`**/api/configs`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: ACTIVE_CONFIG_ID,
          name: 'current',
          content: YAML_NEW,
          created_at: new Date().toISOString(),
          created_by: 'test',
        },
      ]),
    }),
  )
}

async function gotoWorkloadDetail(
  page: Page,
  role: 'editor' | 'viewer' = 'editor',
  features: Record<string, boolean> = {
    'config_safety.guided_rollback': true,
    'config_safety.canary_rollout': true,
    'config_safety.scoped_push': true,
    'config_safety.approvals': true,
    'config_safety.gitops_export': true,
  },
) {
  // Seed auth on the app origin through a stable document before hitting the
  // protected route; this keeps the spec independent from login submission.
  await mockFeatures(page, features)
  await page.route('**/api/auth/methods', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        methods: [
          {
            id: 'password',
            type: 'password',
            display_name: 'Email + password',
            login_url: '/api/auth/login',
          },
        ],
      }),
    }),
  )
  await mockMe(page, {
    groups: [
      {
        id: `grp_system_${role}`,
        name: role,
        role,
        is_system: true,
        created_at: new Date().toISOString(),
      },
    ],
  })
  await page.goto('/login', { waitUntil: 'domcontentloaded' })
  await page.goto(`/workloads/${WORKLOAD_ID}`)
}

const BASE_HISTORY = [
  {
    workload_id: WORKLOAD_ID,
    config_id: HASH_NEW,
    applied_at: '2026-05-08T12:00:00Z',
    status: 'applied',
    pushed_by: 'admin@e2e.local',
    content: YAML_NEW,
  },
  {
    workload_id: WORKLOAD_ID,
    config_id: HASH_OLD,
    applied_at: '2026-05-01T09:00:00Z',
    status: 'applied',
    pushed_by: 'admin@e2e.local',
    content: YAML_OLD,
    label: 'stable-2026-04',
  },
]

test('viewer login after editor cache does not expose cached config content actions', async ({
  loggedInPage: page,
}) => {
  let viewerSession = false
  const fulfillWorkloadsList = (route: Route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: WORKLOAD_ID,
          fingerprint_source: 'k8s',
          fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
          display_name: 'test-collector',
          type: 'collector',
          version: '0.98.0',
          status: 'connected',
          last_seen_at: new Date().toISOString(),
          labels: {},
          active_config_id: ACTIVE_CONFIG_ID,
          accepts_remote_config: true,
          available_components: { components: { receivers: ['otlp'], exporters: ['logging'] } },
        },
      ]),
    })
  const mockWorkloadsListForSession = async () => {
    await page.route('**/api/workloads', fulfillWorkloadsList)
    await page.route('**/api/workloads?*', fulfillWorkloadsList)
  }
  await mockWorkloadsListForSession()
  await mockWorkload(page)
  await page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) => {
    if (viewerSession) {
      return route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'config content restricted' }),
      })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: ACTIVE_CONFIG_ID,
        name: 'current',
        content: YAML_NEW,
        created_at: new Date().toISOString(),
        created_by: 'test',
      }),
    })
  })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs`, (route) => {
    if (viewerSession) {
      return route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'config content restricted' }),
      })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(BASE_HISTORY),
    })
  })
  await mockConfigsList(page)
  await gotoWorkloadDetail(page, 'editor')
  await expect(page.getByRole('button', { name: 'View' }).first()).toBeVisible()
  await expect(page.getByText('# revision-new')).toBeVisible()

  await page.locator('.identity-trigger').click()
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login$/)

  await page.route('**/api/auth/methods', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        methods: [
          {
            id: 'password',
            type: 'password',
            display_name: 'Email + password',
            login_url: '/api/auth/login',
          },
        ],
      }),
    }),
  )
  await mockFeatures(page, {})
  await mockWorkloadsListForSession()
  await mockWorkload(page)
  await page.route(`**/api/configs/${ACTIVE_CONFIG_ID}`, (route) => {
    if (viewerSession) {
      return route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'config content restricted' }),
      })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: ACTIVE_CONFIG_ID,
        name: 'current',
        content: YAML_NEW,
        created_at: new Date().toISOString(),
        created_by: 'test',
      }),
    })
  })
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs`, (route) => {
    if (viewerSession) {
      return route.fulfill({
        status: 403,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'config content restricted' }),
      })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(BASE_HISTORY),
    })
  })
  await mockConfigsList(page)
  await page.route('**/api/auth/login', (route) => {
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ token: 'viewer-token' }),
    })
  })
  await mockMe(page, {
    id: 'viewer-user',
    email: 'viewer@e2e.local',
    groups: [
      {
        id: 'grp_system_viewer',
        name: 'viewer',
        role: 'viewer',
        is_system: true,
        created_at: new Date().toISOString(),
      },
    ],
  })

  await page.locator('#login-email').fill('viewer@e2e.local')
  await page.locator('#login-password').fill('password')
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.locator('.sidebar-logo-name')).toBeVisible()

  viewerSession = true
  await page.goto(`/workloads/${WORKLOAD_ID}`)

  await expect(page.getByText('Config content is restricted by role.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'View', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Compare revisions' })).toHaveCount(0)
  await expect(page.getByText('# revision-new')).toHaveCount(0)
})

test('community features keep shared config primitives but disable paid safety controls', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  let knownGoodHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/known-good`, (route) => {
    knownGoodHit = true
    return route.fulfill({ status: 500, body: 'known-good should be feature gated' })
  })

  await gotoWorkloadDetail(page, 'editor', {})

  await expect(page.getByText('Push history', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Compare revisions' })).toBeEnabled()
  await page.getByRole('button', { name: 'Edit', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
  await expect(page.getByText('Guided rollback is not enabled for this workspace.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Rollback' }).first()).toBeDisabled()
  await expect(
    page.getByRole('button', { name: /Mark as known-good|Clear known-good/ }).first(),
  ).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Start canary' })).toBeDisabled()
  await expect(page.locator('.push-scope-panel')).toContainText('Scoped push is not enabled')
  expect(knownGoodHit).toBe(false)
})

test('label can be set inline via double-click', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  let labelPosted: { hash: string; body: unknown } | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/*/label`, async (route, request) => {
    const url = new URL(request.url())
    const segments = url.pathname.split('/')
    const hash = segments[segments.length - 2]
    labelPosted = { hash, body: request.postDataJSON() }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ label: (request.postDataJSON() as { label: string }).label }),
    })
  })

  await gotoWorkloadDetail(page)
  await expect(page.getByText('Push history', { exact: true })).toBeVisible()

  // The newest row has no label yet — double-click its label cell to edit.
  const newestRow = page.locator('.history-table tbody tr').first()
  await newestRow.locator('.history-label').dblclick()
  const input = newestRow.locator('.history-label input')
  await input.fill('canary')
  await input.blur()

  await expect.poll(() => labelPosted).not.toBeNull()
  expect(labelPosted!.hash).toBe(HASH_NEW)
  expect(labelPosted!.body).toEqual({ label: 'canary' })
})

test('compare dialog diffs two arbitrary revisions', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  // The dialog fetches each revision by hash on demand.
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/${HASH_OLD}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...BASE_HISTORY[1],
        content: YAML_OLD,
      }),
    }),
  )
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/${HASH_NEW}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...BASE_HISTORY[0],
        content: YAML_NEW,
      }),
    }),
  )
  await page.route('**/api/configs/diff', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(HIGH_OTEL_DIFF),
    }),
  )
  await page.route('**/api/configs/policy/preview', async (route, request) => {
    expect(request.postDataJSON()).toMatchObject({
      current_yaml: YAML_OLD,
      candidate_yaml: YAML_NEW,
      target: { workload_id: WORKLOAD_ID },
    })
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(BLOCKING_POLICY),
    })
  })

  await gotoWorkloadDetail(page)
  await page.getByRole('button', { name: 'Compare revisions' }).click()

  await expect(page.getByRole('dialog', { name: 'Compare two revisions' })).toBeVisible()
  const whatChanged = page.getByRole('region', { name: 'What changed?' })
  await expect(whatChanged).toBeVisible()
  await expect(whatChanged).toContainText('Adds Loki exporter.')
  await expect(whatChanged).toContainText('Routes logs to Loki.')
  await expect(whatChanged).toContainText('Keeps traces unchanged.')
  await expect(whatChanged).toContainText('Removes debug exporter.')
  await expect(whatChanged).toContainText('Changes batch timeout.')
  await expect(page.getByText(SECRET_LITERAL)).toHaveCount(0)
  // The MergeView from CodeMirror renders two .cm-content panes side-by-side.
  await expect(page.locator('.config-diff-view .cm-content')).toHaveCount(2)
})

test('viewer cannot initiate rollback or known-good history actions', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  let prepareHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/rollback/prepare**`, (route) => {
    prepareHit = true
    return route.fulfill({ status: 500, body: 'viewer must not prepare rollback' })
  })

  await gotoWorkloadDetail(page, 'viewer')
  const olderRow = page.locator('.history-table tbody tr').nth(1)

  await expect(olderRow.getByRole('button', { name: 'View', exact: true })).toHaveCount(0)
  await expect(olderRow.getByRole('button', { name: 'Rollback' })).toHaveCount(0)
  const knownGoodButton = olderRow.getByRole('button', {
    name: /Mark as known-good|Clear known-good/,
  })
  await expect(knownGoodButton).toBeDisabled()
  await expect(knownGoodButton).toHaveAttribute('title', 'Requires workload:push_config permission')
  expect(prepareHit).toBe(false)
})

test('failed history candidate is not rollbackable', async ({ loggedInPage: page }) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, [
    BASE_HISTORY[0],
    {
      workload_id: WORKLOAD_ID,
      config_id: HASH_OLD,
      applied_at: '2026-05-01T09:00:00Z',
      status: 'failed',
      pushed_by: 'admin@e2e.local',
      content: YAML_OLD,
      error_message: 'collector rejected this candidate',
    },
  ])
  await mockConfigsList(page)

  let prepareHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/rollback/prepare**`, (route) => {
    prepareHit = true
    return route.fulfill({ status: 500, body: 'failed candidate must not prepare rollback' })
  })

  await gotoWorkloadDetail(page, 'editor')
  const failedRow = page.locator('.history-table tbody tr').nth(1)
  const rollbackButton = failedRow.getByRole('button', { name: 'Rollback' })

  await expect(failedRow.getByText('failed')).toBeVisible()
  await expect(rollbackButton).toBeDisabled()
  await expect(rollbackButton).toHaveAttribute(
    'title',
    'Only applied revisions can be rolled back. Failed or pending revisions are not safe rollback targets.',
  )
  expect(prepareHit).toBe(false)
})

test('recovery panel rollback opens guided preview instead of legacy default confirm', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, [
    { ...BASE_HISTORY[0], is_current: true, content_available: true },
    { ...BASE_HISTORY[1], is_previous: true, content_available: true },
  ])
  await mockConfigsList(page)

  let preparedTarget: string | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/rollback/prepare**`, async (route, request) => {
    preparedTarget = new URL(request.url()).searchParams.get('target_hash')
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(buildRollbackPrepare()),
    })
  })

  let legacyDefaultRollbackHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/rollback`, async (route, request) => {
    if (request.method() === 'POST') {
      legacyDefaultRollbackHit = true
      await route.fulfill({ status: 500, body: 'legacy default rollback should not be called' })
      return
    }
    await route.continue()
  })

  await gotoWorkloadDetail(page)
  await page.getByRole('button', { name: 'Rollback to Previous' }).click()

  const dialog = page.getByRole('dialog', { name: 'Guided rollback' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('stable-2026-04')).toBeVisible()
  await expect.poll(() => preparedTarget).toBe(HASH_OLD)
  expect(legacyDefaultRollbackHit).toBe(false)
})

function buildRollbackPrepare(overrides: Record<string, unknown> = {}) {
  return {
    schema_version: 'guided-rollback-prepare.v1',
    workload: {
      id: WORKLOAD_ID,
      display_name: 'test-collector',
      type: 'collector',
      status: 'connected',
      accepts_remote_config: true,
      active_config_hash: HASH_NEW,
      remote_config_status: {
        status: 'applied',
        config_hash: HASH_NEW,
        updated_at: '2026-05-08T12:05:00Z',
      },
    },
    target_ref: {
      selector: 'hash',
      source: 'push_history_row',
      workload_id: WORKLOAD_ID,
      target_hash: HASH_OLD,
      known_good: true,
      known_good_source: 'label_convention',
    },
    current_config: {
      hash: HASH_NEW,
      content_available: true,
      content: YAML_NEW,
      content_sha256: HASH_NEW,
      source: 'active_config',
      metadata: { known_good: false, active_config_hash: HASH_NEW },
    },
    target_config: {
      hash: HASH_OLD,
      content_available: true,
      content: YAML_OLD,
      content_sha256: HASH_OLD,
      source: 'history',
      metadata: {
        label: 'stable-2026-04',
        known_good: true,
        applied_at: '2026-05-01T09:00:00Z',
        pushed_by: 'admin@e2e.local',
        previous_status: 'applied',
      },
    },
    diff: {
      status: 'available',
      direction: 'current_to_target',
      computation: 'backend_raw',
      base_hash: HASH_NEW,
      target_hash: HASH_OLD,
      raw_diff: {
        format: 'unified',
        language: 'yaml',
        base_label: 'Current bbbbbbbb',
        target_label: 'Rollback target aaaaaaaa',
        text: '--- current\n+++ target\n@@\n-# revision-new',
        truncated: false,
      },
      inputs: {
        current_content_available: true,
        target_content_available: true,
        current_yaml: YAML_NEW,
        target_yaml: YAML_OLD,
      },
    },
    validation: {
      status: 'valid_with_warnings',
      valid: true,
      can_confirm: true,
      checked_at: '2026-05-08T12:06:00Z',
      validator_version: 'light-validator.v1',
      inputs: {
        workload_id: WORKLOAD_ID,
        workload_type: 'collector',
        accepts_remote_config: true,
        target_hash: HASH_OLD,
      },
      findings: [
        {
          code: 'noop_target_equals_current',
          severity: 'warning',
          message: 'This target matches the current config.',
          blocking: false,
          source: 'target',
        },
      ],
      unavailable_components: [
        {
          category: 'exporters',
          component_id: 'datadog/main',
          component_type: 'datadog',
          path: 'service.pipelines.traces.exporters[0]',
          available: ['otlp', 'logging'],
          blocking: true,
        },
      ],
    },
    action: {
      can_submit: true,
      submit_url: `/api/workloads/${WORKLOAD_ID}/configs/${HASH_OLD}/rollback`,
      method: 'POST',
      requires_confirmation: true,
      confirmation_label: 'Confirm rollback with warnings',
      blocking_reasons: [],
      warnings: [
        {
          code: 'noop_target_equals_current',
          severity: 'warning',
          message: 'This target matches the current config.',
          blocking: false,
          source: 'target',
        },
      ],
    },
    status_context: {
      initial_remote_config_status: {
        status: 'applied',
        config_hash: HASH_NEW,
        updated_at: '2026-05-08T12:05:00Z',
      },
      timeout_seconds: 30,
    },
    ...overrides,
  }
}

test('guided rollback previews diff, warnings, confirmation, and final status report', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/prepare?target_hash=${HASH_OLD}`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(buildRollbackPrepare()),
      }),
  )

  let rollbackHash: string | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/*/rollback`, async (route, request) => {
    const url = new URL(request.url())
    const segments = url.pathname.split('/')
    rollbackHash = segments[segments.length - 2]
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 'guided-rollback-action.v1',
        request_id: 'rb-test-1',
        status: 'accepted',
        message: 'rollback initiated',
        workload_id: WORKLOAD_ID,
        target_hash: rollbackHash,
        config_hash: rollbackHash,
        status_url: `/api/workloads/${WORKLOAD_ID}/rollback/status?request_id=rb-test-1`,
        timeout_seconds: 30,
      }),
    })
  })
  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/status?request_id=rb-test-1`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'guided-rollback-status.v1',
          request_id: 'rb-test-1',
          workload_id: WORKLOAD_ID,
          target_hash: HASH_OLD,
          target_label: 'stable-2026-04',
          target_status: 'applied',
          target_applied_at: '2026-05-08T12:07:03Z',
          target_submitted_at: '2026-05-08T12:07:00Z',
          target_pushed_by: 'admin@e2e.local',
          request_status: 'accepted',
          apply_status: 'applied',
          terminal: true,
          terminal_status: 'applied',
          started_at: '2026-05-08T12:07:00Z',
          updated_at: '2026-05-08T12:07:04Z',
          elapsed_ms: 4100,
          timeout_seconds: 30,
          timed_out: false,
          last_known_status: 'applied',
        }),
      }),
  )

  let legacyPushHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/config`, async (route) => {
    legacyPushHit = true
    await route.fulfill({ status: 500, body: 'should not be called' })
  })

  await gotoWorkloadDetail(page)
  const olderRow = page.locator('.history-table tbody tr').nth(1)
  await olderRow.getByRole('button', { name: 'Rollback' }).click()

  const dialog = page.getByRole('dialog', { name: 'Guided rollback' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByRole('heading', { name: 'Current config' })).toBeVisible()
  await expect(dialog.getByText(HASH_NEW.slice(0, 12), { exact: true })).toBeVisible()
  await expect(dialog.getByText('stable-2026-04')).toBeVisible()
  await expect(dialog.getByText('admin@e2e.local')).toBeVisible()
  await expect(dialog.getByText('Validation passed with warnings')).toBeVisible()
  await expect(dialog.getByText('datadog/main')).toBeVisible()
  await expect(dialog.locator('.config-diff-view .cm-content')).toHaveCount(2)

  const confirmButton = dialog.getByRole('button', { name: 'Confirm rollback with warnings' })
  await expect(confirmButton).toBeDisabled()
  await dialog.getByLabel('I understand this will replace the collector remote config').check()
  await confirmButton.click()

  await expect.poll(() => rollbackHash).toBe(HASH_OLD)
  expect(legacyPushHit).toBe(false)
  await expect(dialog.getByText('Rollback applied')).toBeVisible()
  await expect(dialog.getByText('Duration: 4.1s')).toBeVisible()
  await expect(
    dialog.getByText(
      `Remote config: applied ${HASH_OLD.slice(0, 12)} (target applied, last known applied)`,
    ),
  ).toBeVisible()
})

const ROLLBACK_STATUS_LABEL_CASES = [
  {
    applyStatus: 'accepted',
    targetStatus: 'sent',
    label: 'Rollback accepted',
    terminal: false,
    duration: '120ms',
  },
  {
    applyStatus: 'applying',
    targetStatus: 'applying',
    label: 'Applying rollback',
    terminal: false,
    duration: '850ms',
  },
  {
    applyStatus: 'applied',
    targetStatus: 'applied',
    label: 'Rollback applied',
    terminal: true,
    terminalStatus: 'applied',
    duration: '1.5s',
  },
  {
    applyStatus: 'failed',
    targetStatus: 'failed',
    label: 'Rollback failed',
    terminal: true,
    terminalStatus: 'failed',
    duration: '2s',
  },
] as const

for (const statusCase of ROLLBACK_STATUS_LABEL_CASES) {
  test(`guided rollback report renders redacted ${statusCase.applyStatus} status`, async ({
    loggedInPage: page,
  }) => {
    await mockWorkload(page)
    await mockActiveConfig(page)
    await mockHistory(page, BASE_HISTORY)
    await mockConfigsList(page)

    await page.route(
      `**/api/workloads/${WORKLOAD_ID}/rollback/prepare?target_hash=${HASH_OLD}`,
      (route) =>
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(buildRollbackPrepare()),
        }),
    )

    const requestId = `rb-${statusCase.applyStatus}`
    await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/*/rollback`, (route) =>
      route.fulfill({
        status: 202,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'guided-rollback-action.v1',
          request_id: requestId,
          status: 'accepted',
          message: 'rollback initiated',
          workload_id: WORKLOAD_ID,
          target_hash: HASH_OLD,
          config_hash: HASH_OLD,
          timeout_seconds: 30,
        }),
      }),
    )
    await page.route(
      `**/api/workloads/${WORKLOAD_ID}/rollback/status?request_id=${requestId}`,
      (route) =>
        route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            schema_version: 'guided-rollback-status.v1',
            request_id: requestId,
            workload_id: WORKLOAD_ID,
            target_hash: HASH_OLD,
            target_label: 'stable-2026-04',
            target_status: statusCase.targetStatus,
            target_applied_at: '2026-05-08T12:07:03Z',
            target_submitted_at: '2026-05-08T12:07:00Z',
            target_pushed_by: 'admin@e2e.local',
            request_status: 'accepted',
            apply_status: statusCase.applyStatus,
            terminal: statusCase.terminal,
            terminal_status: statusCase.terminalStatus,
            started_at: '2026-05-08T12:07:00Z',
            updated_at: '2026-05-08T12:07:04Z',
            elapsed_ms:
              statusCase.applyStatus === 'accepted'
                ? 120
                : statusCase.applyStatus === 'applying'
                  ? 850
                  : statusCase.applyStatus === 'applied'
                    ? 1500
                    : 2000,
            timeout_seconds: 30,
            timed_out: false,
            last_known_status: statusCase.targetStatus,
          }),
        }),
    )

    await gotoWorkloadDetail(page)
    const olderRow = page.locator('.history-table tbody tr').nth(1)
    await olderRow.getByRole('button', { name: 'Rollback' }).click()
    const dialog = page.getByRole('dialog', { name: 'Guided rollback' })
    await dialog.getByLabel('I understand this will replace the collector remote config').check()
    await dialog.getByRole('button', { name: 'Confirm rollback with warnings' }).click()

    await expect(dialog.getByText(statusCase.label)).toBeVisible()
    await expect(dialog.getByText(`Duration: ${statusCase.duration}`)).toBeVisible()
    await expect(
      dialog.getByText(
        `Remote config: ${statusCase.applyStatus} ${HASH_OLD.slice(0, 12)} (target ${statusCase.targetStatus}, last known ${statusCase.targetStatus})`,
      ),
    ).toBeVisible()
    await expect(dialog.getByText('remote_config_status')).toHaveCount(0)
    await expect(dialog.getByText('history_row')).toHaveCount(0)
  })
}

test('guided rollback blocks invalid targets and reports prepare failures', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/prepare?target_hash=${HASH_OLD}`,
    (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(
          buildRollbackPrepare({
            validation: {
              status: 'invalid',
              valid: false,
              can_confirm: false,
              checked_at: '2026-05-08T12:06:00Z',
              validator_version: 'light-validator.v1',
              inputs: {
                workload_id: WORKLOAD_ID,
                workload_type: 'collector',
                accepts_remote_config: true,
                target_hash: HASH_OLD,
              },
              findings: [
                {
                  code: 'component_not_installed',
                  severity: 'error',
                  message: 'exporter type "datadog" is not installed on the target agent',
                  path: 'service.pipelines.traces.exporters[0]',
                  blocking: true,
                  source: 'capabilities',
                },
              ],
              unavailable_components: [],
            },
            action: {
              can_submit: false,
              submit_url: `/api/workloads/${WORKLOAD_ID}/configs/${HASH_OLD}/rollback`,
              method: 'POST',
              requires_confirmation: true,
              confirmation_label: 'Confirm rollback',
              blocking_reasons: [
                {
                  code: 'component_not_installed',
                  severity: 'error',
                  message: 'exporter type "datadog" is not installed on the target agent',
                  path: 'service.pipelines.traces.exporters[0]',
                  blocking: true,
                  source: 'capabilities',
                },
              ],
              warnings: [],
            },
          }),
        ),
      }),
  )

  await gotoWorkloadDetail(page)
  await page
    .locator('.history-table tbody tr')
    .nth(1)
    .getByRole('button', { name: 'Rollback' })
    .click()

  const dialog = page.getByRole('dialog', { name: 'Guided rollback' })
  await expect(dialog.getByText('Validation failed')).toBeVisible()
  await expect(
    dialog.getByText('exporter type "datadog" is not installed on the target agent'),
  ).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Confirm rollback' })).toBeDisabled()

  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/rollback/prepare?target_hash=${HASH_OLD}`,
    (route) =>
      route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'OpAMP unavailable' }),
      }),
  )
  await gotoWorkloadDetail(page)
  await page
    .locator('.history-table tbody tr')
    .nth(1)
    .getByRole('button', { name: 'Rollback' })
    .click()
  await expect(page.getByText('Failed to load rollback preview: OpAMP unavailable')).toBeVisible()
})
test('compare dialog renders enriched OTel diff without leaking redacted secrets', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, BASE_HISTORY)
  await mockConfigsList(page)

  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/${HASH_OLD}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ...BASE_HISTORY[1], content: YAML_OLD }),
    }),
  )
  await page.route(`**/api/workloads/${WORKLOAD_ID}/configs/${HASH_NEW}`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ...BASE_HISTORY[0], content: YAML_NEW }),
    }),
  )
  await page.route('**/api/configs/diff', async (route, request) => {
    expect(request.postDataJSON()).toMatchObject({ base_yaml: YAML_OLD, target_yaml: YAML_NEW })
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(HIGH_OTEL_DIFF),
    })
  })
  await page.route('**/api/configs/policy/preview', async (route, request) => {
    expect(request.postDataJSON()).toMatchObject({
      current_yaml: YAML_OLD,
      candidate_yaml: YAML_NEW,
      target: { workload_id: WORKLOAD_ID },
    })
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(BLOCKING_POLICY),
    })
  })

  await gotoWorkloadDetail(page)
  await page.getByRole('button', { name: 'Compare revisions' }).click()

  await expect(page.getByRole('heading', { name: 'OTel impact summary' })).toBeVisible()
  await expect(page.getByLabel('Risk level: high').first()).toBeVisible()
  await expect(page.getByText('Dangerous removals')).toBeVisible()
  await expect(page.getByText('Memory limiter removed')).toBeVisible()
  await expect(page.getByText('Impacted pipelines')).toBeVisible()
  await expect(page.getByText('modified · traces')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Blast radius' })).toBeVisible()
  await expect(page.getByText('Impacted services')).toBeVisible()
  await expect(page.getByText('checkout-api · checkout collector · degraded')).toBeVisible()
  await expect(page.getByText('Impacted clusters')).toBeVisible()
  await expect(page.getByText('prod-eu-1')).toBeVisible()
  await expect(page.getByText('Affected signals')).toBeVisible()
  await expect(
    page.locator('.blast-radius-card').filter({ hasText: 'Affected signals' }).getByText('traces'),
  ).toBeVisible()
  await expect(page.getByText('Touched exporters')).toBeVisible()
  await expect(page.getByText('otlp/prod')).toBeVisible()
  await expect(page.getByText('Critical collectors')).toBeVisible()
  await expect(
    page.getByText('checkout collector · degraded · critical=true, degraded'),
  ).toBeVisible()
  await expect(page.getByText('Endpoints changed')).toBeVisible()
  await expect(page.getByText('https://otel-new.example:4317').first()).toBeVisible()
  await expect(page.getByText('Auth and headers touched')).toBeVisible()
  await expect(page.getByText('Header authorization modified')).toBeVisible()
  await expect(page.getByText('Policy blocked')).toBeVisible()
  await expect(page.getByText('community.exporters.critical_removal')).toBeVisible()
  await expect(page.getByText('Keep the critical exporter')).toBeVisible()
  await expect(page.getByText('••••masked••••').first()).toBeVisible()
  await expect(page.getByText('Raw YAML diff may contain sensitive values.')).toBeVisible()
  await expect(page.getByText(SECRET_LITERAL)).toHaveCount(0)
})

test('known-good panel renders config states and defaults rollback to Last known-good', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, [
    {
      ...BASE_HISTORY[0],
      is_current: true,
      content_available: true,
    },
    {
      ...BASE_HISTORY[1],
      is_previous: true,
      is_last_known_good: true,
      content_available: true,
    },
    {
      workload_id: WORKLOAD_ID,
      config_id: 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
      applied_at: '2026-05-08T12:05:00Z',
      status: 'failed',
      pushed_by: 'admin@e2e.local',
      error_message: 'collector rejected config',
      is_failed_candidate: true,
      content_available: true,
      content: YAML_NEW,
    },
  ])
  await mockConfigsList(page)
  await page.route(`**/api/workloads/${WORKLOAD_ID}/known-good`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        workload_id: WORKLOAD_ID,
        config_id: HASH_OLD,
        marked_at: '2026-05-01T10:00:00Z',
        marked_by: 'admin@e2e.local',
        source_applied_at: '2026-05-01T09:00:00Z',
        content_available: true,
      }),
    }),
  )
  let preparedTarget: string | null = null
  await page.route(`**/api/workloads/${WORKLOAD_ID}/rollback/prepare**`, async (route, request) => {
    preparedTarget = new URL(request.url()).searchParams.get('target_hash')
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(buildRollbackPrepare()),
    })
  })
  let legacyDefaultRollbackHit = false
  await page.route(`**/api/workloads/${WORKLOAD_ID}/rollback`, async (route, request) => {
    if (request.method() === 'POST') {
      legacyDefaultRollbackHit = true
      await route.fulfill({ status: 500, body: 'legacy default rollback should not be called' })
      return
    }
    await route.continue()
  })

  await gotoWorkloadDetail(page)

  await expect(page.getByRole('region', { name: 'Configuration recovery states' })).toBeVisible()
  await expect(page.getByText('Current').first()).toBeVisible()
  await expect(page.getByText('Previous').first()).toBeVisible()
  await expect(page.getByText('Last known-good').first()).toBeVisible()
  await expect(page.getByText('Failed candidate').first()).toBeVisible()

  const knownGoodRow = page
    .locator('.history-table tbody tr')
    .filter({ hasText: HASH_OLD.substring(0, 8) })
  await expect(knownGoodRow.getByText('Previous')).toBeVisible()
  await expect(
    knownGoodRow.locator('.history-state-badge', { hasText: 'Last known-good' }),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Rollback to Last known-good' }).click()
  const dialog = page.getByRole('dialog', { name: 'Guided rollback' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('stable-2026-04')).toBeVisible()
  await expect.poll(() => preparedTarget).toBe(HASH_OLD)
  expect(legacyDefaultRollbackHit).toBe(false)
})

test('mark as known-good confirms replacement and posts precondition', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, [
    { ...BASE_HISTORY[0], is_current: true, content_available: true },
    { ...BASE_HISTORY[1], is_previous: true, is_last_known_good: true, content_available: true },
  ])
  await mockConfigsList(page)
  await page.route(`**/api/workloads/${WORKLOAD_ID}/known-good`, (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        workload_id: WORKLOAD_ID,
        config_id: HASH_OLD,
        marked_at: '2026-05-01T10:00:00Z',
        marked_by: 'admin@e2e.local',
        content_available: true,
      }),
    }),
  )

  let posted: unknown = null
  await page.route(
    `**/api/workloads/${WORKLOAD_ID}/configs/*/known-good`,
    async (route, request) => {
      posted = request.postDataJSON()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          changed: true,
          replaced_config_id: HASH_OLD,
          known_good: {
            workload_id: WORKLOAD_ID,
            config_id: HASH_NEW,
            marked_at: new Date().toISOString(),
            content_available: true,
          },
        }),
      })
    },
  )

  await gotoWorkloadDetail(page)
  const currentRow = page
    .locator('.history-table tbody tr')
    .filter({ hasText: HASH_NEW.substring(0, 8) })
  await currentRow.getByRole('button', { name: 'Mark as known-good' }).click()
  await expect(
    page.getByRole('dialog', { name: 'Mark this revision as Last known-good?' }),
  ).toBeVisible()
  await expect(
    page.getByText(`This replaces ${HASH_OLD.substring(0, 8)} as Last known-good.`),
  ).toBeVisible()
  await page.getByRole('button', { name: 'Mark as Last known-good' }).click()

  await expect.poll(() => posted).not.toBeNull()
  expect(posted).toMatchObject({ if_current_known_good: HASH_OLD })
})

test('known-good empty fallback uses Previous and viewer cannot mark revisions', async ({
  loggedInPage: page,
}) => {
  await mockWorkload(page)
  await mockActiveConfig(page)
  await mockHistory(page, [
    { ...BASE_HISTORY[0], is_current: true, content_available: true },
    { ...BASE_HISTORY[1], is_previous: true, content_available: true },
  ])
  await mockConfigsList(page)
  await page.route(`**/api/workloads/${WORKLOAD_ID}/known-good`, (route) =>
    route.fulfill({
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'known-good config not found' }),
    }),
  )

  await gotoWorkloadDetail(page, 'viewer')

  await expect(page.getByText('Last known-good: None')).toBeVisible()
  await expect(
    page.getByText('Rollback will use Previous until a known-good revision is marked.'),
  ).toBeVisible()
  const defaultRollback = page.getByRole('button', { name: 'Rollback to Previous' })
  await expect(defaultRollback).toBeDisabled()
  await expect(defaultRollback).toHaveAttribute('title', 'Requires workload:push_config permission')
  await expect(page.getByRole('button', { name: 'Mark as known-good' }).first()).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Mark as known-good' }).first()).toHaveAttribute(
    'title',
    'Requires workload:push_config permission',
  )
})
