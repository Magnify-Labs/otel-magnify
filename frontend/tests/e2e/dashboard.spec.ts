import { test, expect, mockFeatures } from './fixtures'

const mockWorkloads = [
  {
    id: 'w1',
    fingerprint_source: 'k8s',
    fingerprint_keys: {},
    display_name: 'coll-a',
    type: 'collector',
    version: '0.100.0',
    status: 'connected',
    last_seen_at: new Date().toISOString(),
    labels: {},
    accepts_remote_config: true,
  },
  {
    id: 'w2',
    fingerprint_source: 'k8s',
    fingerprint_keys: {},
    display_name: 'coll-b',
    type: 'collector',
    version: '0.100.0',
    status: 'degraded',
    last_seen_at: new Date().toISOString(),
    labels: {},
    accepts_remote_config: false,
  },
  {
    id: 'w3',
    fingerprint_source: 'uid',
    fingerprint_keys: {},
    display_name: 'sdk-a',
    type: 'sdk',
    version: '0.99.0',
    status: 'disconnected',
    last_seen_at: new Date().toISOString(),
    labels: {},
  },
]

const mockActivity = [
  { day: '2026-04-16', count: 1 },
  { day: '2026-04-17', count: 0 },
  { day: '2026-04-18', count: 2 },
  { day: '2026-04-19', count: 0 },
  { day: '2026-04-20', count: 4 },
  { day: '2026-04-21', count: 1 },
  { day: '2026-04-22', count: 3 },
]

