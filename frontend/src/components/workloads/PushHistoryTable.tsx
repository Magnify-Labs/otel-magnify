import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { workloadsAPI } from '../../api/client'
import YamlEditor from '../config/YamlEditor'
import ConfigCompareDialog from './ConfigCompareDialog'
import GuidedRollbackDialog from './GuidedRollbackDialog'
import type { WorkloadConfig } from '../../types'

interface Props {
  workloadId: string
}

export default function PushHistoryTable({ workloadId }: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [viewing, setViewing] = useState<WorkloadConfig | null>(null)
  const [rollbackTarget, setRollbackTarget] = useState<WorkloadConfig | null>(null)
  const [compareOpen, setCompareOpen] = useState(false)
  const [editingLabel, setEditingLabel] = useState<string | null>(null)

  const { data: history = [] } = useQuery({
    queryKey: ['workload-config-history', workloadId],
    queryFn: () => workloadsAPI.getConfigHistory(workloadId),
  })

  const labelMutation = useMutation({
    mutationFn: ({ hash, label }: { hash: string; label: string }) =>
      workloadsAPI.setConfigLabel(workloadId, hash, label),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workloadId] })
    },
  })

  if (history.length === 0) return null

  // Each (config_id, applied_at) pair is unique in history; the same hash
  // can appear multiple times (push then rollback). The label edit applies
  // to *every* row of a given hash, so the inline editor is keyed on hash.
  function commitLabel(hash: string, raw: string) {
    const next = raw.trim()
    const current = history.find((r) => r.config_id === hash)?.label ?? ''
    setEditingLabel(null)
    if (next !== current) {
      labelMutation.mutate({ hash, label: next })
    }
  }

  return (
    <>
      <div className="btn-row btn-row-top">
        <p className="section-title" style={{ flex: 1 }}>
          {t('workloads.config.versioning.history_title')}
        </p>
        <button
          className="btn btn-small"
          onClick={() => setCompareOpen(true)}
          disabled={history.length < 2}
          title={
            history.length < 2
              ? t('workloads.config.versioning.compare_needs_two')
              : t('workloads.config.versioning.compare_button')
          }
        >
          {t('workloads.config.versioning.compare_button')}
        </button>
      </div>
      <table className="history-table">
        <thead>
          <tr>
            <th>{t('workloads.config.versioning.col_time')}</th>
            <th>{t('workloads.config.versioning.col_status')}</th>
            <th>{t('workloads.config.versioning.col_user')}</th>
            <th>{t('workloads.config.versioning.col_hash')}</th>
            <th>{t('workloads.config.versioning.col_label')}</th>
            <th>{t('workloads.config.versioning.col_error')}</th>
            <th aria-label="actions"></th>
          </tr>
        </thead>
        <tbody>
          {history.map((row) => {
            const rowKey = `${row.config_id}-${row.applied_at}`
            const isEditing = editingLabel === rowKey
            return (
              <tr key={rowKey}>
                <td>{new Date(row.applied_at).toLocaleString()}</td>
                <td>
                  <span className={`status-pill status-${row.status}`}>{row.status}</span>
                </td>
                <td>{row.pushed_by || '—'}</td>
                <td>
                  <code>{row.config_id.substring(0, 8)}</code>
                </td>
                <td
                  className="history-label"
                  onDoubleClick={() => setEditingLabel(rowKey)}
                  title={isEditing ? '' : t('workloads.config.versioning.label_double_click_hint')}
                >
                  {isEditing ? (
                    <input
                      autoFocus
                      defaultValue={row.label ?? ''}
                      maxLength={128}
                      aria-label={t('workloads.config.versioning.label_input_aria')}
                      onBlur={(e) => commitLabel(row.config_id, e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          commitLabel(row.config_id, (e.target as HTMLInputElement).value)
                        } else if (e.key === 'Escape') {
                          setEditingLabel(null)
                        }
                      }}
                    />
                  ) : row.label ? (
                    <span className="history-label-value">{row.label}</span>
                  ) : (
                    <span className="history-label-empty">—</span>
                  )}
                </td>
                <td className="history-error">{row.error_message || ''}</td>
                <td>
                  <button className="btn btn-small" onClick={() => setViewing(row)}>
                    {t('workloads.config.versioning.view_button')}
                  </button>
                  {row.content && (
                    <button className="btn btn-small" onClick={() => setRollbackTarget(row)}>
                      {t('workloads.config.versioning.rollback_button')}
                    </button>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>

      {viewing && (
        <div className="modal-backdrop" onClick={() => setViewing(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <span>
                {t('workloads.config.versioning.view_title', {
                  hash: viewing.config_id.substring(0, 12),
                })}
              </span>
              <button className="btn btn-small" onClick={() => setViewing(null)}>
                {t('workloads.config.versioning.close_button')}
              </button>
            </div>
            <YamlEditor value={viewing.content ?? ''} readOnly />
          </div>
        </div>
      )}

      {rollbackTarget && (
        <GuidedRollbackDialog
          workloadId={workloadId}
          target={rollbackTarget}
          onClose={() => setRollbackTarget(null)}
        />
      )}

      {compareOpen && (
        <ConfigCompareDialog
          workloadId={workloadId}
          history={history}
          onClose={() => setCompareOpen(false)}
        />
      )}
    </>
  )
}
