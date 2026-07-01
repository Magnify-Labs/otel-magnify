import { useState } from 'react'
import type {
  RemoteConfigStatus,
  AutoRollbackEvent,
  PushStatus,
  WorkloadConfig,
  WorkloadConfigErrorGroup,
  WorkloadConfigInstanceStatus,
  WorkloadConfigTimelineEntry,
} from '../../types'
import { safeRemoteErrorText } from '../../lib/safeRemoteErrorText'

interface Props {
  status?: RemoteConfigStatus
  push?: WorkloadConfig
  rollback?: AutoRollbackEvent
  onDismissRollback?: () => void
}

const CANONICAL_STATES: PushStatus[] = ['validated', 'submitted', 'sent', 'applying', 'applied']

export default function PushStatusBanner({ status, push, rollback, onDismissRollback }: Props) {
  const [detailsOpen, setDetailsOpen] = useState(false)
  const visiblePush = push ?? statusToPush(status)
  const timedOut = hasTimedOutRequiredTarget(visiblePush)
  const rollbackReason = safeRemoteErrorText(rollback?.reason)

  return (
    <div className="push-status-panel" aria-label="Config push status">
      {!visiblePush && !rollback && (
        <div className="push-empty-state">
          <span className="push-empty-title">No config push in progress</span>
          <span className="push-empty-copy">
            Validate a config, then push it to see delivery status here.
          </span>
        </div>
      )}

      {visiblePush && (
        <div className={`push-banner push-banner-${visiblePush.status}`}>
          <div className="push-banner-row push-banner-row-main">
            <span className="push-banner-label">{label(visiblePush.status)}</span>
            {pushHash(visiblePush) && (
              <code className="push-banner-hash">{shortHash(pushHash(visiblePush))}</code>
            )}
            {visiblePush.updated_at && (
              <time className="push-banner-time" dateTime={visiblePush.updated_at}>
                Updated {formatDateTime(visiblePush.updated_at)}
              </time>
            )}
          </div>

          <div className="push-summary-row">
            <span>{aggregateCopy(visiblePush)}</span>
            {visiblePush.push_id && <code>{visiblePush.push_id}</code>}
          </div>

          {timedOut && (
            <div className="push-timeout-banner" role="status">
              <strong>{visiblePush.timeout_message || 'No OpAMP status after 30s'}</strong>
              <span>
                otel-magnify sent the config but has not received an OpAMP status from the workload
                yet.
              </span>
            </div>
          )}

          <Timeline push={visiblePush} />

          {visiblePush.error_message && !visiblePush.error_groups?.length && (
            <pre className="push-banner-error">
              {safeRemoteErrorText(visiblePush.error_message)}
            </pre>
          )}
          {visiblePush.error_groups && visiblePush.error_groups.length > 0 && (
            <ErrorGroups groups={visiblePush.error_groups} />
          )}

          {visiblePush.instance_statuses && visiblePush.instance_statuses.length > 0 && (
            <div className="push-instance-details">
              <button
                type="button"
                className="btn btn-small"
                onClick={() => setDetailsOpen((open) => !open)}
                aria-expanded={detailsOpen}
              >
                {detailsOpen ? 'Hide instance details' : 'View instance details'}
              </button>
              {detailsOpen && <InstanceTable instances={visiblePush.instance_statuses} />}
            </div>
          )}
        </div>
      )}

      {rollback && (
        <div className="push-banner push-banner-rollback">
          <div className="push-banner-row">
            <span className="push-banner-label">Auto-rolled back</span>
            <code className="push-banner-hash">
              {rollback.from_hash.substring(0, 8)} → {rollback.to_hash.substring(0, 8)}
            </code>
            {onDismissRollback && (
              <button
                className="push-banner-dismiss"
                onClick={onDismissRollback}
                aria-label="Dismiss"
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

function Timeline({ push }: { push: WorkloadConfig }) {
  const entries = normalizedTimeline(push)
  return (
    <ol className="push-timeline" aria-label="Push timeline">
      {entries.map((entry) => (
        <li
          key={`${entry.state}-${entry.at || 'pending'}`}
          className={`push-timeline-step push-timeline-${entryStateTone(entry, push)}`}
        >
          <span className="push-timeline-dot" aria-hidden="true" />
          <span className="push-timeline-label">{timelineLabel(entry.state)}</span>
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

function ErrorGroups({ groups }: { groups: WorkloadConfigErrorGroup[] }) {
  return (
    <div className="push-error-groups" aria-label="Grouped remote config errors">
      <p className="push-detail-heading">Remote config errors grouped by cause</p>
      {groups.map((group) => (
        <div
          key={group.cause}
          className={`push-error-group push-error-${group.severity || 'medium'}`}
        >
          <div className="push-error-group-header">
            <strong>{group.title || group.cause}</strong>
            <span>{formatInstanceCount(group.count)}</span>
            <span className="push-error-severity">{severityLabel(group.severity)}</span>
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

function InstanceTable({ instances }: { instances: WorkloadConfigInstanceStatus[] }) {
  return (
    <table className="push-instance-table">
      <thead>
        <tr>
          <th>Instance</th>
          <th>Target</th>
          <th>Status</th>
          <th>Updated</th>
          <th>Hash</th>
          <th>Error</th>
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
            <td>{instance.required === false ? 'Best effort' : 'Required'}</td>
            <td>
              <span className={`status-pill status-${instance.status}`}>
                {instanceStatusLabel(instance.status)}
              </span>
            </td>
            <td>{instance.updated_at ? formatDateTime(instance.updated_at) : '—'}</td>
            <td>{instance.config_hash ? <code>{shortHash(instance.config_hash)}</code> : '—'}</td>
            <td>{instanceErrorLabel(instance)}</td>
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

function aggregateCopy(push: WorkloadConfig): string {
  const targetCount = push.target_count ?? 0
  if (targetCount > 1) {
    const parts = [`${push.applied_count ?? 0}/${targetCount} applied`]
    if ((push.failed_count ?? 0) > 0) parts.push(`${push.failed_count} failed`)
    if ((push.pending_count ?? 0) > 0) parts.push(`${push.pending_count} pending`)
    return parts.join(' · ')
  }
  switch (push.status) {
    case 'pending':
      return 'Push is pending submission.'
    case 'submitted':
      return 'Request accepted. Waiting for OpAMP delivery.'
    case 'sent':
      return 'Sent via OpAMP. Waiting for remote status.'
    case 'applying':
      return 'The workload is applying this config.'
    case 'applied':
      return 'Config applied successfully.'
    case 'failed':
      return 'Config push failed. Review errors and affected instances.'
    case 'rollback_started':
      return 'Rollback is in progress.'
    case 'rollback_applied':
      return 'Rollback applied successfully.'
    case 'rollback_failed':
      return 'Rollback failed. Review details before retrying.'
    case 'validated':
      return 'Validation passed. Ready to push.'
    default:
      return 'Push status is available.'
  }
}

function pushHash(push: WorkloadConfig): string {
  return push.config_hash || push.config_id
}

function shortHash(hash: string): string {
  return hash.substring(0, 8)
}

function label(s: PushStatus): string {
  switch (s) {
    case 'validated':
      return 'Validated'
    case 'submitted':
      return 'Submitted'
    case 'sent':
      return 'Sent via OpAMP'
    case 'applying':
      return 'Applying'
    case 'applied':
      return '✓ Applied'
    case 'failed':
      return '✗ Failed'
    case 'rollback_started':
      return 'Rolling back'
    case 'rollback_applied':
      return 'Rolled back'
    case 'rollback_failed':
      return 'Rollback failed'
    case 'pending':
      return 'Pending'
  }
}

function timelineLabel(s: string): string {
  if (s === 'sent') return 'Sent via OpAMP'
  if (s === 'rollback_started') return 'Rolling back'
  if (s === 'rollback_applied') return 'Rolled back'
  if (s === 'rollback_failed') return 'Rollback failed'
  return s.charAt(0).toUpperCase() + s.slice(1).replaceAll('_', ' ')
}

function instanceStatusLabel(status: string): string {
  switch (status) {
    case 'no_status':
      return 'No status yet'
    case 'sent':
      return 'Sent'
    case 'applying':
      return 'Applying'
    case 'applied':
      return 'Applied'
    case 'failed':
      return 'Failed'
    default:
      return status || 'Unknown'
  }
}

function instanceErrorLabel(instance: WorkloadConfigInstanceStatus): string {
  if (instance.timed_out || instance.error_message === 'No OpAMP status after 30s') {
    return 'No OpAMP status after 30s'
  }
  if (instance.error_cause) return errorCauseLabel(instance.error_cause)
  if (instance.error_message) return safeRemoteErrorText(instance.error_message)
  return '—'
}

function errorCauseLabel(cause: string): string {
  switch (cause) {
    case 'collector_validation':
      return 'Collector rejected the config'
    case 'opamp_send_failed':
      return 'OpAMP delivery failed'
    case 'apply_timeout':
      return 'No OpAMP status after 30s'
    case 'capability_mismatch':
      return 'Collector capability mismatch'
    case 'permission_or_policy':
      return 'Permission or policy blocked the config'
    case 'rollback_unavailable':
      return 'Rollback status unavailable'
    default:
      return 'Remote config error details redacted'
  }
}

function severityLabel(severity: string): string {
  if (!severity) return 'Severity: medium'
  return `Severity: ${severity}`
}

function formatInstanceCount(count: number): string {
  return `${count} ${count === 1 ? 'instance' : 'instances'}`
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
