import { useQuery } from '@tanstack/react-query'
import { useLocation } from 'react-router-dom'
import { workloadsAPI } from '../../api/client'
import { routeAwarePollInterval } from '../../lib/queryPolling'
import type { WorkloadEvent } from '../../types'

interface Props {
  workloadId: string
}

function groupByDay(events: WorkloadEvent[]): Record<string, WorkloadEvent[]> {
  return events.reduce<Record<string, WorkloadEvent[]>>((acc, e) => {
    const day = new Date(e.occurred_at).toISOString().slice(0, 10)
    if (!acc[day]) acc[day] = []
    acc[day].push(e)
    return acc
  }, {})
}

export default function ActivityTab({ workloadId }: Props) {
  const location = useLocation()
  const activeRoute = `/workloads/${workloadId}`
  const { data: events } = useQuery({
    queryKey: ['workload-events', workloadId],
    queryFn: () => workloadsAPI.events(workloadId, { limit: 100 }),
    refetchInterval: () => routeAwarePollInterval(activeRoute, location.pathname, 10_000),
  })
  const { data: stats } = useQuery({
    queryKey: ['workload-events-stats', workloadId],
    queryFn: () => workloadsAPI.eventsStats(workloadId, '24h'),
    refetchInterval: () => routeAwarePollInterval(activeRoute, location.pathname, 30_000),
  })

  const grouped = groupByDay(events ?? [])
  const days = Object.keys(grouped).sort().reverse()

  return (
    <div className="activity-panel">
      <header className="activity-header">
        <strong>{stats?.disconnected ?? 0}</strong> disconnect
        {stats?.disconnected === 1 ? '' : 's'} in the last 24h
        {stats && (
          <span className="activity-header-meta">({stats.churn_rate_per_hour.toFixed(2)}/h)</span>
        )}
      </header>

      {days.length === 0 ? (
        <div className="empty-state">No activity recorded</div>
      ) : (
        days.map((day) => (
          <section key={day} className="activity-day">
            <h4 className="activity-day-heading">{day}</h4>
            <ul className="activity-timeline">
              {grouped[day].map((e) => (
                <li key={e.id} className={`activity-entry activity-${e.event_type}`}>
                  <time className="activity-time">
                    {new Date(e.occurred_at).toLocaleTimeString()}
                  </time>
                  <span className="activity-dot" aria-hidden="true" />
                  <span className="activity-event-type">{e.event_type.replace('_', ' ')}</span>
                  <span className="mono activity-identity">
                    {e.pod_name || e.instance_uid.slice(0, 8)}
                  </span>
                  {e.event_type === 'version_changed' && (
                    <span className="activity-version">
                      {e.prev_version} → {e.version}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </section>
        ))
      )}
    </div>
  )
}
