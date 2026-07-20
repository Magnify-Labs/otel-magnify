import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { workloadsAPI, alertsAPI } from '../api/client'
import { isSupervised } from '../lib/workloadCapabilities'
import { useCapability } from '../hooks/useCapability'
import StatCard from '../components/dashboard/StatCard'
import PushActivityPanel from '../components/dashboard/PushActivityPanel'
import RecentAlertsPanel from '../components/dashboard/RecentAlertsPanel'
import FleetHealthPanel from '../components/dashboard/FleetHealthPanel'
import DeployedVersionsPanel from '../components/dashboard/DeployedVersionsPanel'
import ConfigSafetyStatusPanel from '../components/dashboard/ConfigSafetyStatusPanel'
import FleetVersionIntelligencePanel from '../components/dashboard/FleetVersionIntelligencePanel'
import '../styles/dashboard.css'

const DEFAULT_RECOMMENDED_COLLECTOR_VERSION = '0.100.0'

export default function Dashboard() {
  const { t } = useTranslation()
  const { enabled: versionIntelligenceEnabled } = useCapability(
    'config_safety.version_intelligence',
  )
  const workloadsQuery = useQuery({
    queryKey: ['workloads'],
    queryFn: () => workloadsAPI.list(),
  })
  const alertsQuery = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })
  const { data: alerts } = alertsQuery
  const {
    data: versionIntelligence,
    isLoading: isVersionIntelligenceLoading,
    isError: isVersionIntelligenceError,
  } = useQuery({
    queryKey: ['workloads', 'version-intelligence', DEFAULT_RECOMMENDED_COLLECTOR_VERSION],
    queryFn: () => workloadsAPI.versionIntelligence(DEFAULT_RECOMMENDED_COLLECTOR_VERSION),
    enabled: versionIntelligenceEnabled,
  })

  const isDashboardLoading = workloadsQuery.isLoading || alertsQuery.isLoading
  const isDashboardError = workloadsQuery.isError || alertsQuery.isError

  if (isDashboardLoading) {
    return (
      <DashboardFrame>
        <div className="loading dashboard-state" role="status" aria-live="polite">
          {t('dashboard.state.loading')}
        </div>
      </DashboardFrame>
    )
  }

  if (isDashboardError) {
    return (
      <DashboardFrame>
        <div className="error-text dashboard-state dashboard-error" role="alert">
          <span>{t('dashboard.state.error')}</span>
          <button
            type="button"
            className="dashboard-retry"
            onClick={() => {
              void workloadsQuery.refetch()
              void alertsQuery.refetch()
            }}
          >
            {t('common.retry')}
          </button>
        </div>
      </DashboardFrame>
    )
  }

  const ws = workloadsQuery.data ?? []
  const connected = ws.filter((w) => w.status === 'connected').length
  const degraded = ws.filter((w) => w.status === 'degraded').length
  const collectors = ws.filter((w) => w.type === 'collector').length
  const sdks = ws.filter((w) => w.type === 'sdk').length
  const supervised = ws.filter(isSupervised).length

  return (
    <DashboardFrame>
      <section className="stat-grid">
        <StatCard
          label={t('dashboard.stat.collectors')}
          value={collectors}
          link="/inventory?type=collector"
        />
        <StatCard label={t('dashboard.stat.sdks')} value={sdks} link="/inventory?type=sdk" />
        <StatCard
          label={t('dashboard.stat.supervised')}
          value={supervised}
          link="/inventory?control=supervised"
        />
        <StatCard label={t('dashboard.stat.connected')} value={connected} />
        <StatCard label={t('dashboard.stat.degraded')} value={degraded} />
        <StatCard
          label={t('dashboard.stat.active_alerts')}
          value={alerts?.length ?? 0}
          link="/alerts"
        />
      </section>

      <section className="dashboard-grid">
        <div className="dashboard-col">
          <PushActivityPanel />
          <ConfigSafetyStatusPanel
            workloads={ws}
            isLoading={workloadsQuery.isLoading}
            isError={workloadsQuery.isError}
          />
          <RecentAlertsPanel alerts={alerts ?? []} />
        </div>
        <aside className="dashboard-col">
          <FleetHealthPanel workloads={ws} />
          <DeployedVersionsPanel workloads={ws} />
          {versionIntelligenceEnabled && (
            <FleetVersionIntelligencePanel
              intelligence={versionIntelligence}
              isLoading={isVersionIntelligenceLoading}
              isError={isVersionIntelligenceError}
            />
          )}
        </aside>
      </section>
    </DashboardFrame>
  )
}

function DashboardFrame({ children }: { children: ReactNode }) {
  const { t } = useTranslation()

  return (
    <div>
      <header className="page-header">
        <div>
          <h1 className="page-title">{t('dashboard.title')}</h1>
          <p className="page-subtitle">{t('dashboard.subtitle')}</p>
        </div>
      </header>

      {children}
    </div>
  )
}
