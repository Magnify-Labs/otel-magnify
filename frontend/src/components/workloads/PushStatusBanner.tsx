import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type {
  RemoteConfigStatus,
  AutoRollbackEvent,
  PushStatus,
  WorkloadConfig,
  WorkloadConfigErrorGroup,
  WorkloadConfigInstanceStatus,
  WorkloadConfigTimelineEntry,
} from '../../types'
import { safeRemoteErrorText, safeRollbackReasonText } from '../../lib/safeRemoteErrorText'

interface Props {
  status?: RemoteConfigStatus
  push?: WorkloadConfig
  rollback?: AutoRollbackEvent
  onDismissRollback?: () => void
}

const CANONICAL_STATES: PushStatus[] = ['validated', 'submitted', 'sent', 'applying', 'applied']

export default function PushStatusBanner({ status, push, rollback, onDismissRollback }: Props) {
  const { t } = useTranslation()
  const [detailsOpen, setDetailsOpen] = useState(false)
  const visiblePush = push ?? statusToPush(status)
  const timedOut = hasTimedOutRequiredTarget(visiblePush)
  const rollbackReason = safeRollbackReasonText(rollback?.reason)

  return (
    <div className="push-status-panel" aria-label={t('workloads.config.push_status.panel_aria')}>
      {!visiblePush && !rollback && (
        <div className="push-empty-state">
          <span className="push-empty-title">{t('workloads.config.push_status.empty_title')}</span>
          <span className="push-empty-copy">{t('workloads.config.push_status.empty_copy')}</span>
        </div>
      )}

      {visiblePush && (
        <div className={`push-banner push-banner-${visiblePush.status}`}>
          <div className="push-banner-row push-banner-row-main">
            <span className="push-banner-label">{label(visiblePush.status, t)}</span>
            {pushHash(visiblePush) && (
              <code className="push-banner-hash">{shortHash(pushHash(visiblePush))}</code>
            )}
            {visiblePush.updated_at && (
              <time className="push-banner-time" dateTime={visiblePush.updated_at}>
                {t('workloads.config.push_status.updated', {
                  time: formatDateTime(visiblePush.updated_at),
                })}
              </time>
            )}
          </div>

          <div className="push-summary-row">
            <span>{aggregateCopy(visiblePush, t)}</span>
            {visiblePush.push_id && <code>{visiblePush.push_id}</code>}
          </div>

          {timedOut && (
            <div className="push-timeout-banner" role="status">
              <strong>
                {visiblePush.timeout_message || t('workloads.config.push_status.no_opamp_status')}
              </strong>
              <span>{t('workloads.config.push_status.timeout_copy')}</span>
            </div>
          )}

          <Timeline push={visiblePush} t={t} />

          {visiblePush.error_message && !visiblePush.error_groups?.length && (
            <pre className="push-banner-error">
              {safeRemoteErrorText(visiblePush.error_message)}
            </pre>
          )}
          {visiblePush.error_groups && visiblePush.error_groups.length > 0 && (
            <ErrorGroups groups={visiblePush.error_groups} t={t} />
          )}

          {visiblePush.instance_statuses && visiblePush.instance_statuses.length > 0 && (
            <div className="push-instance-details">
              <button
                type="button"
                className="btn btn-small"
                onClick={() => setDetailsOpen((open) => !open)}
                aria-expanded={detailsOpen}
              >
                {detailsOpen
                  ? t('workloads.config.push_status.hide_instance_details')
                  : t('workloads.config.push_status.view_instance_details')}
              </button>
              {detailsOpen && <InstanceTable instances={visiblePush.instance_statuses} t={t} />}
            </div>
          )}
        </div>
      )}

      {rollback && (
        <div className="push-banner push-banner-rollback">
          <div className="push-banner-row">
            <span className="push-banner-label">
              {t('workloads.config.push_status.auto_rolled_back')}
            </span>
            <code className="push-banner-hash">
              {rollback.from_hash.substring(0, 8)} → {rollback.to_hash.substring(0, 8)}
            </code>
            {onDismissRollback && (
              <button
                className="push-banner-dismiss"
                onClick={onDismissRollback}
                aria-label={t('workloads.config.push_status.dismiss')}
              >
                ×
              </button>
            )}
          </div>
          {rollbackReason && <pre className="push-banner-error">{rollbackReason}</pre>}
        </div>
      )}
    </div>
  )
}

