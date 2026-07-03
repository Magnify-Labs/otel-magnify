import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { workloadsAPI } from '../../api/client'
import type { Instance } from '../../types'

interface Props {
  workloadId: string
  activeConfigHash?: string
}

function shortHash(hash?: string) {
  return hash ? hash.slice(0, 8) : '—'
}

function formatRelative(value?: string) {
  if (!value) return '—'
  const timestamp = new Date(value).getTime()
  if (!Number.isFinite(timestamp)) return '—'
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`
}

function statusKey(instance: Instance) {
  return instance.remote_config_status?.status ?? 'no_status'
}

export default function InstancesTab({ workloadId, activeConfigHash }: Props) {
  const { t } = useTranslation()
  const {
    data: topology,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ['workload-topology', workloadId],
    queryFn: () => workloadsAPI.topology(workloadId),
    refetchInterval: 5000,
  })

  if (isLoading) {
    return <div className="loading">{t('workloads.instances.loading')}</div>
  }
  if (isError) {
    return <div className="error-text">{t('workloads.instances.error')}</div>
  }

  const instances = topology?.instances ?? []
  const summary = topology?.summary
  if (instances.length === 0) {
    return <div className="empty-state">{t('workloads.instances.empty')}</div>
  }

  return (
    <div className="instances-panel">
      {summary && (
        <div className="topology-summary" aria-label={t('workloads.instances.summary_aria')}>
          <span>
            {t('workloads.instances.summary.connected', { count: summary.connected_count })}
          </span>
          <span>{t('workloads.instances.summary.healthy', { count: summary.healthy_count })}</span>
          <span>{t('workloads.instances.summary.drifted', { count: summary.drifted_count })}</span>
          <span>
            {t('workloads.instances.summary.failed', {
              count: summary.remote_config_status_counts.failed ?? 0,
            })}
          </span>
        </div>
      )}

      {summary?.heterogeneous && (
        <div className="topology-warning-box" role="note">
          <strong>{t('workloads.instances.warnings.title')}</strong>
          <ul className="topology-warning-list">
            {summary.heterogeneity_reasons.map((reason) => (
              <li key={reason}>
                {t(`workloads.instances.warnings.reason.${reason}`, { defaultValue: reason })}
              </li>
            ))}
          </ul>
        </div>
      )}

      <table className="instances-table">
        <thead>
          <tr>
            <th>{t('workloads.instances.table.instance')}</th>
            <th>{t('workloads.instances.table.pod')}</th>
            <th>{t('workloads.instances.table.health')}</th>
            <th>{t('workloads.instances.table.last_message')}</th>
            <th>{t('workloads.instances.table.version')}</th>
            <th>{t('workloads.instances.table.effective_config')}</th>
            <th>{t('workloads.instances.table.config_status')}</th>
          </tr>
        </thead>
        <tbody>
          {instances.map((instance) => {
            const drift = Boolean(
              activeConfigHash &&
              instance.effective_config_hash &&
              instance.effective_config_hash !== activeConfigHash,
            )
            const remoteStatus = instance.remote_config_status
            return (
              <tr key={instance.instance_uid} className={drift ? 'instance-drift' : undefined}>
                <td className="mono">{instance.instance_uid.slice(0, 8)}</td>
                <td>{instance.pod_name || '—'}</td>
                <td>
                  <span className={`instance-health ${instance.healthy ? 'healthy' : 'unhealthy'}`}>
                    {instance.healthy
                      ? t('workloads.instances.health.healthy')
                      : t('workloads.instances.health.unhealthy')}
                  </span>
                </td>
                <td>{formatRelative(instance.last_message_at)}</td>
                <td>{instance.version || '—'}</td>
                <td className="mono">
                  {shortHash(instance.effective_config_hash)}
                  {drift && (
                    <span className="instance-drift-tag">{t('workloads.instances.drift')}</span>
                  )}
                </td>
                <td>
                  <span
                    className={`instance-config-status instance-config-status-${statusKey(instance)}`}
                  >
                    {t(`workloads.instances.config_status.${statusKey(instance)}`, {
                      defaultValue: statusKey(instance),
                    })}
                  </span>
                  {remoteStatus?.error_message && (
                    <span className="instance-config-error">{remoteStatus.error_message}</span>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
