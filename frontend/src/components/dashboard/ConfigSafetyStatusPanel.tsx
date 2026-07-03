import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { pushesAPI } from '../../api/client'
import { useFeature } from '../../hooks/useFeature'
import ConfigEvidencePanel from './ConfigEvidencePanel'
import type { Workload } from '../../types'
import { isSupervised } from '../../lib/workloadCapabilities'

interface Props {
  workloads: Workload[]
  isLoading?: boolean
  isError?: boolean
}

export default function ConfigSafetyStatusPanel({
  workloads,
  isLoading = false,
  isError = false,
}: Props) {
  const { t } = useTranslation()
  const { enabled: driftDashboardEnabled } = useFeature('config_safety.drift_dashboard')
  const { data: activity, isError: activityError } = useQuery({
    queryKey: ['push-activity', '7d'],
    queryFn: () => pushesAPI.activity('7d'),
    staleTime: 60_000,
  })

  const supervised = workloads.filter(isSupervised)
  const last7dPushes = activity?.reduce((acc, point) => acc + point.count, 0)
  const hasApplying = supervised.some((w) => w.remote_config_status?.status === 'applying')
  const hasFailed = supervised.some((w) => w.remote_config_status?.status === 'failed')

  let statusLine = t('dashboard.config_safety.status.ready')
  let tone = 'success'
  if (supervised.length === 0) {
    statusLine = t('dashboard.config_safety.status.empty')
    tone = 'neutral'
  } else if (hasFailed) {
    statusLine = t('dashboard.config_safety.status.failed')
    tone = 'danger'
  } else if (hasApplying) {
    statusLine = t('dashboard.config_safety.status.applying')
    tone = 'active'
  }

  return (
    <section
      className="panel config-safety-status-panel config-safety-panel"
      aria-labelledby="config-safety-status-title"
    >
      <header className="panel-head">
        <div>
          <h2 className="panel-title" id="config-safety-status-title">
            {t('dashboard.config_safety.title')}
          </h2>
          <p className="panel-hint">{t('dashboard.config_safety.hint')}</p>
        </div>
      </header>

      {isLoading ? (
        <p className="config-safety-status-line config-safety-status-line-active">
          {t('dashboard.config_safety.loading')}
        </p>
      ) : isError ? (
        <p className="config-safety-status-line config-safety-status-line-danger">
          {t('dashboard.config_safety.error')}
        </p>
      ) : (
        <>
          <div className="config-safety-metrics">
            <div className="config-safety-metric-row">
              <span>{t('dashboard.config_safety.supervised_collectors')}</span>
              <strong>{supervised.length}</strong>
            </div>
            <div className="config-safety-metric-row">
              <span>{t('dashboard.config_safety.last_7d_pushes')}</span>
              <strong>{activityError || last7dPushes === undefined ? '—' : last7dPushes}</strong>
            </div>
          </div>
          <p className={`config-safety-status-line config-safety-status-line-${tone}`}>
            {statusLine}
          </p>
          <Link className="config-safety-status-link" to="/inventory?control=supervised">
            {t('dashboard.config_safety.cta')}
          </Link>
          {driftDashboardEnabled && (
            <Link className="config-safety-status-link" to="/config-safety/drift">
              {t('dashboard.config_safety.drift_cta')}
            </Link>
          )}
          <ConfigEvidencePanel />
        </>
      )}
    </section>
  )
}
