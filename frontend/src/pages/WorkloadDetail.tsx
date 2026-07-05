import { useState } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { workloadsAPI } from '../api/client'
import StatusBadge from '../components/workloads/StatusBadge'
import WorkloadConfigSection from '../components/workloads/WorkloadConfigSection'
import InstancesTab from '../components/workloads/InstancesTab'
import ActivityTab from '../components/workloads/ActivityTab'
import { isSupervised } from '../lib/workloadCapabilities'
import { hasPerm } from '../lib/perm'
import { useStore } from '../store'

type Tab = 'overview' | 'instances' | 'activity'

export default function WorkloadDetail() {
  const { t } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const me = useStore((s) => s.me)
  const { data: workload, isLoading } = useQuery({
    queryKey: ['workload', id],
    queryFn: () => workloadsAPI.get(id!),
    enabled: !!id,
  })
  const archiveMutation = useMutation({
    mutationFn: (workloadId: string) => workloadsAPI.archive(workloadId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ['workloads'] })
      navigate('/inventory')
    },
  })
  const [tab, setTab] = useState<Tab>('overview')

  if (isLoading) return <div className="loading">Loading workload...</div>
  if (!workload) return <div className="error-text">Workload not found.</div>

  const currentWorkload = workload
  const supervised = isSupervised(workload)
  const isCollector = workload.type === 'collector'
  const canArchive = hasPerm(me?.groups, 'workload:archive')
  const showArchiveAction =
    canArchive && !workload.archived_at && workload.status === 'disconnected'

  function archiveWorkload() {
    if (
      !window.confirm(
        t('workloads.archive.confirm', {
          name: currentWorkload.display_name || currentWorkload.id,
        }),
      )
    ) {
      return
    }
    archiveMutation.mutate(currentWorkload.id)
  }

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">{workload.display_name || workload.id}</h1>
        {showArchiveAction && (
          <button
            type="button"
            className="btn btn-danger btn-small"
            onClick={archiveWorkload}
            disabled={archiveMutation.isPending}
          >
            {archiveMutation.isPending
              ? t('workloads.archive.in_flight')
              : t('workloads.archive.button')}
          </button>
        )}
        <Link to="/inventory" className="page-header-backlink">
          ← Inventory
        </Link>
      </div>

      {workload.archived_at && (
        <div className="notice notice-muted">
          {t('workloads.archive.notice', {
            date: new Date(workload.archived_at).toLocaleString(),
          })}
        </div>
      )}
      {archiveMutation.isError && <div className="error-text">{t('workloads.archive.error')}</div>}

      <nav className="workload-tab-bar">
        <button
          type="button"
          className={tab === 'overview' ? 'workload-tab workload-tab-active' : 'workload-tab'}
          onClick={() => setTab('overview')}
        >
          Overview
        </button>
        <button
          type="button"
          className={tab === 'instances' ? 'workload-tab workload-tab-active' : 'workload-tab'}
          onClick={() => setTab('instances')}
        >
          Instances
        </button>
        <button
          type="button"
          className={tab === 'activity' ? 'workload-tab workload-tab-active' : 'workload-tab'}
          onClick={() => setTab('activity')}
        >
          Activity
        </button>
      </nav>

      {tab === 'overview' && (
        <>
          {/* Key metrics grid */}
          <div className="detail-grid">
            <div className="detail-cell">
              <div className="detail-cell-label">Type</div>
              <div className="detail-cell-value">{workload.type}</div>
            </div>
            <div className="detail-cell">
              <div className="detail-cell-label">Version</div>
              <div className="detail-cell-value">v{workload.version}</div>
            </div>
            <div className="detail-cell">
              <div className="detail-cell-label">Status</div>
              <div className="detail-cell-value">
                <StatusBadge status={workload.status} />
              </div>
            </div>
            {isCollector && (
              <div className="detail-cell">
                <div className="detail-cell-label">Control</div>
                <div
                  className={`detail-cell-value ${supervised ? 'control-supervised' : 'control-readonly'}`}
                >
                  {supervised ? 'Supervised' : 'Read-only'}
                  <span className="detail-cell-sub">
                    {supervised ? 'OpAMP Supervisor' : 'opamp extension only'}
                  </span>
                </div>
              </div>
            )}
            <div className="detail-cell">
              <div className="detail-cell-label">Last seen</div>
              <div className="detail-cell-value detail-cell-timestamp">
                {new Date(workload.last_seen_at).toLocaleString()}
              </div>
            </div>
            {workload.active_config_id && (
              <div className="detail-cell">
                <div className="detail-cell-label">Active config</div>
                <div className="detail-cell-value">
                  <code>{workload.active_config_id.substring(0, 12)}...</code>
                </div>
              </div>
            )}
          </div>

          {/* Labels */}
          {Object.keys(workload.labels).length > 0 && (
            <>
              <p className="section-title">Labels</p>
              <div className="label-chip-list">
                {Object.entries(workload.labels).map(([k, v]) => (
                  <span key={k} className="label-chip">
                    <span className="label-chip-key">{k}</span>
                    <span className="label-chip-eq">=</span>
                    <span className="label-chip-val">{v}</span>
                  </span>
                ))}
              </div>
            </>
          )}

          {/* Configuration */}
          <WorkloadConfigSection workload={workload} />
        </>
      )}

      {tab === 'instances' && (
        <InstancesTab workloadId={workload.id} activeConfigHash={workload.active_config_hash} />
      )}

      {tab === 'activity' && <ActivityTab workloadId={workload.id} />}
    </div>
  )
}
