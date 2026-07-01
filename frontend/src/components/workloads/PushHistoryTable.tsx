import { useState } from 'react'
import axios from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { workloadsAPI } from '../../api/client'
import { useStore } from '../../store'
import { hasPerm } from '../../lib/perm'
import YamlEditor from '../config/YamlEditor'
import ConfigCompareDialog from './ConfigCompareDialog'
import GuidedRollbackDialog from './GuidedRollbackDialog'
import type { WorkloadConfig } from '../../types'

interface Props {
  workloadId: string
}

function shortHash(hash: string) {
  return hash.substring(0, 8)
}

function isNotFoundError(err: unknown) {
  return axios.isAxiosError(err) && err.response?.status === 404
}

function hasContent(row: WorkloadConfig) {
  return row.content_available ?? !!row.content
}

function knownGoodDisableReason(row: WorkloadConfig, canMark: boolean) {
  if (!canMark) return 'Requires workload:push_config permission'
  if (row.status !== 'applied') return 'Only applied revisions can be marked known-good'
  if (!hasContent(row)) return 'Config content unavailable'
  return ''
}

function rollbackDisableReason(row: WorkloadConfig, canRollback: boolean) {
  if (!canRollback) return 'Requires workload:push_config permission'
  if (row.status !== 'applied') {
    return 'Only applied revisions can be rolled back. Failed or pending revisions are not safe rollback targets.'
  }
  if (!hasContent(row)) return 'Config content unavailable'
  return ''
}

