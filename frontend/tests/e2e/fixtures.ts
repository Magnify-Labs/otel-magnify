import { test as base, expect, type Page } from '@playwright/test'
import type {
  PushGroup,
  PushGroupSelector,
  PushPreview,
  PushPreviewTarget,
  Workload,
} from '../../src/types'

export const E2E_ACTIVE_CONFIG_ID = 'abc123'
export const E2E_SINGLE_COLLECTOR_WORKLOAD_ID = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
export const E2E_FIXED_TIMESTAMP = '2026-06-30T10:00:00.000Z'

export function buildCollectorWorkload(overrides: Partial<Workload> = {}): Workload {
  return {
    id: E2E_SINGLE_COLLECTOR_WORKLOAD_ID,
    fingerprint_source: 'k8s',
    fingerprint_keys: { cluster: 'prod', namespace: 'obs', kind: 'deployment', name: 'otel' },
    display_name: 'test-collector',
    type: 'collector',
    version: '0.98.0',
    status: 'connected',
    last_seen_at: E2E_FIXED_TIMESTAMP,
    labels: { team: 'platform', env: 'prod' },
    active_config_id: E2E_ACTIVE_CONFIG_ID,
    accepts_remote_config: true,
    available_components: {
      components: {
        receivers: ['otlp'],
        exporters: ['logging', 'debug'],
      },
    },
    ...overrides,
  }
}

export const scopeCollectorScenarios = {
  singleCollector: buildCollectorWorkload(),
  capableCollector: buildCollectorWorkload({
    id: 'payments-capable-1',
    display_name: 'payments-capable-1',
    labels: { team: 'payments', env: 'prod', cluster: 'prod-eu' },
  }),
  readOnlyCollector: buildCollectorWorkload({
    id: 'payments-ro',
    display_name: 'payments-ro',
    accepts_remote_config: false,
    labels: { team: 'payments', env: 'prod', cluster: 'prod-eu' },
  }),
  incompatibleCollector: buildCollectorWorkload({
    id: 'payments-incompatible',
    display_name: 'payments-incompatible',
    version: '0.74.0',
    labels: { team: 'payments', env: 'prod', cluster: 'prod-eu' },
    available_components: { components: { receivers: ['otlp'], exporters: ['logging'] } },
  }),
  offlineCollector: buildCollectorWorkload({
    id: 'payments-offline',
    display_name: 'payments-offline',
    status: 'disconnected',
    last_seen_at: '2026-06-30T09:30:00.000Z',
    labels: { team: 'payments', env: 'prod', cluster: 'prod-eu' },
  }),
} satisfies Record<string, Workload>

export const savedPushGroups = [
  {
    id: 'payments',
    name: 'Payments collectors',
    description: 'Production payment pipeline collectors',
    selector: { match_labels: { team: 'payments', env: 'prod' }, types: ['collector'] },
  },
  {
    id: 'read-only',
    name: 'Read-only collectors',
    description: 'Collectors that can report but not accept remote config',
    selector: { capabilities: ['report_config'], types: ['collector'] },
  },
] satisfies PushGroup[]

export const dynamicPaymentsSelector = {
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
} satisfies PushGroupSelector

function buildPreviewTarget(
  workload: Workload,
  bucket: PushPreviewTarget['bucket'],
  overrides: Partial<PushPreviewTarget> = {},
): PushPreviewTarget {
  return {
    workload_id: workload.id,
    display_name: workload.display_name,
    type: workload.type,
    version: workload.version,
    status: workload.status,
    bucket,
    accepts_remote_config: workload.accepts_remote_config ?? false,
    ...overrides,
  }
}

export function buildPushPreview(
  targets: PushPreviewTarget[],
  selector: PushGroupSelector,
  overrides: Partial<PushPreview> = {},
): PushPreview {
  return {
    selector,
    targeted_count: targets.length,
    breakdown: {
      remote_config_capable: targets.filter((target) => target.bucket === 'remote_config_capable')
        .length,
      read_only: targets.filter((target) => target.bucket === 'read_only').length,
      incompatible: targets.filter((target) => target.bucket === 'incompatible').length,
      offline: targets.filter((target) => target.bucket === 'offline').length,
    },
    targets,
    ...overrides,
  }
}

const paymentsCapableTargets = Array.from({ length: 5 }, (_, index) =>
  buildPreviewTarget(
    buildCollectorWorkload({
      id: `payments-capable-${index + 1}`,
      display_name: `payments-capable-${index + 1}`,
      labels: { team: 'payments', env: 'prod', cluster: 'prod-eu' },
    }),
    'remote_config_capable',
  ),
)

const dynamicCapableTargets = Array.from({ length: 3 }, (_, index) =>
  buildPreviewTarget(
    buildCollectorWorkload({
      id: `dynamic-capable-${index + 1}`,
      display_name: `dynamic-capable-${index + 1}`,
      labels: { team: 'platform', env: 'prod', cluster: 'prod-eu' },
    }),
    'remote_config_capable',
  ),
)

export const pushPreviewScenarios = {
  savedPaymentsMixedCollectors: buildPushPreview(
    [
      ...paymentsCapableTargets,
      buildPreviewTarget(scopeCollectorScenarios.readOnlyCollector, 'read_only', {
        reason: 'workload does not accept remote config',
      }),
      buildPreviewTarget(scopeCollectorScenarios.incompatibleCollector, 'incompatible', {
        reason: 'collector lacks the debug exporter',
      }),
      buildPreviewTarget(scopeCollectorScenarios.offlineCollector, 'offline', {
        reason: 'workload is not connected',
      }),
    ],
    savedPushGroups[0].selector,
    { group_id: 'payments' },
  ),
  dynamicCapableCollectors: buildPushPreview(dynamicCapableTargets, dynamicPaymentsSelector),
}

export async function mockFeatures(page: Page, features: Record<string, boolean> = {}) {
  return page.route('**/api/features', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ features }),
    }),
  )
}

export async function mockPushGroups(page: Page, groups: PushGroup[] = savedPushGroups) {
  return page.route('**/api/push-groups', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(groups),
    }),
  )
}

interface PushPreviewMockOptions {
  previewsByGroupId?: Record<string, PushPreview>
  dynamicPreview?: PushPreview
}

export async function mockPushPreview(page: Page, options: PushPreviewMockOptions = {}) {
  const previewsByGroupId = options.previewsByGroupId ?? {
    payments: pushPreviewScenarios.savedPaymentsMixedCollectors,
  }
  const dynamicPreview = options.dynamicPreview ?? pushPreviewScenarios.dynamicCapableCollectors

  return page.route('**/api/pushes/preview', async (route) => {
    const request = route.request().postDataJSON() as {
      group_id?: string
      selector?: PushGroupSelector
    }
    const preview = request.group_id ? previewsByGroupId[request.group_id] : dynamicPreview
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ...preview,
        group_id: request.group_id ?? preview?.group_id,
        selector: request.selector ?? preview?.selector ?? {},
      }),
    })
  })
}

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
      const e2eWindow = window as unknown as { __OTEL_MAGNIFY_E2E_DISABLE_WS__?: boolean }
      e2eWindow.__OTEL_MAGNIFY_E2E_DISABLE_WS__ = true
    })
    const defaultMe = buildMe({})
    await page.route('**/api/me**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(defaultMe),
      }),
    )
    await mockFeatures(page, {})
    await page.route('**/api/push-groups', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      }),
    )
    await page.route('**/api/alerts*', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: '[]',
    }))
    await page.route('**/api/configs', (route) => route.fulfill({
      status: 200, contentType: 'application/json', body: '[]',
    }))
    await use(page)
  },
})

export { expect }
