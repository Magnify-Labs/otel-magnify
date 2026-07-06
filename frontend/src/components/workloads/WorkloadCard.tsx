import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { Workload } from '../../types'
import StatusBadge from './StatusBadge'
import LabelChips from './LabelChips'
import { isSupervised } from '../../lib/workloadCapabilities'
import { useStore } from '../../store'

function TypeIcon({ type }: { type: 'collector' | 'sdk' }) {
  if (type === 'collector') {
    return (
      <svg
        width="18"
        height="18"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.3"
        strokeLinecap="round"
        strokeLinejoin="round"
        style={{ color: 'var(--accent)' }}
      >
        <path d="M2 4h12M2 8h12M2 12h12" />
        <circle cx="5" cy="4" r="1" fill="currentColor" stroke="none" />
        <circle cx="8" cy="8" r="1" fill="currentColor" stroke="none" />
        <circle cx="11" cy="12" r="1" fill="currentColor" stroke="none" />
      </svg>
    )
  }
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinecap="round"
      strokeLinejoin="round"
      style={{ color: 'var(--blue)' }}
    >
      <rect x="3" y="2" width="10" height="12" rx="2" />
      <path d="M6 6h4M6 9h2" />
    </svg>
  )
}

export default function WorkloadCard({ workload }: { workload: Workload }) {
  const { t } = useTranslation()
  const connectedCount = useStore((s) => s.connectedInstanceCounts[workload.id])

  return (
    <Link to={`/workloads/${workload.id}`} className="workload-card">
      <div className="workload-card-main">
        <TypeIcon type={workload.type} />
        <div className="workload-card-identity">
          <div className="workload-card-name-row">
            <span className="workload-name">{workload.display_name || workload.id}</span>
            <span className={`agent-type-${workload.type}`}>{workload.type}</span>
            {isSupervised(workload) && (
              <span
                className="agent-supervised-pill"
                title="Managed by an OpAMP Supervisor — accepts remote config pushes"
              >
                {t('inventory.card.supervised')}
              </span>
            )}
            {workload.archived_at && (
              <span className="agent-archived-pill" title={t('inventory.card.archived_title')}>
                {t('inventory.card.archived')}
              </span>
            )}
          </div>
          <div className="workload-card-meta">
            <span>v{workload.version || '—'}</span>
            <span className="workload-card-meta-sep">·</span>
            <span>{formatLastSeen(workload.last_seen_at)}</span>
            {typeof connectedCount === 'number' && (
              <>
                <span className="workload-card-meta-sep">·</span>
                <span className="instance-count-badge">
                  {connectedCount} {connectedCount === 1 ? 'instance' : 'instances'}
                </span>
              </>
            )}
          </div>
        </div>
      </div>
      <div className="workload-card-side">
        <LabelChips labels={workload.labels} />
        <StatusBadge status={workload.status} />
      </div>
    </Link>
  )
}

function formatLastSeen(iso: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  const delta = Date.now() - d.getTime()
  const mins = Math.floor(delta / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}