function Timeline({
  push,
  t,
}: {
  push: WorkloadConfig
  t: ReturnType<typeof useTranslation>['t']
}) {
  const entries = normalizedTimeline(push)
  return (
    <ol className="push-timeline" aria-label={t('workloads.config.push_status.timeline_aria')}>
      {entries.map((entry) => (
        <li
          key={`${entry.state}-${entry.at || 'pending'}`}
          className={`push-timeline-step push-timeline-${entryStateTone(entry, push)}`}
        >
          <span className="push-timeline-dot" aria-hidden="true" />
          <span className="push-timeline-label">{timelineLabel(entry.state, t)}</span>
          {entry.at && (
            <time className="push-timeline-time" dateTime={entry.at}>
              {formatDateTime(entry.at)}
            </time>
          )}
          {entry.message && <span className="push-timeline-message">{entry.message}</span>}
        </li>
      ))}
    </ol>
  )
}

function ErrorGroups({
  groups,
  t,
}: {
  groups: WorkloadConfigErrorGroup[]
  t: ReturnType<typeof useTranslation>['t']
}) {
  return (
    <div
      className="push-error-groups"
      aria-label={t('workloads.config.push_status.error_groups_aria')}
    >
      <p className="push-detail-heading">
        {t('workloads.config.push_status.error_groups_heading')}
      </p>
      {groups.map((group) => (
        <div
          key={group.cause}
          className={`push-error-group push-error-${group.severity || 'medium'}`}
        >
          <div className="push-error-group-header">
            <strong>{group.title || group.cause}</strong>
            <span>{formatInstanceCount(group.count, t)}</span>
            <span className="push-error-severity">{severityLabel(group.severity, t)}</span>
            <code className="push-error-cause">{group.cause}</code>
          </div>
          {group.sample_message && (
            <pre className="push-error-sample">{safeRemoteErrorText(group.sample_message)}</pre>
          )}
          {group.sample_path && <code className="push-error-path">{group.sample_path}</code>}
          {group.affected_instances && group.affected_instances.length > 0 && (
            <div className="push-affected-instances">
              {group.affected_instances.map((instance) => (
                <code key={instance}>{instance}</code>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

function InstanceTable({
  instances,
  t,
}: {
  instances: WorkloadConfigInstanceStatus[]
  t: ReturnType<typeof useTranslation>['t']
}) {
  return (
    <table className="push-instance-table">
      <thead>
        <tr>
          <th>{t('workloads.config.push_status.instance.col_instance')}</th>
          <th>{t('workloads.config.push_status.instance.col_target')}</th>
          <th>{t('workloads.config.push_status.instance.col_status')}</th>
          <th>{t('workloads.config.push_status.instance.col_updated')}</th>
          <th>{t('workloads.config.push_status.instance.col_hash')}</th>
          <th>{t('workloads.config.push_status.instance.col_error')}</th>
        </tr>
      </thead>
      <tbody>
        {instances.map((instance) => (
          <tr key={instance.instance_uid}>
            <td>
              <code>{instance.instance_uid}</code>
              {instance.pod_name && <span className="push-instance-meta">{instance.pod_name}</span>}
              {instance.node && <span className="push-instance-meta">{instance.node}</span>}
            </td>
            <td>
              {instance.required === false
                ? t('workloads.config.push_status.instance.best_effort')
                : t('workloads.config.push_status.instance.required')}
            </td>
            <td>
              <span className={`status-pill status-${instance.status}`}>
                {instanceStatusLabel(instance.status, t)}
              </span>
            </td>
            <td>{instance.updated_at ? formatDateTime(instance.updated_at) : '—'}</td>
            <td>{instance.config_hash ? <code>{shortHash(instance.config_hash)}</code> : '—'}</td>
            <td>{instanceErrorLabel(instance, t)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

function hasTimedOutRequiredTarget(push?: WorkloadConfig): boolean {
  if (!push) return false
  if (push.timed_out_waiting_for_opamp_status) return true
  return (push.instance_statuses ?? []).some(
    (instance) =>
      instance.required !== false &&
      (instance.timed_out ||
        instance.error_message === 'No OpAMP status after 30s' ||
        (instance.status === 'no_status' && push.timeout_message === 'No OpAMP status after 30s')),
  )
}

function statusToPush(status?: RemoteConfigStatus): WorkloadConfig | undefined {
  if (!status) return undefined
  return (
    status.push_status ?? {
      workload_id: '',
      config_id: status.config_hash,
      config_hash: status.config_hash,
      applied_at: status.updated_at,
      status: status.status,
      error_message: status.error_message,
      updated_at: status.updated_at,
      target_count: 1,
      applied_count: status.status === 'applied' ? 1 : 0,
      failed_count: status.status === 'failed' ? 1 : 0,
      pending_count: status.status === 'applied' || status.status === 'failed' ? 0 : 1,
    }
  )
}

function normalizedTimeline(push: WorkloadConfig): WorkloadConfigTimelineEntry[] {
  const byState = new Map((push.timeline ?? []).map((entry) => [entry.state, entry]))
  const states = [...CANONICAL_STATES]
  if (push.status.startsWith('rollback') && !states.includes(push.status)) {
    states.push(push.status)
  }
  if (push.status === 'failed' && !states.includes('failed')) {
    states.push('failed')
  }
  return states.map((state) => {
    const entry = byState.get(state)
    return entry ?? { state, at: inferredAt(push, state), terminal: isTerminal(state) }
  })
}

function inferredAt(push: WorkloadConfig, state: string): string | undefined {
  if (state === 'submitted') return push.submitted_at || push.applied_at
  if (state === 'sent') return push.sent_at
  if (state === push.status) return push.updated_at || push.applied_at
  return undefined
}

function entryStateTone(entry: WorkloadConfigTimelineEntry, push: WorkloadConfig): string {
  if (entry.timed_out) return 'timeout'
  if (entry.state === push.status)
    return push.status === 'failed' || push.status === 'rollback_failed' ? 'failed' : 'active'
  if (entry.at) return 'done'
  return 'pending'
}

function aggregateCopy(push: WorkloadConfig, t: ReturnType<typeof useTranslation>['t']): string {
  const targetCount = push.target_count ?? 0
  if (targetCount > 1) {
    const parts = [
      t('workloads.config.push_status.aggregate.applied_count', {
        applied: push.applied_count ?? 0,
        total: targetCount,
      }),
    ]
    if ((push.failed_count ?? 0) > 0) {
      parts.push(
        t('workloads.config.push_status.aggregate.failed_count', { count: push.failed_count }),
      )
    }
    if ((push.pending_count ?? 0) > 0) {
      parts.push(
        t('workloads.config.push_status.aggregate.pending_count', { count: push.pending_count }),
      )
    }
    return parts.join(' · ')
  }
  switch (push.status) {
    case 'pending':
      return t('workloads.config.push_status.aggregate.pending')
    case 'submitted':
      return t('workloads.config.push_status.aggregate.submitted')
    case 'sent':
      return t('workloads.config.push_status.aggregate.sent')
    case 'applying':
      return t('workloads.config.push_status.aggregate.applying')
    case 'applied':
      return t('workloads.config.push_status.aggregate.applied')
    case 'failed':
      return t('workloads.config.push_status.aggregate.failed')
    case 'rollback_started':
      return t('workloads.config.push_status.aggregate.rollback_started')
    case 'rollback_applied':
      return t('workloads.config.push_status.aggregate.rollback_applied')
    case 'rollback_failed':
      return t('workloads.config.push_status.aggregate.rollback_failed')
    case 'validated':
      return t('workloads.config.push_status.aggregate.validated')
    default:
      return t('workloads.config.push_status.aggregate.default')
  }
}

function pushHash(push: WorkloadConfig): string {
  return push.config_hash || push.config_id
}

function shortHash(hash: string): string {
  return hash.substring(0, 8)
}

function label(s: PushStatus, t: ReturnType<typeof useTranslation>['t']): string {
  switch (s) {
    case 'validated':
      return t('workloads.config.push_status.status.validated')
    case 'submitted':
      return t('workloads.config.push_status.status.submitted')
    case 'sent':
      return t('workloads.config.push_status.status.sent')
    case 'applying':
      return t('workloads.config.push_status.status.applying')
    case 'applied':
      return t('workloads.config.push_status.status.applied')
    case 'failed':
      return t('workloads.config.push_status.status.failed')
    case 'rollback_started':
      return t('workloads.config.push_status.status.rollback_started')
    case 'rollback_applied':
      return t('workloads.config.push_status.status.rollback_applied')
    case 'rollback_failed':
      return t('workloads.config.push_status.status.rollback_failed')
    case 'pending':
      return t('workloads.config.push_status.status.pending')
  }
}

function timelineLabel(s: string, t: ReturnType<typeof useTranslation>['t']): string {
  if (s === 'sent') return t('workloads.config.push_status.status.sent')
  if (s === 'rollback_started') return t('workloads.config.push_status.status.rollback_started')
  if (s === 'rollback_applied') return t('workloads.config.push_status.status.rollback_applied')
  if (s === 'rollback_failed') return t('workloads.config.push_status.status.rollback_failed')
  return t(`workloads.config.push_status.status.${s}`, { defaultValue: s.replaceAll('_', ' ') })
}

function instanceStatusLabel(status: string, t: ReturnType<typeof useTranslation>['t']): string {
  switch (status) {
    case 'no_status':
      return t('workloads.config.push_status.instance.status.no_status')
    case 'sent':
      return t('workloads.config.push_status.instance.status.sent')
    case 'applying':
      return t('workloads.config.push_status.status.applying')
    case 'applied':
      return t('workloads.config.push_status.instance.status.applied')
    case 'failed':
      return t('workloads.config.push_status.instance.status.failed')
    default:
      return status || t('workloads.config.push_status.instance.status.unknown')
  }
}

function instanceErrorLabel(
  instance: WorkloadConfigInstanceStatus,
  t: ReturnType<typeof useTranslation>['t'],
): string {
  if (instance.timed_out || instance.error_message === 'No OpAMP status after 30s') {
    return t('workloads.config.push_status.no_opamp_status')
  }
  if (instance.error_cause) return errorCauseLabel(instance.error_cause, t)
  if (instance.error_message) return safeRemoteErrorText(instance.error_message)
  return '—'
}

function errorCauseLabel(cause: string, t: ReturnType<typeof useTranslation>['t']): string {
  switch (cause) {
    case 'collector_validation':
      return t('workloads.config.push_status.error_cause.collector_validation')
    case 'opamp_send_failed':
      return t('workloads.config.push_status.error_cause.opamp_send_failed')
    case 'apply_timeout':
      return t('workloads.config.push_status.no_opamp_status')
    case 'capability_mismatch':
      return t('workloads.config.push_status.error_cause.capability_mismatch')
    case 'permission_or_policy':
      return t('workloads.config.push_status.error_cause.permission_or_policy')
    case 'rollback_unavailable':
      return t('workloads.config.push_status.error_cause.rollback_unavailable')
    default:
      return t('workloads.config.push_status.error_cause.redacted')
  }
}

function severityLabel(severity: string, t: ReturnType<typeof useTranslation>['t']): string {
  return t('workloads.config.push_status.severity', { severity: severity || 'medium' })
}

function formatInstanceCount(count: number, t: ReturnType<typeof useTranslation>['t']): string {
  return t('workloads.config.push_status.instance_count', { count })
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function isTerminal(status: string): boolean {
  return (
    status === 'applied' ||
    status === 'failed' ||
    status === 'rollback_applied' ||
    status === 'rollback_failed'
  )
}
