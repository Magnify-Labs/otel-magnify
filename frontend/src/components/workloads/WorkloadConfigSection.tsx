import { useEffect, useState } from 'react'
import axios from 'axios'
import { useQuery, useMutation } from '@tanstack/react-query'
import { configsAPI, workloadsAPI } from '../../api/client'
import { DOCS_BASE_URL } from '../../constants'
import YamlEditor from '../config/YamlEditor'
import PushStatusBanner from './PushStatusBanner'
import ConfigDiffView from './ConfigDiffView'
import PushHistoryTable from './PushHistoryTable'
import ConfigSafetySection from './ConfigSafetySection'
import GuidedRollbackDialog from './GuidedRollbackDialog'
import { useStore } from '../../store'
import { hasPerm } from '../../lib/perm'
import { isReadOnlyCollector } from '../../lib/workloadCapabilities'
import type {
  ValidationCheck,
  ValidationMessage,
  ValidationResult,
  Workload,
  WorkloadConfig,
  WorkloadKnownGoodConfig,
} from '../../types'

interface Props {
  workload: Workload
}

type Tab = 'edit' | 'diff'

const PUSH_TIMEOUT_MS = 30_000

function shortHash(hash?: string) {
  return hash ? hash.substring(0, 8) : '—'
}

function isNotFoundError(err: unknown) {
  return axios.isAxiosError(err) && err.response?.status === 404
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

function contentIsAvailable(row?: Pick<WorkloadConfig, 'content' | 'content_available'> | null) {
  if (!row) return false
  return row.content_available ?? !!row.content
}

interface RecoveryPanelProps {
  history: WorkloadConfig[]
  knownGood?: WorkloadKnownGoodConfig
  knownGoodMissing: boolean
  knownGoodError?: unknown
  loading: boolean
  canRollback: boolean
  onRollback: (target: WorkloadConfig) => void
}

function ConfigRecoveryPanel({
  history,
  knownGood,
  knownGoodMissing,
  knownGoodError,
  loading,
  canRollback,
  onRollback,
}: RecoveryPanelProps) {
  const current = history.find((row) => row.is_current)
  const previous = history.find((row) => row.is_previous)
  const lastKnownGoodRow = history.find((row) => row.is_last_known_good)
  const failedCandidate = history.find((row) => row.is_failed_candidate)
  const hasKnownGood = !!knownGood || !!lastKnownGoodRow
  const knownGoodHash = knownGood?.config_id ?? lastKnownGoodRow?.config_id
  const knownGoodAvailable = knownGood?.content_available ?? contentIsAvailable(lastKnownGoodRow)
  const previousAvailable = contentIsAvailable(previous)
  const knownGoodTarget =
    hasKnownGood && knownGoodAvailable
      ? (lastKnownGoodRow ??
        (knownGood
          ? ({
              workload_id: knownGood.workload_id,
              config_id: knownGood.config_id,
              applied_at: knownGood.source_applied_at ?? knownGood.marked_at,
              status: 'applied',
              pushed_by: knownGood.marked_by,
              label: 'Last known-good',
              is_last_known_good: true,
              content_available: knownGood.content_available,
            } satisfies WorkloadConfig)
          : null))
      : null
  const rollbackTarget = knownGoodTarget ?? (previousAvailable ? previous : null)
  const rollbackKind = knownGoodTarget ? 'last_known_good' : previousAvailable ? 'previous' : null
  const rollbackLabel =
    rollbackKind === 'last_known_good' ? 'Rollback to Last known-good' : 'Rollback to Previous'
  const disableReason = !canRollback
    ? 'Requires workload:push_config permission'
    : hasKnownGood && !knownGoodAvailable
      ? 'Last known-good content unavailable. Mark another applied revision before rollback.'
      : 'No Last known-good or Previous config is available for rollback.'

  return (
    <section
      className="config-recovery-panel"
      role="region"
      aria-label="Configuration recovery states"
    >
      <div className="config-recovery-header">
        <div>
          <p className="section-title">Configuration recovery states</p>
          <p className="config-recovery-help">
            Review the effective recovery target before pushing risky Collector changes.
          </p>
        </div>
        <button
          className="btn btn-primary"
          onClick={() => rollbackTarget && onRollback(rollbackTarget)}
          disabled={!canRollback || !rollbackTarget || loading}
          title={canRollback && rollbackTarget ? rollbackLabel : disableReason}
        >
          {rollbackLabel}
        </button>
      </div>
      {loading ? (
        <div className="loading">Loading recovery states...</div>
      ) : (
        <div className="config-state-grid">
          <ConfigStateCard
            title="Current"
            hash={current?.config_id}
            meta={current?.status ?? 'No current config'}
          />
          <ConfigStateCard
            title="Previous"
            hash={previous?.config_id}
            meta={previous ? formatDate(previous.applied_at) : 'None yet'}
          />
          <ConfigStateCard
            title="Last known-good"
            hash={knownGoodHash}
            meta={
              hasKnownGood
                ? knownGoodAvailable
                  ? `${knownGood?.marked_by ?? 'Unknown marker'} · ${formatDate(knownGood?.marked_at)}`
                  : 'Content unavailable'
                : knownGoodMissing
                  ? 'Last known-good: None'
                  : 'Not loaded'
            }
            detail={
              !hasKnownGood
                ? 'Rollback will use Previous until a known-good revision is marked.'
                : undefined
            }
            tone={hasKnownGood && !knownGoodAvailable ? 'danger' : 'default'}
          />
          {failedCandidate && (
            <ConfigStateCard
              title="Failed candidate"
              hash={failedCandidate.config_id}
              meta={failedCandidate.error_message || 'Candidate failed'}
              tone="danger"
            />
          )}
        </div>
      )}
      {!!knownGoodError && !knownGoodMissing && (
        <div className="error-text config-recovery-error">
          Failed to load Last known-good configuration.
        </div>
      )}
    </section>
  )
}

function ConfigStateCard({
  title,
  hash,
  meta,
  detail,
  tone = 'default',
}: {
  title: string
  hash?: string
  meta: string
  detail?: string
  tone?: 'default' | 'danger'
}) {
  return (
    <div className={`config-state-card config-state-card-${tone}`}>
      <span className="config-state-title">{title}</span>
      <code>{shortHash(hash)}</code>
      <span className="config-state-meta">{meta}</span>
      {detail && <span className="config-state-detail">{detail}</span>}
    </div>
  )
}

export default function WorkloadConfigSection({ workload }: Props) {
  const configStatus = useStore((s) => s.configStatus[workload.id])
  const rollback = useStore((s) => s.lastRollback[workload.id])
  const clearRollback = useStore((s) => s.clearAutoRollback)
  const me = useStore((s) => s.me)

  const [editMode, setEditMode] = useState(false)
  const [tab, setTab] = useState<Tab>('edit')
  const [draftYaml, setDraftYaml] = useState('')
  const [pendingHash, setPendingHash] = useState<string | null>(null)
  const [pendingPush, setPendingPush] = useState<WorkloadConfig | null>(null)
  const [timedOut, setTimedOut] = useState(false)
  const [validation, setValidation] = useState<ValidationResult | null>(null)
  const [pushError, setPushError] = useState<string | null>(null)
  const [selectedConfigId, setSelectedConfigId] = useState('')
  const [defaultRollbackTarget, setDefaultRollbackTarget] = useState<WorkloadConfig | null>(null)

  const {
    data: config,
    isLoading,
    isError,
  } = useQuery({
    queryKey: ['workload-config', workload.active_config_id],
    queryFn: () => configsAPI.get(workload.active_config_id!),
    enabled: workload.type === 'collector' && !!workload.active_config_id,
  })

  const { data: savedConfigs, isError: configsListError } = useQuery({
    queryKey: ['configs'],
    queryFn: configsAPI.list,
  })

  const { data: history = [], isLoading: historyLoading } = useQuery({
    queryKey: ['workload-config-history', workload.id],
    queryFn: () => workloadsAPI.getConfigHistory(workload.id),
    enabled: workload.type === 'collector',
  })

  const {
    data: knownGood,
    isLoading: knownGoodLoading,
    isError: knownGoodIsError,
    error: knownGoodError,
  } = useQuery({
    queryKey: ['workload-known-good', workload.id],
    queryFn: () => workloadsAPI.getKnownGood(workload.id),
    enabled: workload.type === 'collector',
    retry: false,
  })

  const activeContent = config?.content ?? ''

  const validateMutation = useMutation({
    mutationFn: () => workloadsAPI.validateConfig(workload.id, draftYaml),
    onSuccess: (result) => {
      setValidation(result)
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Validation request failed'
      setPushError(msg)
    },
  })

  const pushMutation = useMutation({
    mutationFn: () => workloadsAPI.pushConfig(workload.id, draftYaml),
    onSuccess: (res) => {
      const nextHash = res.config_hash || res.config_id || null
      setPendingHash(nextHash)
      setPendingPush(res)
      setTimedOut(false)
      setPushError(null)
    },
    onError: (err: unknown) => {
      if (axios.isAxiosError(err) && err.response?.data?.validation_errors) {
        setValidation({ valid: false, errors: err.response.data.validation_errors })
        setPushError('Configuration failed validation')
        return
      }
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to push configuration'
      setPushError(msg)
    },
  })

  const loadConfigMutation = useMutation({
    mutationFn: (configId: string) => configsAPI.get(configId),
    onSuccess: (cfg) => {
      enterEditMode(cfg.content, workload.active_config_id ? 'diff' : 'edit')
      setSelectedConfigId('')
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to load configuration'
      setPushError(`Failed to load configuration: ${msg}`)
      setSelectedConfigId('')
    },
  })

  // Reconcile local UI state with WS-driven status updates that match our
  // pending push hash. Doing this during render (rather than in an effect)
  // is the React-recommended pattern for adjusting state from external
  // sources: React drops the in-progress render and starts again with the
  // new state — no extra commit, no setState-in-effect anti-pattern. The
  // WS dispatcher already invalidates the related TanStack Query keys, so
  // this block only resets local UI state.
  if (pendingHash && configStatus && configStatus.config_hash === pendingHash) {
    if (configStatus.status === 'applied') {
      setPendingHash(null)
      setPendingPush(null)
      setTimedOut(false)
      setEditMode(false)
      setDraftYaml('')
      setValidation(null)
    } else if (configStatus.status === 'failed') {
      // keep editMode + draftYaml so the user can fix and retry
      setPendingHash(null)
      setPendingPush(null)
      setTimedOut(false)
    }
  }

  useEffect(() => {
    if (!pendingHash) return
    const timer = setTimeout(() => setTimedOut(true), PUSH_TIMEOUT_MS)
    return () => clearTimeout(timer)
  }, [pendingHash])

  function enterEditMode(initialContent: string, targetTab: Tab = 'edit') {
    setDraftYaml(initialContent)
    setEditMode(true)
    setTab(targetTab)
    setValidation(null)
    setPushError(null)
  }

  function cancelEdit() {
    setEditMode(false)
    setDraftYaml('')
    setValidation(null)
    setPushError(null)
  }

  function onDraftChange(next: string) {
    setDraftYaml(next)
    if (validation !== null) setValidation(null)
  }

  const currentPush = configStatus?.push_status ?? pendingPush ?? workload.current_config_push
  const derivedStatus =
    configStatus ??
    (currentPush
      ? {
          status: currentPush.status,
          config_hash: currentPush.config_hash || currentPush.config_id,
          error_message: currentPush.error_message,
          updated_at: currentPush.updated_at || currentPush.applied_at,
          push_status: currentPush,
        }
      : undefined)

  const canPush =
    !!draftYaml &&
    !pendingHash &&
    !pushMutation.isPending &&
    validation !== null &&
    validation.valid === true

  const canRollback = hasPerm(me?.groups, 'workload:push_config')
  const knownGoodMissing = knownGoodIsError && isNotFoundError(knownGoodError)
  const recoveryPanel = (
    <ConfigRecoveryPanel
      history={history}
      knownGood={knownGood}
      knownGoodMissing={knownGoodMissing}
      knownGoodError={knownGoodError}
      loading={historyLoading || knownGoodLoading}
      canRollback={canRollback}
      onRollback={setDefaultRollbackTarget}
    />
  )

  const defaultRollbackDialog = defaultRollbackTarget ? (
    <GuidedRollbackDialog
      workloadId={workload.id}
      target={defaultRollbackTarget}
      onClose={() => setDefaultRollbackTarget(null)}
    />
  ) : null

  const safetySection = (
    <ConfigSafetySection
      workload={workload}
      validation={validation}
      isValidating={validateMutation.isPending}
      activeConfigLoading={isLoading}
      activeConfigError={isError}
      pendingHash={pendingHash}
      timedOut={timedOut}
      configStatus={derivedStatus}
      rollback={rollback}
      canPush={canPush}
    />
  )

  // ── SDK workloads: labels as "configuration" ──────────────────────────────
  if (workload.type === 'sdk') {
    const hasLabels = Object.keys(workload.labels).length > 0
    if (!hasLabels) return null
    return (
      <>
        {safetySection}
        <p className="section-title">Configuration</p>
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
    )
  }

  // ── Collector that does not accept remote config: read-only view ─────────
  if (isReadOnlyCollector(workload)) {
    const hasConfig = !!workload.active_config_id
    return (
      <>
        {safetySection}
        {recoveryPanel}
        {defaultRollbackDialog}
        <p className="section-title">Configuration</p>
        {hasConfig && isLoading ? (
          <div className="loading">Loading configuration...</div>
        ) : hasConfig && isError ? (
          <div className="error-text">Failed to load configuration</div>
        ) : hasConfig ? (
          <YamlEditor value={activeContent} readOnly />
        ) : (
          <div className="empty-state">No config reported yet.</div>
        )}
        <div className="config-readonly-note">
          Read-only — this collector uses the <code>opamp</code> extension which can only report its
          config. Run it under the OpAMP Supervisor to enable config push.{' '}
          <a
            href={`${DOCS_BASE_URL}/users/connecting-agents.md#running-a-collector-via-opamp-supervisor`}
            target="_blank"
            rel="noreferrer"
          >
            Learn more →
          </a>
        </div>
        <PushHistoryTable workloadId={workload.id} />
      </>
    )
  }

  const editorPanel = (
    <div>
      <div className="tabstrip">
        <button
          className={`tab ${tab === 'edit' ? 'tab-active' : ''}`}
          onClick={() => setTab('edit')}
        >
          Edit
        </button>
        <button
          className={`tab ${tab === 'diff' ? 'tab-active' : ''}`}
          onClick={() => setTab('diff')}
          disabled={!workload.active_config_id}
          title={workload.active_config_id ? '' : 'No active config to diff against'}
        >
          Diff
        </button>
      </div>

      {tab === 'edit' && <YamlEditor value={draftYaml} onChange={onDraftChange} />}
      {tab === 'diff' && <ConfigDiffView oldYaml={activeContent} newYaml={draftYaml} />}

      {validation && <ValidationDetails validation={validation} />}

      {pushError && <div className="error-text error-text-push">{pushError}</div>}

      <div className="btn-row">
        <button
          className="btn"
          onClick={() => validateMutation.mutate()}
          disabled={!draftYaml || validateMutation.isPending || !!pendingHash}
        >
          {validateMutation.isPending ? 'Validating...' : 'Validate for this collector'}
        </button>
        <button
          className="btn btn-primary"
          onClick={() => pushMutation.mutate()}
          disabled={!canPush}
          title={
            validation === null
              ? 'Validate the configuration first'
              : !validation.valid
                ? 'Fix validation errors before pushing'
                : ''
          }
        >
          {pendingHash ? 'Applying...' : pushMutation.isPending ? 'Pushing...' : 'Push'}
        </button>
        <button className="btn" onClick={cancelEdit} disabled={!!pendingHash}>
          Cancel
        </button>
        {timedOut && (
          <span className="error-text error-text-inline">
            No response from workload — still applying?
          </span>
        )}
      </div>
    </div>
  )

  const isConfigsEmpty = !configsListError && (savedConfigs?.length ?? 0) === 0
  let placeholderLabel = '— Apply a saved config —'
  if (configsListError) {
    placeholderLabel = '— Failed to load configs —'
  } else if (isConfigsEmpty) {
    placeholderLabel = '— No saved configs (create one in Config Library) —'
  }

  const applySelector = (
    <select
      className="filter-select apply-config-select"
      value={selectedConfigId}
      onChange={(e) => {
        const id = e.target.value
        if (!id) return
        setSelectedConfigId(id)
        loadConfigMutation.mutate(id)
      }}
      aria-label="Apply a saved config"
      disabled={loadConfigMutation.isPending || !!pendingHash || isConfigsEmpty || configsListError}
    >
      <option value="">{placeholderLabel}</option>
      {(savedConfigs ?? []).map((c) => (
        <option key={c.id} value={c.id}>
          {c.id === workload.active_config_id ? `${c.name} (currently applied)` : c.name}
        </option>
      ))}
    </select>
  )

  // ── Collector without active config ──────────────────────────────────────
  if (!workload.active_config_id) {
    return (
      <>
        {safetySection}
        {recoveryPanel}
        {defaultRollbackDialog}
        <p className="section-title">Configuration</p>
        {applySelector}
        {editMode ? (
          editorPanel
        ) : (
          <button className="btn" onClick={() => enterEditMode('')}>
            Push a config
          </button>
        )}
        <PushStatusBanner
          status={derivedStatus}
          push={currentPush}
          rollback={rollback}
          onDismissRollback={() => clearRollback(workload.id)}
        />
        <PushHistoryTable workloadId={workload.id} />
      </>
    )
  }

  if (isLoading) {
    return (
      <>
        {safetySection}
        {recoveryPanel}
        {defaultRollbackDialog}
        <p className="section-title">Configuration</p>
        <div className="loading">Loading configuration...</div>
      </>
    )
  }
  if (isError) {
    return (
      <>
        {safetySection}
        {recoveryPanel}
        {defaultRollbackDialog}
        <p className="section-title">Configuration</p>
        <div className="error-text">Failed to load configuration</div>
      </>
    )
  }

  return (
    <>
      {safetySection}
      {recoveryPanel}
      {defaultRollbackDialog}
      <p className="section-title">Configuration</p>
      {applySelector}

      {!editMode ? (
        <div>
          <YamlEditor value={activeContent} readOnly />
          <div className="btn-row btn-row-top">
            <button className="btn" onClick={() => enterEditMode(activeContent)}>
              Edit
            </button>
          </div>
        </div>
      ) : (
        editorPanel
      )}

      <PushStatusBanner
        status={derivedStatus}
        push={currentPush}
        rollback={rollback}
        onDismissRollback={() => clearRollback(workload.id)}
      />

      <PushHistoryTable workloadId={workload.id} />
    </>
  )
}

interface ValidationDetailsProps {
  validation: ValidationResult
}

function ValidationDetails({ validation }: ValidationDetailsProps) {
  const errors = validation.errors ?? []
  const warnings = validation.warnings ?? []
  const blockingMessages: ValidationMessage[] = errors.map((error) => ({
    code: error.code,
    severity: 'error',
    message: error.message,
    path: error.path,
    check_id: error.check_id,
  }))
  const checks = validation.checks ?? legacyChecksFromResult(validation)
  const runtimeCheck = checks.find((check) => check.id === 'otelcol_runtime')
  const binaryVersion = metadataString(runtimeCheck, 'binary_version')
  const targetVersion =
    validation.target_collector_version ?? metadataString(runtimeCheck, 'target_version')
  const statusLabel = validationStatusLabel(validation)

  return (
    <section
      className={`validation-block validation-details ${validation.valid ? 'validation-ok' : 'validation-errors'}`}
      aria-label="Configuration validation result"
    >
      <div className="validation-details-header">
        <div>
          <p className="validation-details-title">{statusLabel}</p>
          {validation.summary && <p className="validation-details-summary">{validation.summary}</p>}
        </div>
        <div className="validation-version-row" aria-label="Validation versions">
          {targetVersion && <span className="validation-version-pill">Target {targetVersion}</span>}
          {binaryVersion ? (
            <span className="validation-version-pill">otelcol {binaryVersion}</span>
          ) : runtimeCheck ? (
            <span className="validation-version-pill">otelcol unavailable</span>
          ) : null}
        </div>
      </div>

      {(errors.length > 0 || warnings.length > 0) && (
        <div className="validation-message-groups">
          {errors.length > 0 && (
            <ValidationMessageGroup
              title="Blocking errors"
              tone="error"
              messages={blockingMessages}
            />
          )}
          {warnings.length > 0 && (
            <ValidationMessageGroup title="Warnings" tone="warning" messages={warnings} />
          )}
        </div>
      )}

      <div className="validation-check-grid">
        {checks.map((check) => (
          <article
            className={`validation-check-card validation-check-card-${check.status}`}
            key={check.id}
          >
            <div className="validation-check-topline">
              <div>
                <p className="validation-check-label">{check.label || humanizeCheckId(check.id)}</p>
                <p className="validation-check-source">{check.source}</p>
              </div>
              <div className="validation-check-badges">
                <span className={`validation-status-badge validation-status-badge-${check.status}`}>
                  {humanizeStatus(check.status)}
                </span>
                <span className="validation-required-badge">
                  {check.required ? 'Required' : 'Advisory'}
                </span>
              </div>
            </div>

            {check.id === 'otelcol_runtime' && (
              <dl className="validation-check-meta">
                {metadataString(check, 'binary_version') && (
                  <>
                    <dt>Binary</dt>
                    <dd>otelcol {metadataString(check, 'binary_version')}</dd>
                  </>
                )}
                {metadataString(check, 'target_version') && (
                  <>
                    <dt>Target</dt>
                    <dd>{metadataString(check, 'target_version')}</dd>
                  </>
                )}
                {metadataString(check, 'binary_path') && (
                  <>
                    <dt>Path</dt>
                    <dd>{metadataString(check, 'binary_path')}</dd>
                  </>
                )}
              </dl>
            )}

            {(check.messages ?? []).length > 0 && (
              <ul className="validation-check-messages">
                {(check.messages ?? []).map((message, index) => (
                  <ValidationMessageItem message={message} key={`${message.code}-${index}`} />
                ))}
              </ul>
            )}
          </article>
        ))}
      </div>
    </section>
  )
}

function ValidationMessageGroup({
  title,
  tone,
  messages,
}: {
  title: string
  tone: 'error' | 'warning'
  messages: ValidationMessage[]
}) {
  return (
    <div className={`validation-message-group validation-message-group-${tone}`}>
      <p className="validation-message-group-title">{title}</p>
      <ul className="validation-error-list">
        {messages.map((message, index) => (
          <ValidationMessageItem message={message} key={`${message.code}-${index}`} />
        ))}
      </ul>
    </div>
  )
}

function ValidationMessageItem({ message }: { message: ValidationMessage }) {
  return (
    <li>
      <strong>{message.code}</strong>
      {message.path ? <code className="validation-error-path">{message.path}</code> : null}
      <span className="validation-error-msg">— {message.message}</span>
    </li>
  )
}

function validationStatusLabel(validation: ValidationResult) {
  if (!validation.valid) return 'Validation failed'
  if (validation.overall_status === 'warning' || (validation.warnings ?? []).length > 0) {
    return 'Validation passed with warnings'
  }
  return 'Validation passed'
}

function legacyChecksFromResult(validation: ValidationResult): ValidationCheck[] {
  if (validation.valid) {
    return [
      {
        id: 'legacy_validation',
        label: 'Configuration validation',
        source: 'server.validation',
        status: 'passed',
        required: true,
        messages: [
          {
            code: 'validation_ok',
            severity: 'info',
            message: 'Configuration is valid.',
            check_id: 'legacy_validation',
          },
        ],
      },
    ]
  }
  return [
    {
      id: 'legacy_validation',
      label: 'Configuration validation',
      source: 'server.validation',
      status: 'failed',
      required: true,
      messages: (validation.errors ?? []).map((error) => ({
        code: error.code,
        severity: 'error' as const,
        message: error.message,
        path: error.path,
        check_id: error.check_id,
      })),
    },
  ]
}

function metadataString(check: ValidationCheck | undefined, key: string) {
  const value = check?.metadata?.[key]
  return typeof value === 'string' && value.trim() ? value : undefined
}

function humanizeStatus(status: ValidationCheck['status']) {
  switch (status) {
    case 'passed':
      return 'Passed'
    case 'warning':
      return 'Warning'
    case 'failed':
      return 'Failed'
    case 'skipped':
      return 'Skipped'
    default:
      return status
  }
}

function humanizeCheckId(id: string) {
  return id
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}
