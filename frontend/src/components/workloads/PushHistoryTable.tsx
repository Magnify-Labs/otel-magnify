import { useState } from 'react'
import axios from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { workloadsAPI } from '../../api/client'
import { useStore } from '../../store'
import { hasPerm } from '../../lib/perm'
import { useFeature } from '../../hooks/useFeature'
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

function knownGoodDisableReason(
  row: WorkloadConfig,
  canMark: boolean,
  t: ReturnType<typeof useTranslation>['t'],
  featureDisabledReason = '',
) {
  if (featureDisabledReason) return featureDisabledReason
  if (!canMark) return t('workloads.config.versioning.requires_push_permission')
  if (row.status !== 'applied') return t('workloads.config.versioning.known_good_only_applied')
  if (!hasContent(row)) return t('workloads.config.versioning.content_unavailable')
  return ''
}

function rollbackDisableReason(
  row: WorkloadConfig,
  canRollback: boolean,
  canReadConfigContent: boolean,
  t: ReturnType<typeof useTranslation>['t'],
  featureDisabledReason = '',
) {
  if (featureDisabledReason) return featureDisabledReason
  if (!canReadConfigContent) return t('workloads.config.permission.content_restricted')
  if (!canRollback) return t('workloads.config.versioning.requires_push_permission')
  if (row.status !== 'applied') {
    return t('workloads.config.versioning.rollback_only_applied')
  }
  if (!hasContent(row)) return t('workloads.config.versioning.content_unavailable')
  return ''
}

