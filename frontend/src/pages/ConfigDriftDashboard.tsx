import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configSafetyAPI } from '../api/client'
import { useFeature } from '../hooks/useFeature'
import { hasPerm } from '../lib/perm'
import { useStore } from '../store'
import type {
  ConfigDriftAction,
  ConfigDriftItem,
  ConfigDriftSummary,
  EvidenceReport,
  EvidenceReportExportFormat,
} from '../types'
import '../styles/config-drift.css'

const summaryKeys: Array<{
  key: keyof ConfigDriftSummary
  labelKey: string
}> = [
  { key: 'total_collectors', labelKey: 'total_collectors' },
  { key: 'drifted_collectors', labelKey: 'drifted_collectors' },
  { key: 'pending_too_long', labelKey: 'pending_too_long' },
  { key: 'missing_effective_config', labelKey: 'missing_effective_config' },
  { key: 'remote_config_unsupported', labelKey: 'remote_config_unsupported' },
  { key: 'outdated_versions', labelKey: 'outdated_versions' },
  { key: 'unknown_incomplete_components', labelKey: 'unknown_incomplete_components' },
  { key: 'heterogeneous_groups', labelKey: 'heterogeneous_groups' },
]

const reportSummaryKeys: Array<{
  key: keyof EvidenceReport['summary']
  labelKey: string
}> = [
  { key: 'config_changes', labelKey: 'config_changes' },
  { key: 'validation_failures', labelKey: 'validation_failures' },
  { key: 'rollbacks', labelKey: 'rollbacks' },
  { key: 'drifted_collectors', labelKey: 'drifted_collectors' },
  { key: 'outdated_collectors', labelKey: 'outdated_collectors' },
  { key: 'audit_events', labelKey: 'audit_events' },
]

const reportExportFormats = ['markdown', 'csv', 'pdf'] as const satisfies Array<
  Exclude<EvidenceReportExportFormat, 'json'>
>

const actionKeys = [
  'view_diff',
  'validate_current',
  'push_expected',
  'rollback',
  'mark_ignored',
] as const

function shortHash(hash?: string) {
  if (!hash) return '—'
  return hash.length > 12 ? `${hash.slice(0, 12)}…` : hash
}

function formatLastPush(item: ConfigDriftItem) {
  if (!item.last_push) return '—'
  if (item.pending_too_long) return 'Pending >15m'
  return item.last_push.status
}

function translateActionReason(t: ReturnType<typeof useTranslation>['t'], reason?: string) {
  if (!reason) return t('config_drift.action_unavailable')
  return t(`config_drift.action_reasons.${reason}`, { defaultValue: reason })
}

function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

function ActionButton({ actionKey, action }: { actionKey: string; action?: ConfigDriftAction }) {
  const { t } = useTranslation()
  const disabledReason = action?.enabled ? undefined : translateActionReason(t, action?.reason)

  return (
    <span className="config-drift-action-wrap">
      <button
        className="button button-secondary config-drift-action"
        type="button"
        disabled={!action?.enabled}
        title={disabledReason}
      >
        {t(`config_drift.actions.${actionKey}`)}
      </button>
      {disabledReason && <span className="config-drift-action-reason">{disabledReason}</span>}
    </span>
  )
}

function DriftBadge({ item }: { item: ConfigDriftItem }) {
  const { t } = useTranslation()
  const tone =
    item.drift_status === 'in_sync'
      ? 'success'
      : item.drift_status === 'pending_too_long'
        ? 'warning'
        : 'danger'

  return (
    <span className={`config-drift-badge config-drift-badge-${tone}`}>
      {t(`config_drift.status.${item.drift_status}`, {
        defaultValue: item.drift_status.replaceAll('_', ' '),
      })}
    </span>
  )
}

function reportFilename(
  report: EvidenceReport | undefined,
  format: Exclude<EvidenceReportExportFormat, 'json'>,
) {
  const prefix = report?.report_id ? report.report_id.slice(0, 12) : 'config-safety'
  const extension = format === 'markdown' ? 'md' : format
  return `config-safety-evidence-${prefix}.${extension}`
}

