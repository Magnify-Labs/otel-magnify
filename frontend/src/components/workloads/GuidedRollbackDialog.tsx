import { useMemo, useState } from 'react'
import axios from 'axios'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { workloadsAPI } from '../../api/client'
import ConfigDiffView from './ConfigDiffView'
import type {
  RollbackPrepareResponse,
  RollbackStatusReport,
  RollbackValidationFinding,
  UnavailableComponentWarning,
  WorkloadConfig,
} from '../../types'

interface Props {
  workloadId: string
  target: WorkloadConfig
  onClose: () => void
}

function shortHash(hash?: string) {
  return hash ? hash.slice(0, 12) : '—'
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

function errorMessage(err: unknown, fallback: string) {
  if (axios.isAxiosError(err)) {
    const data = err.response?.data as { error?: string; message?: string } | undefined
    return data?.error ?? data?.message ?? err.message
  }
  return fallback
}

function validationLabel(prepare: RollbackPrepareResponse) {
  if (!prepare.validation.valid) return 'Validation failed'
  if (prepare.validation.status === 'valid_with_warnings' || prepare.action.warnings.length > 0) {
    return 'Validation passed with warnings'
  }
  return 'Validation passed'
}

function reportLabel(report: RollbackStatusReport) {
  if (report.apply_status === 'applied') return 'Rollback applied'
  if (report.apply_status === 'applying') return 'Applying rollback'
  if (report.apply_status === 'accepted') return 'Rollback accepted'
  if (report.apply_status === 'failed') return 'Rollback failed'
  if (report.timed_out || report.apply_status === 'unknown') return 'Rollback status unknown'
  return 'Rollback report'
}

function durationLabel(ms?: number) {
  if (ms === undefined) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(ms % 1000 === 0 ? 0 : 1)}s`
}

function FindingList({
  title,
  findings,
}: {
  title: string
  findings: RollbackValidationFinding[]
}) {
  if (findings.length === 0) return null
  return (
    <section className="rollback-panel" aria-label={title}>
      <h3>{title}</h3>
      <ul className="rollback-finding-list">
        {findings.map((finding, index) => (
          <li key={`${finding.code}-${index}`} className={`rollback-finding-${finding.severity}`}>
            <strong>{finding.code}</strong>
            {finding.path ? <code>{finding.path}</code> : null}
            <span>{finding.message}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function UnavailableComponents({ warnings }: { warnings: UnavailableComponentWarning[] }) {
  if (warnings.length === 0) return null
  return (
    <section className="rollback-panel rollback-warning-panel" aria-label="Unavailable components">
      <h3>Unavailable components</h3>
      <ul className="rollback-finding-list">
        {warnings.map((warning) => (
          <li
            key={`${warning.category}-${warning.component_id}`}
            className="rollback-finding-error"
          >
            <strong>{warning.component_id}</strong>
            <span>
              {warning.component_type} is not available in {warning.category}
              {warning.available?.length ? ` (available: ${warning.available.join(', ')})` : ''}
            </span>
            {warning.path ? <code>{warning.path}</code> : null}
          </li>
        ))}
      </ul>
    </section>
  )
}

export default function GuidedRollbackDialog({ workloadId, target, onClose }: Props) {
  const queryClient = useQueryClient()
  const [confirmed, setConfirmed] = useState(false)
  const [statusReport, setStatusReport] = useState<RollbackStatusReport | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)

  const prepareQuery = useQuery({
    queryKey: ['guided-rollback-prepare', workloadId, target.config_id],
    queryFn: () => workloadsAPI.prepareRollback(workloadId, target.config_id),
  })

  const submitMutation = useMutation({
    mutationFn: () => workloadsAPI.rollbackConfig(workloadId, target.config_id),
    onSuccess: async (response) => {
      queryClient.invalidateQueries({ queryKey: ['workload-config-history', workloadId] })
      queryClient.invalidateQueries({ queryKey: ['workload', workloadId] })
      if (!response.request_id) {
        setStatusReport({
          schema_version: 'guided-rollback-status.v1',
          request_id: 'legacy-response',
          workload_id: workloadId,
          target_hash: response.config_hash,
          request_status: 'accepted',
          apply_status: response.status === 'accepted' ? 'accepted' : 'unknown',
          terminal: false,
          started_at: new Date().toISOString(),
          elapsed_ms: 0,
          timeout_seconds:
            response.timeout_seconds ?? prepareQuery.data?.status_context.timeout_seconds ?? 30,
          timed_out: false,
        })
        return
      }
      try {
        const report = await workloadsAPI.getRollbackStatus(workloadId, response.request_id)
        setStatusReport(report)
      } catch (err) {
        setStatusError(errorMessage(err, 'Failed to load rollback status'))
        setStatusReport({
          schema_version: 'guided-rollback-status.v1',
          request_id: response.request_id,
          workload_id: workloadId,
          target_hash: response.target_hash ?? response.config_hash,
          target_label: prepareQuery.data?.target_config.metadata.label,
          request_status: 'accepted',
          apply_status: 'accepted',
          terminal: false,
          started_at: new Date().toISOString(),
          elapsed_ms: 0,
          timeout_seconds:
            response.timeout_seconds ?? prepareQuery.data?.status_context.timeout_seconds ?? 30,
          timed_out: false,
        })
      }
    },
  })

  const prepareError = prepareQuery.isError
    ? `Failed to load rollback preview: ${errorMessage(prepareQuery.error, 'Rollback preview failed')}`
    : null

  const diffInputs = prepareQuery.data?.diff.inputs
  const canRenderMergeDiff =
    prepareQuery.data?.diff.status === 'available' &&
    diffInputs?.current_content_available &&
    diffInputs?.target_content_available &&
    !!diffInputs.current_yaml &&
    !!diffInputs.target_yaml

  const canSubmit = useMemo(() => {
    const prepare = prepareQuery.data
    if (!prepare) return false
    return (
      prepare.action.can_submit &&
      prepare.validation.can_confirm &&
      confirmed &&
      !submitMutation.isPending
    )
  }, [confirmed, prepareQuery.data, submitMutation.isPending])

  const prepare = prepareQuery.data
  const targetLabel = prepare?.target_config.metadata.label ?? target.label ?? 'Unlabeled target'
  const submitLabel = prepare?.action.confirmation_label ?? 'Confirm rollback'

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal rollback-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="guided-rollback-title"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-header">
          <div>
            <span id="guided-rollback-title">Guided rollback</span>
            <p className="rollback-subtitle">
              Review the exact target before replacing this collector config.
            </p>
          </div>
          <button className="btn btn-small" onClick={onClose} disabled={submitMutation.isPending}>
            Close
          </button>
        </div>

        {prepareQuery.isLoading && <div className="loading">Preparing rollback preview...</div>}
        {prepareError && <div className="error-text rollback-alert">{prepareError}</div>}

        {prepare && (
          <>
            <div className="rollback-summary-grid">
              <section className="rollback-panel">
                <h3>Current config</h3>
                <dl>
                  <dt>Hash</dt>
                  <dd>
                    <code>{shortHash(prepare.current_config.hash)}</code>
                  </dd>
                  <dt>Remote config</dt>
                  <dd>
                    {prepare.workload.remote_config_status?.status ?? 'unknown'}{' '}
                    {shortHash(prepare.workload.remote_config_status?.config_hash)}
                  </dd>
                </dl>
              </section>

              <section className="rollback-panel rollback-target-panel">
                <h3>Rollback target</h3>
                <dl>
                  <dt>Label</dt>
                  <dd>{targetLabel}</dd>
                  <dt>Hash</dt>
                  <dd>
                    <code>{shortHash(prepare.target_config.hash)}</code>
                  </dd>
                  <dt>Date</dt>
                  <dd>{formatDate(prepare.target_config.metadata.applied_at)}</dd>
                  <dt>Author</dt>
                  <dd>{prepare.target_config.metadata.pushed_by ?? '—'}</dd>
                  <dt>Known-good</dt>
                  <dd>{prepare.target_ref.known_good ? 'Yes' : 'No'}</dd>
                </dl>
              </section>
            </div>

            <section className="rollback-panel rollback-validation-panel">
              <h3>{validationLabel(prepare)}</h3>
              <p>
                Checked {formatDate(prepare.validation.checked_at)} with{' '}
                {prepare.validation.validator_version}.
              </p>
            </section>

            <FindingList title="Blocking reasons" findings={prepare.action.blocking_reasons} />
            <FindingList title="Warnings" findings={prepare.action.warnings} />
            <UnavailableComponents warnings={prepare.validation.unavailable_components} />

            <section className="rollback-panel" aria-label="Current to target diff">
              <h3>Current-to-target diff</h3>
              {canRenderMergeDiff ? (
                <ConfigDiffView
                  oldYaml={diffInputs.current_yaml!}
                  newYaml={diffInputs.target_yaml!}
                />
              ) : prepare.diff.raw_diff?.text ? (
                <>
                  <div className="rollback-raw-diff-warning">
                    Semantic redacted diff is unavailable. Raw fallback diff may include sensitive
                    values; review only when necessary.
                  </div>
                  <pre className="rollback-raw-diff">{prepare.diff.raw_diff.text}</pre>
                </>
              ) : (
                <div className="empty-state">
                  {prepare.diff.message ?? 'Diff preview is unavailable for this target.'}
                </div>
              )}
            </section>

            {statusReport && (
              <section className="rollback-report" aria-label="Post-rollback report">
                <h3>{reportLabel(statusReport)}</h3>
                <p>Duration: {durationLabel(statusReport.elapsed_ms)}</p>
                <p>
                  Remote config:{' '}
                  {statusReport.remote_config_status
                    ? `${statusReport.remote_config_status.status} ${shortHash(statusReport.remote_config_status.config_hash)}`
                    : 'not reported yet'}
                </p>
                {statusReport.error_message ? (
                  <p className="error-text">{statusReport.error_message}</p>
                ) : null}
                {statusError ? (
                  <p className="error-text">Status refresh failed: {statusError}</p>
                ) : null}
              </section>
            )}

            {submitMutation.isError && (
              <div className="error-text rollback-alert">
                Rollback request failed:{' '}
                {errorMessage(submitMutation.error, 'Failed to submit rollback')}
              </div>
            )}

            <label className="rollback-confirm-check">
              <input
                type="checkbox"
                checked={confirmed}
                onChange={(event) => setConfirmed(event.target.checked)}
                disabled={
                  !prepare.action.can_submit ||
                  !prepare.validation.can_confirm ||
                  submitMutation.isPending
                }
              />
              <span>I understand this will replace the collector remote config</span>
            </label>

            <div className="btn-row rollback-actions">
              <button
                className="btn btn-primary"
                onClick={() => submitMutation.mutate()}
                disabled={!canSubmit}
                title={!prepare.action.can_submit ? 'Resolve blocking reasons before rollback' : ''}
              >
                {submitMutation.isPending ? 'Submitting rollback...' : submitLabel}
              </button>
              <button className="btn" onClick={onClose} disabled={submitMutation.isPending}>
                Cancel
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