export default function PushHistoryTable({ workloadId }: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const me = useStore((s) => s.me)
  const canPushConfig = hasPerm(me?.groups, 'workload:push_config')
  const [viewing, setViewing] = useState<WorkloadConfig | null>(null)
  const [rollbackTarget, setRollbackTarget] = useState<WorkloadConfig | null>(null)
  const [confirmKnownGood, setConfirmKnownGood] = useState<WorkloadConfig | null>(null)
  const [compareOpen, setCompareOpen] = useState(false)
  const [editingLabel, setEditingLabel] = useState<string | null>(null)
  const [knownGoodError, setKnownGoodError] = useState<string | null>(null)

  const { data: history = [] } = useQuery({
    queryKey: ['workload-config-history', workloadId],
    queryFn: () => workloadsAPI.getConfigHistory(workloadId),
  })

  const { data: knownGood, error: knownGoodQueryError } = useQuery({
    queryKey: ['workload-known-good', workloadId],
    queryFn: () => workloadsAPI.getKnownGood(workloadId),
    retry: false,
  })

  const activeKnownGoodHash =
    knownGood?.config_id ?? history.find((row) => row.is_last_known_good)?.config_id

  const labelMutation = useMutation({
    mutationFn: ({ hash, label }: { hash: string; label: string }) =>
      workloadsAPI.setConfigLabel(workloadId, hash, label),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workloadId] })
    },
  })

  const markKnownGoodMutation = useMutation({
    mutationFn: (row: WorkloadConfig) =>
      workloadsAPI.markKnownGood(workloadId, row.config_id, {
        ifCurrentKnownGood:
          activeKnownGoodHash && activeKnownGoodHash !== row.config_id
            ? activeKnownGoodHash
            : undefined,
        replaceReason:
          activeKnownGoodHash && activeKnownGoodHash !== row.config_id
            ? `Replaced from UI by marking ${shortHash(row.config_id)}`
            : '',
      }),
    onSuccess: () => {
      setConfirmKnownGood(null)
      setKnownGoodError(null)
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workloadId] })
      queryClient.invalidateQueries({ queryKey: ['workload-known-good', workloadId] })
      queryClient.invalidateQueries({ queryKey: ['workload', workloadId] })
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to mark known-good'
      setKnownGoodError(msg)
    },
  })

  const clearKnownGoodMutation = useMutation({
    mutationFn: () => workloadsAPI.clearKnownGood(workloadId),
    onSuccess: () => {
      setKnownGoodError(null)
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workloadId] })
      queryClient.invalidateQueries({ queryKey: ['workload-known-good', workloadId] })
      queryClient.invalidateQueries({ queryKey: ['workload', workloadId] })
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to clear known-good'
      setKnownGoodError(msg)
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

  function renderStateBadges(row: WorkloadConfig) {
    const badges = [
      row.is_current ? 'Current' : null,
      row.is_previous ? 'Previous' : null,
      row.is_last_known_good || row.config_id === activeKnownGoodHash ? 'Last known-good' : null,
      row.is_failed_candidate ? 'Failed candidate' : null,
    ].filter(Boolean)

    if (badges.length === 0) return null
    return (
      <div className="history-state-badges" aria-label={`States for ${shortHash(row.config_id)}`}>
        {badges.map((badge) => (
          <span
            key={badge}
            className={`history-state-badge history-state-${String(badge).toLowerCase().replaceAll(' ', '-')}`}
          >
            {badge}
          </span>
        ))}
      </div>
    )
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
      {knownGoodError && <div className="error-text error-text-push">{knownGoodError}</div>}
      {knownGoodQueryError && !isNotFoundError(knownGoodQueryError) && (
        <div className="error-text error-text-push">Failed to load Last known-good state.</div>
      )}
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
            const markDisabledReason = knownGoodDisableReason(row, canPushConfig)
            const markDisabled = !!markDisabledReason || markKnownGoodMutation.isPending
            const rollbackDisabledReason = rollbackDisableReason(row, canPushConfig)
            const rollbackDisabled = !!rollbackDisabledReason
            const isKnownGood = row.is_last_known_good || row.config_id === activeKnownGoodHash
            return (
              <tr key={rowKey}>
                <td>{new Date(row.applied_at).toLocaleString()}</td>
                <td>
                  <span className={`status-pill status-${row.status}`}>{row.status}</span>
                  {renderStateBadges(row)}
                </td>
                <td>{row.pushed_by || '—'}</td>
                <td>
                  <code>{shortHash(row.config_id)}</code>
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
                  <button
                    className="btn btn-small"
                    onClick={() => setRollbackTarget(row)}
                    disabled={rollbackDisabled}
                    title={
                      rollbackDisabledReason || t('workloads.config.versioning.rollback_button')
                    }
                  >
                    {t('workloads.config.versioning.rollback_button')}
                  </button>
                  {isKnownGood ? (
                    <button
                      className="btn btn-small"
                      onClick={() => clearKnownGoodMutation.mutate()}
                      disabled={!canPushConfig || clearKnownGoodMutation.isPending}
                      title={
                        canPushConfig
                          ? 'Clear Last known-good'
                          : 'Requires workload:push_config permission'
                      }
                    >
                      Clear known-good
                    </button>
                  ) : (
                    <button
                      className="btn btn-small"
                      onClick={() => setConfirmKnownGood(row)}
                      disabled={markDisabled}
                      title={markDisabledReason}
                    >
                      Mark as known-good
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

      {confirmKnownGood && (
        <div className="modal-backdrop" onClick={() => setConfirmKnownGood(null)}>
          <div
            className="modal"
            role="dialog"
            aria-modal="true"
            aria-label="Mark this revision as Last known-good?"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="modal-header">
              <span>Mark this revision as Last known-good?</span>
            </div>
            <div className="modal-body">
              <p>
                Future rollback defaults will target this config for this workload until another
                revision is marked.
              </p>
              {activeKnownGoodHash && activeKnownGoodHash !== confirmKnownGood.config_id && (
                <p className="config-confirm-detail">
                  This replaces {shortHash(activeKnownGoodHash)} as Last known-good.
                </p>
              )}
            </div>
            <div className="btn-row modal-actions">
              <button
                className="btn btn-primary"
                onClick={() => markKnownGoodMutation.mutate(confirmKnownGood)}
                disabled={markKnownGoodMutation.isPending}
              >
                {markKnownGoodMutation.isPending ? 'Marking…' : 'Mark as Last known-good'}
              </button>
              <button
                className="btn"
                onClick={() => setConfirmKnownGood(null)}
                disabled={markKnownGoodMutation.isPending}
              >
                {t('workloads.config.versioning.cancel_button')}
              </button>
            </div>
          </div>
        </div>
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