function ReportExportButton({
  format,
  report,
}: {
  format: Exclude<EvidenceReportExportFormat, 'json'>
  report?: EvidenceReport
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<'idle' | 'loading' | 'error'>('idle')

  async function exportReport() {
    setStatus('loading')
    try {
      const blob = await configSafetyAPI.exportReport(format, report?.recommended_version)
      saveBlob(blob, reportFilename(report, format))
      setStatus('idle')
    } catch {
      setStatus('error')
    }
  }

  return (
    <span className="config-report-export-action">
      <button
        className="button button-secondary"
        disabled={status === 'loading'}
        onClick={exportReport}
        type="button"
      >
        {status === 'loading'
          ? t('config_drift.report.exporting')
          : t(`config_drift.report.download.${format}`)}
      </button>
      {status === 'error' && (
        <span className="config-drift-error" role="status">
          {t('config_drift.report.export_error')}
        </span>
      )}
    </span>
  )
}

function SignatureMetadata({ report }: { report: EvidenceReport }) {
  const { t } = useTranslation()
  const signature = report.signature
  const isUnsignedDigest = signature.algorithm.includes('unsigned')
  const statusKey = isUnsignedDigest ? 'unsigned_digest' : 'signed'

  return (
    <section
      className="config-report-signature"
      aria-label={t('config_drift.report.signature_aria')}
    >
      <div>
        <span className="config-drift-muted">{t('config_drift.report.signature_status')}</span>
        <strong>{t(`config_drift.report.signature.${statusKey}`)}</strong>
      </div>
      <div>
        <span className="config-drift-muted">{t('config_drift.report.algorithm')}</span>
        <code>{signature.algorithm}</code>
      </div>
      <div>
        <span className="config-drift-muted">{t('config_drift.report.digest')}</span>
        <code>{shortHash(signature.payload_digest_sha256)}</code>
      </div>
      {signature.key_id && (
        <div>
          <span className="config-drift-muted">{t('config_drift.report.key_id')}</span>
          <code>{signature.key_id}</code>
        </div>
      )}
      <p className="panel-hint">{t('config_drift.report.community_signature_note')}</p>
      {signature.verification_hint && <p className="panel-hint">{signature.verification_hint}</p>}
    </section>
  )
}

function EvidenceReportPanel({ report }: { report: EvidenceReport }) {
  const { t } = useTranslation()
  const configChanges = report.config_changes.slice(0, 3)
  const failures = report.validation_failures.slice(0, 3)
  const rollbacks = report.rollbacks.slice(0, 3)
  const auditTrail = report.audit_trail.slice(0, 3)

  return (
    <section
      className="panel config-drift-panel config-report-panel"
      aria-labelledby="config-report-title"
    >
      <header className="panel-head config-report-head">
        <div>
          <h2 className="panel-title" id="config-report-title">
            {t('config_drift.report.title')}
          </h2>
          <p className="panel-hint">
            {t('config_drift.report.subtitle', { id: report.report_id })}
          </p>
        </div>
        <div
          className="config-report-export-actions"
          aria-label={t('config_drift.report.export_aria')}
        >
          {reportExportFormats.map((format) => (
            <ReportExportButton format={format} key={format} report={report} />
          ))}
        </div>
      </header>

      <section
        className="config-report-summary-grid"
        aria-label={t('config_drift.report.summary_aria')}
      >
        {reportSummaryKeys.map(({ key, labelKey }) => (
          <article className="stat-card config-drift-summary-card" key={key}>
            <span>{t(`config_drift.report.summary.${labelKey}`)}</span>
            <strong>{report.summary[key]}</strong>
          </article>
        ))}
      </section>

      <SignatureMetadata report={report} />

      <div className="config-report-detail-grid">
        <article>
          <h3>{t('config_drift.report.sections.config_changes')}</h3>
          {configChanges.length === 0 ? (
            <p className="panel-hint">{t('config_drift.report.empty.config_changes')}</p>
          ) : (
            <ul>
              {configChanges.map((change) => (
                <li key={`${change.workload_id}-${change.config_hash}-${change.applied_at}`}>
                  <strong>{change.display_name ?? change.workload_id}</strong> →{' '}
                  <code>{shortHash(change.config_hash)}</code>
                  <span>{change.status}</span>
                  {change.diff_summary && <small>{change.diff_summary}</small>}
                </li>
              ))}
            </ul>
          )}
        </article>
        <article>
          <h3>{t('config_drift.report.sections.validation_failures')}</h3>
          {failures.length === 0 ? (
            <p className="panel-hint">{t('config_drift.report.empty.validation_failures')}</p>
          ) : (
            <ul>
              {failures.map((failure) => (
                <li key={`${failure.workload_id}-${failure.config_hash}-${failure.occurred_at}`}>
                  <strong>{failure.display_name ?? failure.workload_id}</strong>
                  <span>{failure.error}</span>
                </li>
              ))}
            </ul>
          )}
        </article>
        <article>
          <h3>{t('config_drift.report.sections.rollbacks')}</h3>
          {rollbacks.length === 0 ? (
            <p className="panel-hint">{t('config_drift.report.empty.rollbacks')}</p>
          ) : (
            <ul>
              {rollbacks.map((rollback) => (
                <li key={`${rollback.workload_id}-${rollback.config_hash}-${rollback.occurred_at}`}>
                  <strong>{rollback.display_name ?? rollback.workload_id}</strong>
                  <span>{rollback.status}</span>
                  <code>{shortHash(rollback.config_hash)}</code>
                </li>
              ))}
            </ul>
          )}
        </article>
        <article>
          <h3>{t('config_drift.report.sections.audit_trail')}</h3>
          {auditTrail.length === 0 ? (
            <p className="panel-hint">{t('config_drift.report.empty.audit_trail')}</p>
          ) : (
            <ul>
              {auditTrail.map((entry) => (
                <li key={`${entry.action}-${entry.resource_id ?? entry.at}`}>
                  <strong>{entry.action}</strong>
                  <span>{entry.detail ?? entry.resource}</span>
                </li>
              ))}
            </ul>
          )}
        </article>
      </div>
    </section>
  )
}

export default function ConfigDriftDashboard() {
  const { t } = useTranslation()
  const me = useStore((s) => s.me)
  const { enabled: driftDashboardEnabled, isLoading: driftDashboardLoading } = useFeature(
    'config_safety.drift_dashboard',
  )
  const canExportReports = hasPerm(me?.groups, 'reports:export')
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['config-safety', 'drift'],
    queryFn: configSafetyAPI.drift,
    staleTime: 30_000,
    enabled: driftDashboardEnabled,
  })
  const reportQuery = useQuery({
    queryKey: ['config-safety', 'report'],
    queryFn: () => configSafetyAPI.report(),
    staleTime: 30_000,
    enabled: driftDashboardEnabled && canExportReports,
  })

  if (driftDashboardLoading) {
    return (
      <section className="panel config-drift-panel">
        <p className="panel-hint">{t('common.loading')}</p>
      </section>
    )
  }

  if (!driftDashboardEnabled) {
    return (
      <section className="panel config-drift-panel">
        <h1 className="page-title">{t('config_drift.disabled.title')}</h1>
        <p className="panel-hint">{t('config_drift.disabled.body')}</p>
      </section>
    )
  }

  return (
    <div>
      <header className="page-header">
        <div>
          <h1 className="page-title">{t('config_drift.title')}</h1>
          <p className="page-subtitle">{t('config_drift.subtitle')}</p>
        </div>
      </header>

      {isLoading ? (
        <section className="panel config-drift-panel">
          <p className="panel-hint">{t('common.loading')}</p>
        </section>
      ) : isError || !data ? (
        <section className="panel config-drift-panel">
          <p className="config-drift-error">{t('config_drift.error')}</p>
          <button className="button button-secondary" type="button" onClick={() => refetch()}>
            {t('common.retry')}
          </button>
        </section>
      ) : (
        <>
          <section
            className="config-drift-summary-grid"
            aria-label={t('config_drift.summary_aria')}
          >
            {summaryKeys.map(({ key, labelKey }) => (
              <article className="stat-card config-drift-summary-card" key={key}>
                <span>{t(`config_drift.summary.${labelKey}`)}</span>
                <strong>{data.summary[key]}</strong>
              </article>
            ))}
          </section>

          {canExportReports &&
            (reportQuery.isLoading ? (
              <section className="panel config-drift-panel">
                <p className="panel-hint">{t('config_drift.report.loading')}</p>
              </section>
            ) : reportQuery.isError || !reportQuery.data ? (
              <section className="panel config-drift-panel">
                <p className="config-drift-error">{t('config_drift.report.error')}</p>
                <button
                  className="button button-secondary"
                  type="button"
                  onClick={() => reportQuery.refetch()}
                >
                  {t('common.retry')}
                </button>
              </section>
            ) : (
              <EvidenceReportPanel report={reportQuery.data} />
            ))}

          <section className="panel config-drift-panel" aria-labelledby="config-drift-table-title">
            <header className="panel-head">
              <div>
                <h2 className="panel-title" id="config-drift-table-title">
                  {t('config_drift.table_title')}
                </h2>
                <p className="panel-hint">{t('config_drift.table_hint')}</p>
              </div>
            </header>

            <div className="config-drift-table-wrap">
              <table className="config-drift-table">
                <thead>
                  <tr>
                    <th>{t('config_drift.columns.collector')}</th>
                    <th>{t('config_drift.columns.env')}</th>
                    <th>{t('config_drift.columns.version')}</th>
                    <th>{t('config_drift.columns.expected')}</th>
                    <th>{t('config_drift.columns.effective')}</th>
                    <th>{t('config_drift.columns.drift')}</th>
                    <th>{t('config_drift.columns.last_push')}</th>
                    <th>{t('config_drift.columns.action')}</th>
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((item) => (
                    <tr key={item.workload_id}>
                      <td>
                        <strong>{item.collector}</strong>
                        <span className="config-drift-muted">{item.workload_id}</span>
                      </td>
                      <td>{item.env || '—'}</td>
                      <td>{item.version || '—'}</td>
                      <td className="config-drift-hash">{shortHash(item.expected_config_hash)}</td>
                      <td className="config-drift-hash">
                        {item.effective_config_hashes && item.effective_config_hashes.length > 1
                          ? item.effective_config_hashes.map(shortHash).join(', ')
                          : shortHash(item.effective_config_hash)}
                      </td>
                      <td>
                        <DriftBadge item={item} />
                        {item.has_version_outdated_alert && (
                          <span className="config-drift-chip">{t('config_drift.outdated')}</span>
                        )}
                      </td>
                      <td>{formatLastPush(item)}</td>
                      <td>
                        <div className="config-drift-actions">
                          {actionKeys.map((actionKey) => (
                            <ActionButton
                              action={item.actions?.[actionKey]}
                              actionKey={actionKey}
                              key={actionKey}
                            />
                          ))}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
    </div>
  )
}