test.describe('Dashboard', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await page.route('**/api/workloads*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockWorkloads),
      }),
    )
    await page.route('**/api/workloads/version-intelligence*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'fleet-version-intelligence.v1',
          recommended_version: '0.100.0',
          version_matrix: [],
          collectors_below_recommended: [],
          unsupported_config_components: [],
          invalid_versions: [],
          recommendations: [],
        }),
      }),
    )
    await page.route('**/api/alerts*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: '[]',
      }),
    )
    await page.route('**/api/pushes/activity*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(mockActivity),
      }),
    )
  })

  test('renders the six stat cards', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.stat-grid .stat-card')).toHaveCount(6)
  })

  test('fleet health donut renders with correct total', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.fleet-donut text')).toHaveText('3')
  })

  test('push activity chart renders 7 bars', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.push-chart rect')).toHaveCount(7)
    await expect(page.locator('.push-chart rect.push-chart-bar-last')).toHaveCount(1)
  })

  test('config safety status summarizes supervised config readiness', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/')
    await expect(
      page.getByRole('heading', { name: /Config safety status|Statut de sécurité des configs/ }),
    ).toBeVisible()
    await expect(page.locator('.config-safety-panel')).toContainText(
      /Supervised collectors|Collecteurs supervisés/,
    )
    await expect(page.locator('.config-safety-panel')).toContainText('1')
    await expect(page.locator('.config-safety-panel')).toContainText(/Last 7d pushes|Pushes sur 7j/)
    await expect(page.locator('.config-safety-panel')).toContainText('11')
    await expect(
      page.getByRole('link', { name: /Review supervised workloads|Voir les workloads supervisés/ }),
    ).toHaveAttribute('href', '/inventory?control=supervised')
    await expect(
      page.getByRole('link', { name: /Open fleet drift dashboard|Ouvrir le tableau des dérives/ }),
    ).toHaveCount(0)
  })

  test('config safety status drift CTA is hidden without the drift dashboard feature', async ({
    loggedInPage: page,
  }) => {
    await mockFeatures(page)

    await page.goto('/')

    const panel = page.locator('.config-safety-status-panel')
    await expect(panel).toBeVisible()
    await expect(panel.getByRole('link', { name: 'Review supervised workloads' })).toHaveAttribute(
      'href',
      '/inventory?control=supervised',
    )
    await expect(panel.getByText('Supervised collectors', { exact: true })).toBeVisible()
    await expect(panel.getByText('Last 7d pushes')).toBeVisible()
    await expect(panel.getByRole('link', { name: 'Open fleet drift dashboard' })).toHaveCount(0)
  })

  test('config safety status drift CTA is visible with the drift dashboard feature', async ({
    loggedInPage: page,
  }) => {
    await mockFeatures(page, { 'config_safety.drift_dashboard': true })

    await page.goto('/')

    const panel = page.locator('.config-safety-status-panel')
    await expect(panel.getByRole('link', { name: 'Open fleet drift dashboard' })).toHaveAttribute(
      'href',
      '/config-safety/drift',
    )
  })

  test('config safety status panel summarizes supervised collectors and safe flow', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/')

    const panel = page.locator('.config-safety-status-panel')
    await expect(panel).toBeVisible()
    await expect(panel).toContainText('Safe remote config across supervised collectors')
    await expect(panel).toContainText('Supervised collectors')
    await expect(panel).toContainText('1')
    await expect(panel).toContainText('Last 7d pushes')
    await expect(panel).toContainText('11')
    await expect(panel).toContainText(
      'Validation, diff, safe push and rollback are available on workload detail pages.',
    )
    await expect(panel.getByRole('link', { name: 'Review supervised workloads' })).toHaveAttribute(
      'href',
      '/inventory?control=supervised',
    )

    const pushPanelTop = await page.locator('.panel', { hasText: 'Push activity' }).boundingBox()
    const safetyTop = await panel.boundingBox()
    const alertsTop = await page.locator('.panel', { hasText: 'Recent alerts' }).boundingBox()
    expect(pushPanelTop?.y).toBeLessThan(safetyTop?.y ?? Number.POSITIVE_INFINITY)
    expect(safetyTop?.y).toBeLessThan(alertsTop?.y ?? Number.POSITIVE_INFINITY)
  })

  test('deployed versions panel groups by version', async ({ loggedInPage: page }) => {
    await page.goto('/')
    await expect(page.locator('.versions-row')).toHaveCount(2)
    await expect(page.locator('.versions-row').first()).toContainText('0.100.0')
    await expect(page.locator('.versions-row').first()).toContainText('2')
  })

  test('hides fleet version intelligence and does not call paid endpoint without feature', async ({
    loggedInPage: page,
  }) => {
    let versionIntelligenceHit = false
    await page.route('**/api/workloads/version-intelligence*', (route) => {
      versionIntelligenceHit = true
      return route.fulfill({ status: 500, body: 'version intelligence should be gated' })
    })

    await page.goto('/')

    await expect(page.getByRole('heading', { name: 'Fleet version intelligence' })).toHaveCount(0)
    expect(versionIntelligenceHit).toBe(false)
  })

  test('renders fleet version intelligence recommendations', async ({ loggedInPage: page }) => {
    await mockFeatures(page, { 'config_safety.version_intelligence': true })
    await page.route('**/api/workloads/version-intelligence*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'fleet-version-intelligence.v1',
          recommended_version: '0.100.0',
          version_matrix: [
            {
              group: 'payments',
              type: 'collector',
              status: 'connected',
              version: '0.99.0',
              count: 1,
              workload_ids: ['w1'],
            },
            {
              group: 'payments',
              type: 'collector',
              status: 'connected',
              version: '0.100.0',
              count: 2,
              workload_ids: ['w2', 'w3'],
            },
          ],
          collectors_below_recommended: [
            {
              workload_id: 'w1',
              display_name: 'collector-payments-a',
              group: 'payments',
              version: '0.99.0',
              recommended_version: '0.100.0',
            },
          ],
          unsupported_config_components: [
            {
              workload_id: 'w1',
              display_name: 'collector-payments-a',
              config_hash: 'cfg_123456',
              category: 'receivers',
              component_type: 'kafka',
              path: 'receivers.kafka',
              available_hash: 'cap_abc',
              available_types: ['otlp'],
            },
          ],
          invalid_versions: [],
          recommendations: [
            {
              action: 'upgrade_collector',
              workload_id: 'w1',
              reason: 'collector version 0.99.0 is below recommended 0.100.0',
            },
            {
              action: 'choose_older_config',
              workload_id: 'w1',
              config_hash: 'cfg_123456',
              reason: 'current collector capabilities do not support this config component',
              components: ['kafka'],
            },
            {
              action: 'remove_component',
              workload_id: 'w1',
              config_hash: 'cfg_123456',
              reason: 'remove or replace unsupported component before pushing this config',
              components: ['kafka'],
            },
          ],
        }),
      }),
    )

    await page.goto('/')

    await expect(page.getByRole('heading', { name: 'Fleet version intelligence' })).toBeVisible()
    await expect(page.getByText('Recommended collector version: 0.100.0')).toBeVisible()
    await expect(page.locator('.version-intelligence-row')).toHaveCount(2)
    await expect(page.locator('.version-intelligence-row').first()).toContainText('payments')
    await expect(page.locator('.version-intelligence-row').first()).toContainText('0.99.0')
    await expect(page.locator('.version-intelligence-row').first()).toContainText(
      'Below recommended',
    )
    await expect(page.getByText('collector-payments-a is running 0.99.0')).toBeVisible()
    await expect(page.getByText('Upgrade collector to 0.100.0')).toBeVisible()
    await expect(page.getByText('receivers.kafka uses unsupported kafka')).toBeVisible()
    await expect(
      page.getByText('Choose an older config, upgrade collector, or remove kafka.'),
    ).toBeVisible()
  })

  test('fleet version matrix renders groups, versions, status badges, and counts', async ({
    loggedInPage: page,
  }) => {
    await mockFeatures(page, { 'config_safety.version_intelligence': true })
    await page.route('**/api/workloads/version-intelligence*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'fleet-version-intelligence.v1',
          recommended_version: '0.100.0',
          version_matrix: [
            {
              group: 'payments',
              type: 'collector',
              status: 'connected',
              version: '0.99.0',
              version_status: 'below_recommended',
              count: 1,
              workload_ids: ['collector-payments-a'],
            },
            {
              group: 'checkout',
              type: 'collector',
              status: 'degraded',
              version: '0.100.0',
              version_status: 'at_recommended',
              count: 2,
              workload_ids: ['collector-checkout-a', 'collector-checkout-b'],
            },
            {
              group: 'mobile',
              type: 'sdk',
              status: 'disconnected',
              version: 'not reported',
              version_status: 'not_applicable',
              count: 3,
              workload_ids: ['sdk-mobile-a', 'sdk-mobile-b', 'sdk-mobile-c'],
            },
          ],
          collectors_below_recommended: [],
          unsupported_config_components: [],
          invalid_versions: [],
          recommendations: [],
        }),
      }),
    )

    await page.goto('/')

    const matrix = page.getByRole('table', { name: 'Fleet version matrix' })
    const firstRow = matrix.locator('.version-intelligence-row').nth(0)
    const secondRow = matrix.locator('.version-intelligence-row').nth(1)
    const thirdRow = matrix.locator('.version-intelligence-row').nth(2)

    await expect(matrix).toBeVisible()
    await expect(matrix.locator('.version-intelligence-row')).toHaveCount(3)
    await expect(firstRow).toContainText('payments')
    await expect(firstRow).toContainText('Collector · Connected')
    await expect(firstRow).toContainText('0.99.0')
    await expect(firstRow).toContainText('Below recommended')
    await expect(firstRow).toContainText('1 workloads')
    await expect(secondRow).toContainText('checkout')
    await expect(secondRow).toContainText('Collector · Degraded')
    await expect(secondRow).toContainText('0.100.0')
    await expect(secondRow).toContainText('At recommended')
    await expect(secondRow).toContainText('2 workloads')
    await expect(thirdRow).toContainText('mobile')
    await expect(thirdRow).toContainText('SDK · Disconnected')
    await expect(thirdRow).toContainText('not reported')
    await expect(thirdRow).toContainText('N/A')
    await expect(thirdRow).toContainText('3 workloads')
  })

  test('fleet version intelligence surfaces all three recommendation paths for unsupported components', async ({
    loggedInPage: page,
  }) => {
    await mockFeatures(page, { 'config_safety.version_intelligence': true })
    await page.route('**/api/workloads/version-intelligence*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'fleet-version-intelligence.v1',
          recommended_version: '0.100.0',
          version_matrix: [
            {
              group: 'payments',
              type: 'collector',
              status: 'connected',
              version: '0.99.0',
              version_status: 'below_recommended',
              count: 1,
              workload_ids: ['w1'],
            },
          ],
          collectors_below_recommended: [
            {
              workload_id: 'w1',
              display_name: 'collector-payments-a',
              group: 'payments',
              version: '0.99.0',
              recommended_version: '0.100.0',
            },
          ],
          unsupported_config_components: [
            {
              workload_id: 'w1',
              display_name: 'collector-payments-a',
              config_hash: 'cfg_123456',
              category: 'receivers',
              component_type: 'kafka',
              path: 'receivers.kafka',
              available_hash: 'cap_abc',
              available_types: ['otlp'],
            },
          ],
          invalid_versions: [],
          recommendations: [
            {
              action: 'upgrade_collector',
              workload_id: 'w1',
              reason: 'Upgrade collector before applying config requiring kafka.',
              components: ['kafka'],
            },
            {
              action: 'choose_older_config',
              workload_id: 'w1',
              config_hash: 'cfg_123456',
              reason: 'Choose a config built for collector 0.99.0.',
              components: ['kafka'],
            },
            {
              action: 'remove_component',
              workload_id: 'w1',
              config_hash: 'cfg_123456',
              reason: 'Remove kafka from this config before applying it.',
              components: ['kafka'],
            },
          ],
        }),
      }),
    )

    await page.goto('/')

    const recommendations = page.getByLabel('Actionable recommendations')

    await expect(page.getByText('collector-payments-a is running 0.99.0')).toBeVisible()
    await expect(page.getByText('receivers.kafka uses unsupported kafka')).toBeVisible()
    await expect(recommendations.getByText('Upgrade collector', { exact: true })).toBeVisible()
    await expect(
      recommendations.getByText('Upgrade collector before applying config requiring kafka.'),
    ).toBeVisible()
    await expect(recommendations.getByText('Choose older config', { exact: true })).toBeVisible()
    await expect(
      recommendations.getByText('Choose a config built for collector 0.99.0.'),
    ).toBeVisible()
    await expect(
      recommendations.getByText('Remove or replace component', { exact: true }),
    ).toBeVisible()
    await expect(
      recommendations.getByText('Remove kafka from this config before applying it.'),
    ).toBeVisible()
  })

  test('fleet version intelligence localizes recommendation reasons in French', async ({
    loggedInPage: page,
  }) => {
    await mockFeatures(page, { 'config_safety.version_intelligence': true })
    await page.addInitScript(() => window.localStorage.setItem('lang', 'fr'))
    await page.route('**/api/workloads/version-intelligence*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'fleet-version-intelligence.v1',
          recommended_version: '0.100.0',
          version_matrix: [
            {
              group: 'payments',
              type: 'collector',
              status: 'connected',
              version: '0.99.0',
              version_status: 'below_recommended',
              count: 1,
              workload_ids: ['w1'],
            },
          ],
          collectors_below_recommended: [
            {
              workload_id: 'w1',
              display_name: 'collector-payments-a',
              group: 'payments',
              version: '0.99.0',
              recommended_version: '0.100.0',
            },
          ],
          unsupported_config_components: [
            {
              workload_id: 'w1',
              display_name: 'collector-payments-a',
              config_hash: 'cfg_123456',
              category: 'receivers',
              component_type: 'kafka',
              path: 'receivers.kafka',
            },
          ],
          invalid_versions: [],
          recommendations: [
            {
              action: 'upgrade_collector',
              workload_id: 'w1',
              reason: 'Upgrade collector before applying config requiring kafka.',
              components: ['kafka'],
            },
            {
              action: 'choose_older_config',
              workload_id: 'w1',
              config_hash: 'cfg_123456',
              reason: 'Choose a config built for collector 0.99.0.',
              components: ['kafka'],
            },
            {
              action: 'remove_component',
              workload_id: 'w1',
              config_hash: 'cfg_123456',
              reason: 'Remove kafka from this config before applying it.',
              components: ['kafka'],
            },
          ],
        }),
      }),
    )

    await page.goto('/')

    const recommendations = page.getByLabel('Recommandations actionnables')
    await expect(page.getByText('Intelligence versions flotte')).toBeVisible()
    await expect(page.getByText('Le collecteur est sous la version recommandée.')).toBeVisible()
    await expect(
      recommendations.getByText('Mettez à niveau le collecteur avant d’appliquer cette config.'),
    ).toBeVisible()
    await expect(
      recommendations.getByText(
        'Choisissez une config plus ancienne compatible avec les capacités actuelles du collecteur.',
      ),
    ).toBeVisible()
    await expect(
      recommendations.getByText('Retirez ou remplacez kafka avant d’appliquer cette config.'),
    ).toBeVisible()
    await expect(
      page.getByText('Upgrade collector before applying config requiring kafka.'),
    ).toHaveCount(0)
    await expect(page.getByText('Choose a config built for collector 0.99.0.')).toHaveCount(0)
    await expect(page.getByText('Remove kafka from this config before applying it.')).toHaveCount(0)
  })

  test('fleet version intelligence represents loading, empty, and error states', async ({
    loggedInPage: page,
  }) => {
    await mockFeatures(page, { 'config_safety.version_intelligence': true })
    await page.route('**/api/workloads/version-intelligence*', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 1000))
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 'fleet-version-intelligence.v1',
          recommended_version: '0.100.0',
          version_matrix: [],
          collectors_below_recommended: [],
          unsupported_config_components: [],
          invalid_versions: [],
          recommendations: [],
        }),
      })
    })

    await page.goto('/')
    await expect(page.getByText('Loading version intelligence…')).toBeVisible()
    await expect(page.getByText('No version intelligence data yet.')).toBeVisible()

    await page.unroute('**/api/workloads/version-intelligence*')
    await page.route('**/api/workloads/version-intelligence*', (route) =>
      route.fulfill({ status: 500, contentType: 'text/plain', body: 'boom' }),
    )
    await page.reload()

    await expect(page.getByText('Version intelligence is unavailable.')).toBeVisible()
  })

  test('clicking the Collectors stat card navigates to filtered inventory', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/')
    await page.locator('.stat-card', { hasText: /Collectors|Collecteurs/ }).click()
    await expect(page).toHaveURL(/\/inventory\?type=collector/)
  })
})
