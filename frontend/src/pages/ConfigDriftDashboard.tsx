import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configSafetyAPI } from '../api/client'
import { useFeature } from '../hooks/useFeature'
import type { ConfigDriftAction, ConfigDriftItem, ConfigDriftSummary } from '../types'
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

export default function ConfigDriftDashboard() {
  const { t } = useTranslation()
  const { enabled: driftDashboardEnabled, isLoading: driftDashboardLoading } = useFeature(
    'config_safety.drift_dashboard',
  )
  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['config-safety', 'drift'],
    queryFn: configSafetyAPI.drift,
    staleTime: 30_000,
    enabled: driftDashboardEnabled,
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
