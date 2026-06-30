import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { workloadsAPI, alertsAPI } from '../api/client'
import { useStore } from '../store'
import { isSupervised } from '../lib/workloadCapabilities'
import StatCard from '../components/dashboard/StatCard'
import PushActivityPanel from '../components/dashboard/PushActivityPanel'
import RecentAlertsPanel from '../components/dashboard/RecentAlertsPanel'
import FleetHealthPanel from '../components/dashboard/FleetHealthPanel'
import DeployedVersionsPanel from '../components/dashboard/DeployedVersionsPanel'
import ConfigSafetyStatusPanel from '../components/dashboard/ConfigSafetyStatusPanel'
import '../styles/dashboard.css'

export default function Dashboard() {
  const { t } = useTranslation()
  const workloadsQuery = useQuery({
    queryKey: ['workloads'],
    queryFn: () => workloadsAPI.list(),
  })
  const { data: alerts } = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })

  const setWorkloads = useStore((s) => s.setWorkloads)
  const setAlerts = useStore((s) => s.setAlerts)

  useEffect(() => {
    if (workloadsQuery.data) setWorkloads(workloadsQuery.data)
  }, [workloadsQuery.data, setWorkloads])
  useEffect(() => {
    if (alerts) setAlerts(alerts)
  }, [alerts, setAlerts])

  const ws = workloadsQuery.data ?? []
  const connected = ws.filter((w) => w.status === 'connected').length
  const degraded = ws.filter((w) => w.status === 'degraded').length
  const collectors = ws.filter((w) => w.type === 'collector').length
  const sdks = ws.filter((w) => w.type === 'sdk').length
  const supervised = ws.filter(isSupervised).length

  return (
    <div>
      <header className="page-header">
        <div>
          <h1 className="page-title">{t('dashboard.title')}</h1>
          <p className="page-subtitle">{t('dashboard.subtitle')}</p>
        </div>
      </header>

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
        </aside>
      </section>
    </div>
  )
}