export default function PushHistoryTable({ workloadId }: Props) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const me = useStore((s) => s.me)
  const canPushConfig = hasPerm(me?.groups, 'workload:push_config')
  const canReadConfigContent = hasPerm(me?.groups, 'config:read_content')
  const { enabled: guidedRollbackEnabled, isLoading: guidedRollbackLoading } = useFeature(
    'config_safety.guided_rollback',
  )
  const guidedRollbackDisabledReason = guidedRollbackLoading
    ? t('workloads.config.recovery.loading')
    : !guidedRollbackEnabled
      ? t('workloads.config.recovery.feature_disabled')
      : ''
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
    enabled: guidedRollbackEnabled,
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
        : t('workloads.config.versioning.mark_known_good_failed')
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
        : t('workloads.config.versioning.clear_known_good_failed')
      setKnownGoodError(msg)
    },
  })

  if (history.length === 0) return null

  const hasReadableContent = canReadConfigContent && history.some((row) => Boolean(row.content))

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
      row.is_current
        ? { key: 'current', label: t('workloads.config.versioning.state.current') }
        : null,
      row.is_previous
        ? { key: 'previous', label: t('workloads.config.versioning.state.previous') }
        : null,
      row.is_last_known_good || row.config_id === activeKnownGoodHash
        ? { key: 'last-known-good', label: t('workloads.config.versioning.state.last_known_good') }
        : null,
      row.is_failed_candidate
        ? {
            key: 'failed-candidate',
            label: t('workloads.config.versioning.state.failed_candidate'),
          }
        : null,
    ].filter((badge): badge is { key: string; label: string } => badge !== null)

    if (badges.length === 0) return null
    return (
      <div
        className="history-state-badges"
        aria-label={t('workloads.config.versioning.states_for', { hash: shortHash(row.config_id) })}
      >
        {badges.map((badge) => (
          <span key={badge.key} className={`history-state-badge history-state-${badge.key}`}>
            {badge.label}
          </span>
        ))}
      </div>
    )
  }

  return (
    <>
      <div className="btn-row btn-row-top">
        <p className="section-title section-title-flex">
          {t('workloads.config.versioning.history_title')}
        </p>
        {canReadConfigContent && (
          <button
            className="btn btn-small"
            onClick={() => setCompareOpen(true)}
            disabled={history.length < 2 || !hasReadableContent}
            title={
              history.length < 2
                ? t('workloads.config.versioning.compare_needs_two')
                : !hasReadableContent
                  ? t('workloads.config.permission.content_restricted')
                  : t('workloads.config.versioning.compare_button')
            }
          >
            {t('workloads.config.versioning.compare_button')}
          </button>
        )}
      </div>
      {knownGoodError && <div className="error-text error-text-push">{knownGoodError}</div>}
      {knownGoodQueryError && !isNotFoundError(knownGoodQueryError) && (
        <div className="error-text error-text-push">
          {t('workloads.config.versioning.known_good_load_error')}
        </div>
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
            <th aria-label={t('workloads.config.versioning.col_actions')}></th>
          </tr>
        </thead>
        <tbody>
          {history.map((row) => {
            const rowKey = `${row.config_id}-${row.applied_at}`
            const isEditing = editingLabel === rowKey
            const markDisabledReason = knownGoodDisableReason(
              row,
              canPushConfig,
              t,
              guidedRollbackDisabledReason,
            )
            const markDisabled = !!markDisabledReason || markKnownGoodMutation.isPending
            const rollbackDisabledReason = rollbackDisableReason(
              row,
              canPushConfig,
              canReadConfigContent,
              t,
              guidedRollbackDisabledReason,
            )
            const rollbackDisabled = !!rollbackDisabledReason
            const isKnownGood = row.is_last_known_good || row.config_id === activeKnownGoodHash
            return (
              <tr key={rowKey}>
                <td>{new Date(row.applied_at).toLocaleString()}</td>
                <td>
                  <span className={`status-pill status-${row.status}`}>
                    {t(`workloads.config.versioning.status.${row.status}`)}
                  </span>
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
                  {canReadConfigContent && row.content && (
                    <button className="btn btn-small" onClick={() => setViewing(row)}>
                      {t('workloads.config.versioning.view_button')}
                    </button>
                  )}
                  {canReadConfigContent && row.content && (
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
                  )}
                  {isKnownGood ? (
                    <button
                      className="btn btn-small"
                      onClick={() => clearKnownGoodMutation.mutate()}
                      disabled={
                        !!guidedRollbackDisabledReason ||
                        !canPushConfig ||
                        clearKnownGoodMutation.isPending
                      }
                      title={
                        guidedRollbackDisabledReason ||
                        (canPushConfig
                          ? t('workloads.config.versioning.clear_known_good')
                          : t('workloads.config.versioning.requires_push_permission'))
                      }
                    >
                      {t('workloads.config.versioning.clear_known_good')}
                    </button>
                  ) : (
                    <button
                      className="btn btn-small"
                      onClick={() => setConfirmKnownGood(row)}
                      disabled={markDisabled}
                      title={markDisabledReason}
                    >
                      {t('workloads.config.versioning.mark_known_good')}
                    </button>
                  )}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>

      {canReadConfigContent && viewing && (
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

      {canReadConfigContent && rollbackTarget && (
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
            aria-label={t('workloads.config.versioning.mark_known_good_confirm_title')}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="modal-header">
              <span>{t('workloads.config.versioning.mark_known_good_confirm_title')}</span>
            </div>
            <div className="modal-body">
              <p>{t('workloads.config.versioning.mark_known_good_confirm_body')}</p>
              {activeKnownGoodHash && activeKnownGoodHash !== confirmKnownGood.config_id && (
                <p className="config-confirm-detail">
                  {t('workloads.config.versioning.mark_known_good_replaces', {
                    hash: shortHash(activeKnownGoodHash),
                  })}
                </p>
              )}
            </div>
            <div className="btn-row modal-actions">
              <button
                className="btn btn-primary"
                onClick={() => markKnownGoodMutation.mutate(confirmKnownGood)}
                disabled={markKnownGoodMutation.isPending}
              >
                {markKnownGoodMutation.isPending
                  ? t('workloads.config.versioning.marking_known_good')
                  : t('workloads.config.versioning.mark_as_last_known_good')}
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

      {canReadConfigContent && compareOpen && (
        <ConfigCompareDialog
          workloadId={workloadId}
          history={history}
          onClose={() => setCompareOpen(false)}
        />
      )}
    </>
  )
}
