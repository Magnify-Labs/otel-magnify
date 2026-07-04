import { test, expect, mockFeatures, mockMe } from './fixtures'

const editorGroup = {
  id: 'grp_system_editor',
  name: 'editor' as const,
  role: 'editor' as const,
  is_system: true,
  created_at: '2026-06-30T10:00:00.000Z',
}

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

const evidenceReport = {
  schema_version: 'config_safety_evidence_report.v1',
  report_id: 'rpt-config-safety-1234567890',
  generated_at: '2026-07-02T20:00:00Z',
  recommended_version: '0.100.0',
  summary: {
    config_changes: 2,
    validation_failures: 1,
    rollbacks: 1,
    drifted_collectors: 1,
    outdated_collectors: 1,
    audit_events: 4,
  },
  config_changes: [
    {
      workload_id: 'wl-drift',
      display_name: 'collector-prod',
      config_hash: 'cfg-redacted-a',
      previous_hash: 'cfg-redacted-prev',
      status: 'applied',
      pushed_by: 'operator@example.com',
      applied_at: '2026-07-02T18:00:00Z',
      content_available: false,
      diff_summary: 'receivers changed; secret values redacted by backend',
    },
  ],
  validation_failures: [
    {
      workload_id: 'wl-drift',
      display_name: 'collector-prod',
      config_hash: 'cfg-bad',
      status: 'failed',
      error: 'processor batch is unavailable',
      occurred_at: '2026-07-02T18:10:00Z',
    },
  ],
  rollbacks: [
    {
      workload_id: 'wl-drift',
      display_name: 'collector-prod',
      config_hash: 'cfg-known-good',
      rollback_of_push_id: 'push-123',
      status: 'applied',
      occurred_at: '2026-07-02T18:20:00Z',
    },
  ],
  drift: driftDashboard,
  outdated_collectors: [
    {
      workload_id: 'wl-drift',
      display_name: 'collector-prod',
      current_version: '0.88.0',
      recommended_version: '0.100.0',
      severity: 'warning',
      reason: 'collector is behind the recommended version',
    },
  ],
  audit_trail: [
    {
      action: 'report.config_safety.export',
      resource: 'report',
      resource_id: 'rpt-config-safety-1234567890',
      detail: 'format=json',
      at: '2026-07-02T20:00:01Z',
    },
  ],
  signature: {
    algorithm: 'sha256-unsigned-digest-v1',
    payload_digest_sha256: 'abcdef1234567890',
    verification_hint:
      'Community unsigned digest. Enterprise builds may attach a detached signature.',
  },
}

test.describe('Config drift dashboard', () => {
  test.beforeEach(async ({ loggedInPage: page }) => {
    await mockFeatures(page, {
      'config_safety.drift_dashboard': true,
      'reports.evidence_pack': true,
    })
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

  test('shows evidence report summary, signed metadata, and downloadable formats for report exporters', async ({
    loggedInPage: page,
  }) => {
    await mockMe(page, { groups: [editorGroup] })
    const exportedFormats: string[] = []
    await page.route('**/api/reports/config-safety*', async (route) => {
      const url = new URL(route.request().url())
      const format = url.searchParams.get('format') ?? 'json'
      if (format === 'json') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(evidenceReport),
        })
        return
      }
      exportedFormats.push(format)
      await route.fulfill({
        status: 200,
        contentType:
          format === 'pdf' ? 'application/pdf' : format === 'csv' ? 'text/csv' : 'text/markdown',
        body: format === 'pdf' ? '%PDF-1.4' : 'redacted report export',
      })
    })

    await page.goto('/config-safety/drift')

    await expect(page.getByRole('heading', { name: 'Evidence report' })).toBeVisible()
    await expect(page.getByText('Report ID rpt-config-safety-1234567890')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Config changes' })).toBeVisible()
    await expect(
      page
        .getByLabel('Evidence report summary')
        .getByRole('article')
        .filter({ hasText: 'Config changes' }),
    ).toContainText('2')
    await expect(page.getByText('processor batch is unavailable')).toBeVisible()
    await expect(page.getByText('collector-prod').first()).toBeVisible()
    await expect(page.getByText('cfg-redacted…')).toBeVisible()
    await expect(page.getByText('Unsigned digest ready')).toBeVisible()
    await expect(page.getByText('sha256-unsigned-digest-v1')).toBeVisible()
    await expect(
      page.getByText('Community/Pro exports are not shown as enterprise signed.'),
    ).toBeVisible()

    await page.getByRole('button', { name: 'Download Markdown' }).click()
    await page.getByRole('button', { name: 'Download CSV' }).click()
    await page.getByRole('button', { name: 'Download PDF' }).click()

    expect(exportedFormats).toEqual(['markdown', 'csv', 'pdf'])
  })

  test('hides evidence report actions from viewers without report export permission', async ({
    loggedInPage: page,
  }) => {
    await page.goto('/config-safety/drift')

    await expect(page.getByRole('heading', { name: 'Evidence report' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Download Markdown' })).toHaveCount(0)
  })
})
