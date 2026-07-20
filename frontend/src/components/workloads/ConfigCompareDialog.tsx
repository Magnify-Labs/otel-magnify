import { useMemo, useState } from 'react'
import axios from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { configsAPI, workloadsAPI } from '../../api/client'
import ConfigDiffView from './ConfigDiffView'
import { useCapability } from '../../hooks/useCapability'
import type { WorkloadConfig } from '../../types'

interface Props {
  workloadId: string
  history: WorkloadConfig[]
  onClose: () => void
}

interface RevisionEntry {
  hash: string
  appliedAt: string
  label?: string
}

// dedupeRevisions collapses the history (which can contain repeated hashes
// for rollback rows) into one entry per (hash, applied_at) pair, sorted
// newest-first like the table view. Two select boxes both choose from this
// flat list.
function dedupeRevisions(history: WorkloadConfig[]): RevisionEntry[] {
  return history.map((row) => ({
    hash: row.config_id,
    appliedAt: row.applied_at,
    label: row.label,
  }))
}

function isForbiddenError(err: unknown) {
  return axios.isAxiosError(err) && err.response?.status === 403
}

export default function ConfigCompareDialog({ workloadId, history, onClose }: Props) {
  const { t } = useTranslation()
  const { enabled: policyPreviewEnabled, isLoading: policyPreviewLoading } = useCapability(
    'config_safety.policy_preview',
  )
  const revisions = useMemo(() => dedupeRevisions(history), [history])

  // Default to comparing the two most recent revisions when available.
  const [leftKey, setLeftKey] = useState<string>(() =>
    revisions[1] ? revisionKey(revisions[1]) : '',
  )
  const [rightKey, setRightKey] = useState<string>(() =>
    revisions[0] ? revisionKey(revisions[0]) : '',
  )

  const leftRev = revisions.find((r) => revisionKey(r) === leftKey)
  const rightRev = revisions.find((r) => revisionKey(r) === rightKey)

  const leftQuery = useQuery({
    queryKey: ['workload-config-by-hash', workloadId, leftRev?.hash],
    queryFn: () => workloadsAPI.getConfigByHash(workloadId, leftRev!.hash),
    enabled: !!leftRev,
  })
  const rightQuery = useQuery({
    queryKey: ['workload-config-by-hash', workloadId, rightRev?.hash],
    queryFn: () => workloadsAPI.getConfigByHash(workloadId, rightRev!.hash),
    enabled: !!rightRev,
  })

  const leftYaml = leftQuery.data?.content ?? ''
  const rightYaml = rightQuery.data?.content ?? ''
  const isLoading = leftQuery.isLoading || rightQuery.isLoading
  const isError = leftQuery.isError || rightQuery.isError
  const isContentRestricted =
    isForbiddenError(leftQuery.error) || isForbiddenError(rightQuery.error)
  const canCompare = !isLoading && !isError && leftYaml.length > 0 && rightYaml.length > 0

  const otelDiffQuery = useQuery({
    queryKey: ['otel-config-diff', workloadId, leftRev?.hash, rightRev?.hash],
    queryFn: () =>
      configsAPI.diff({
        base_yaml: leftYaml,
        target_yaml: rightYaml,
        context: {
          workload_id: workloadId,
          base_label: leftRev ? formatRevision(leftRev) : undefined,
          target_label: rightRev ? formatRevision(rightRev) : undefined,
          include_raw_paths: true,
        },
      }),
    enabled: canCompare,
    retry: false,
  })

  const policyQuery = useQuery({
    queryKey: ['config-policy-preview', workloadId, leftRev?.hash, rightRev?.hash],
    queryFn: () =>
      configsAPI.previewPolicy({
        current_yaml: leftYaml,
        candidate_yaml: rightYaml,
        target: { workload_id: workloadId },
      }),
    enabled: canCompare && policyPreviewEnabled,
    retry: false,
  })

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal modal-wide"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label={t('workloads.config.versioning.compare_dialog_title')}
      >
        <div className="modal-header">
          <span>{t('workloads.config.versioning.compare_dialog_title')}</span>
          <button className="btn btn-small" onClick={onClose}>
            {t('workloads.config.versioning.close_button')}
          </button>
        </div>

        <div className="btn-row" style={{ padding: '0.5rem 1rem' }}>
          <label style={{ flex: 1 }}>
            <span className="filter-label">
              {t('workloads.config.versioning.compare_left_label')}
            </span>
            <select
              className="filter-select"
              value={leftKey}
              onChange={(e) => setLeftKey(e.target.value)}
              aria-label={t('workloads.config.versioning.compare_left_label')}
            >
              {revisions.map((r) => (
                <option key={revisionKey(r)} value={revisionKey(r)}>
                  {formatRevision(r)}
                </option>
              ))}
            </select>
          </label>
          <label style={{ flex: 1 }}>
            <span className="filter-label">
              {t('workloads.config.versioning.compare_right_label')}
            </span>
            <select
              className="filter-select"
              value={rightKey}
              onChange={(e) => setRightKey(e.target.value)}
              aria-label={t('workloads.config.versioning.compare_right_label')}
            >
              {revisions.map((r) => (
                <option key={revisionKey(r)} value={revisionKey(r)}>
                  {formatRevision(r)}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div style={{ padding: '0 1rem 1rem' }}>
          {isLoading ? (
            <div className="loading">{t('workloads.config.versioning.compare_loading')}</div>
          ) : isContentRestricted ? (
            <div className="empty-state">{t('workloads.config.permission.content_restricted')}</div>
          ) : isError ? (
            <div className="error-text">{t('workloads.config.versioning.compare_error')}</div>
          ) : (
            <ConfigDiffView
              oldYaml={leftYaml}
              newYaml={rightYaml}
              otelDiff={otelDiffQuery.data}
              otelDiffLoading={otelDiffQuery.isLoading}
              otelDiffUnavailable={otelDiffQuery.isError}
              policy={policyPreviewEnabled ? policyQuery.data : undefined}
              policyLoading={
                policyPreviewLoading || (policyPreviewEnabled && policyQuery.isLoading)
              }
              policyUnavailable={
                !policyPreviewLoading && (!policyPreviewEnabled || policyQuery.isError)
              }
            />
          )}
        </div>
      </div>
    </div>
  )
}

function revisionKey(r: RevisionEntry): string {
  return `${r.hash}@${r.appliedAt}`
}

function formatRevision(r: RevisionEntry): string {
  const ts = new Date(r.appliedAt).toLocaleString()
  const short = r.hash.substring(0, 8)
  return r.label ? `${r.label} · ${short} · ${ts}` : `${short} · ${ts}`
}
