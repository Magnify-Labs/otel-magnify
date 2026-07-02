import { test, expect } from './fixtures'

const driftDashboard = {
  generated_at: new Date().toISOString(),
  summary: {
    total_collectors: 2,
    drifted_collectors: 1,
    pending_too_long: 1,
    missing_effective_config: 1,
    remote_config_unsupported: 0,
    outdated_versions: 1,
    unknown_incomplete_components: 1,
    heterogeneous_groups: 0,
  },
  items: [
    {
      workload_id: 'wl-drift',
      collector: 'collector-prod',
      env: 'prod',
      version: '0.88.0',
      expected_config_hash: 'expected-a',
      effective_config_hash: 'effective-b',
      effective_config_hashes: ['effective-b'],
      drift_status: 'drifted',
      drift_reasons: ['drifted', 'version_outdated'],
      pending_too_long: false,
      accepts_remote_config: true,
      missing_effective_config: false,
      unknown_incomplete_components: false,
      group_heterogeneous_config: false,
      has_config_drift_alert: true,
      has_version_outdated_alert: true,
      actions: {
        view_diff: { enabled: false, reason: 'diff_requires_config_content' },
        validate_current: { enabled: false, reason: 'validate_from_workload_detail' },
        push_expected: { enabled: false, reason: 'review_expected_before_push' },
        rollback: { enabled: false, reason: 'rollback_from_workload_detail' },
        mark_ignored: { enabled: false, reason: 'ignore_not_implemented' },
      },
    },
    {
      workload_id: 'wl-pending',
      collector: 'collector-staging',
      env: 'staging',
      version: '0.100.0',
      expected_config_hash: 'expected-p',
      drift_status: 'missing_effective_config',
      drift_reasons: ['missing_effective_config', 'pending_too_long'],
      pending_too_long: true,
      accepts_remote_config: true,
      missing_effective_config: true,
      unknown_incomplete_components: true,
      group_heterogeneous_config: false,
      has_config_drift_alert: false,
      has_version_outdated_alert: false,
      last_push: {
        workload_id: 'wl-pending',
        config_id: 'expected-p',
        applied_at: new Date().toISOString(),
        status: 'sent',
      },
      actions: {
        view_diff: { enabled: false, reason: 'diff_requires_config_content' },
        validate_current: { enabled: false, reason: 'validate_from_workload_detail' },
        push_expected: { enabled: false, reason: 'push_pending_too_long' },
        rollback: { enabled: false, reason: 'rollback_from_workload_detail' },
        mark_ignored: { enabled: false, reason: 'ignore_not_implemented' },
      },
    },
  ],
}

test.describe('Config drift dashboard', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await page.route('**/api/config-safety/drift', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(driftDashboard),
      }),
    )
  })

  test('renders summary cards, drift table, and explicit disabled action reasons', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/config-safety/drift')

    await expect(page.getByRole('heading', { name: 'Fleet drift dashboard' })).toBeVisible()
    await expect(page.getByText('Collectors with drift')).toBeVisible()
    await expect(
      page.getByRole('article').filter({ hasText: 'Collectors with drift' }).getByRole('strong'),
    ).toHaveText('1')

    const row = page.getByRole('row', { name: /collector-prod/ })
    await expect(row).toContainText('prod')
    await expect(row).toContainText('0.88.0')
    await expect(row).toContainText('expected-a')
    await expect(row).toContainText('effective-b')
    await expect(row).toContainText('Drifted')
    await expect(row.getByRole('button', { name: 'View diff' })).toBeDisabled()
    await expect(row).toContainText('Diff view needs retrievable config content')
    await expect(row.getByRole('button', { name: 'Mark ignored' })).toBeDisabled()
    await expect(row).toContainText('Ignore persistence is not implemented yet.')

    const pending = page.getByRole('row', { name: /collector-staging/ })
    await expect(pending).toContainText('Missing effective config')
    await expect(pending).toContainText('Pending >15m')
    await expect(pending.getByRole('button', { name: 'Push expected' })).toBeDisabled()
  })

  test('localizes disabled action reasons in French', async ({ loggedInPage: page }) => {
    await page.addInitScript(() => window.localStorage.setItem('lang', 'fr'))
    await page.goto('/config-safety/drift')

    await expect(page.getByRole('heading', { name: 'Dashboard de drift flotte' })).toBeVisible()
    const row = page.getByRole('row', { name: /collector-prod/ })
    await expect(row.getByRole('button', { name: 'Voir diff' })).toBeDisabled()
    await expect(row).toContainText('La vue diff nécessite un contenu de config récupérable')
    await expect(row).not.toContainText('Diff view needs retrievable config content')
  })
})
