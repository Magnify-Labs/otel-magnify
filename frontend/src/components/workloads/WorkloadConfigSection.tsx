import { useEffect, useState } from 'react'
import axios from 'axios'
import { useQuery, useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configsAPI, pushesAPI, workloadsAPI } from '../../api/client'
import { DOCS_BASE_URL } from '../../constants'
import YamlEditor from '../config/YamlEditor'
import PushStatusBanner from './PushStatusBanner'
import ConfigDiffView from './ConfigDiffView'
import PushHistoryTable from './PushHistoryTable'
import ConfigSafetySection from './ConfigSafetySection'
import GuidedRollbackDialog from './GuidedRollbackDialog'
import ManualCanaryPanel from './ManualCanaryPanel'
import { useStore } from '../../store'
import { hasPerm } from '../../lib/perm'
import { isReadOnlyCollector } from '../../lib/workloadCapabilities'
import type {
  PushGroup,
  PushGroupSelector,
  PushPreview,
  PushPreviewRequest,
  ValidationCheck,
  ConfigApplicationPlan,
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
type PlanExportStatus = 'idle' | 'ready' | 'error'
type PushScopeMode = 'single' | 'saved' | 'dynamic'

interface DynamicSelectorState {
  cluster: string
  namespace: string
  env: string
  team: string
  workloadType: string
  version: string
  capabilities: string
}

const PUSH_TIMEOUT_MS = 30_000

const emptyDynamicSelector: DynamicSelectorState = {
  cluster: '',
  namespace: '',
  env: '',
  team: '',
  workloadType: '',
  version: '',
  capabilities: '',
}

function splitList(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function buildDynamicSelector(fields: DynamicSelectorState): PushGroupSelector {
  const matchLabels: Record<string, string> = {}
  if (fields.cluster.trim()) matchLabels.cluster = fields.cluster.trim()
  if (fields.namespace.trim()) matchLabels.namespace = fields.namespace.trim()
  if (fields.env.trim()) matchLabels.env = fields.env.trim()
  if (fields.team.trim()) matchLabels.team = fields.team.trim()
  if (fields.workloadType.trim()) matchLabels.workload_type = fields.workloadType.trim()

  const selector: PushGroupSelector = { types: ['collector'] }
  if (Object.keys(matchLabels).length > 0) selector.match_labels = matchLabels
  const versions = splitList(fields.version)
  if (versions.length > 0) selector.versions = versions
  const capabilities = splitList(fields.capabilities)
  if (capabilities.length > 0) selector.capabilities = capabilities
  return selector
}

function hasDynamicSelector(fields: DynamicSelectorState): boolean {
  return Object.values(fields).some((value) => value.trim().length > 0)
}

function previewBlockedCount(preview: PushPreview | null): number {
  if (!preview) return 0
  return preview.breakdown.read_only + preview.breakdown.incompatible + preview.breakdown.offline
}

function translationLookupKey(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '')
}

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

function ConfigApplicationPlanPanel({
  plan,
  isExporting,
  exportStatus,
  onExport,
}: {
  plan: ConfigApplicationPlan
  isExporting: boolean
  exportStatus: PlanExportStatus
  onExport: () => void
}) {
  const { t } = useTranslation()
  const blocked = !plan.can_push || !plan.apply_allowed || plan.hard_failures.length > 0
  const summary = plan.summary
  const hasBackendExport = plan.export.supported && plan.export.formats.includes('markdown')
  const counters = [
    [t('workloads.config.application_plan.counter.targets'), summary.target_count],
    [t('workloads.config.application_plan.counter.collectors'), summary.collector_target_count],
    [
      t('workloads.config.application_plan.counter.remote_config'),
      summary.remote_config_capable_count,
    ],
    [t('workloads.config.application_plan.counter.readonly'), summary.read_only_count],
    [t('workloads.config.application_plan.counter.validation_ok'), summary.validation_ok_count],
    [
      t('workloads.config.application_plan.counter.validation_failed'),
      summary.validation_failed_count,
    ],
    [
      t('workloads.config.application_plan.counter.components_missing'),
      summary.components_missing_count,
    ],
    [t('workloads.config.application_plan.counter.high_risk'), summary.high_risk_change_count],
    [t('workloads.config.application_plan.counter.excluded'), summary.excluded_count],
  ] as const

  return (
    <section
      className={`config-application-plan ${blocked ? 'config-application-plan-blocked' : ''}`}
      aria-labelledby="config-application-plan-title"
    >
      <div className="config-plan-header">
        <div>
          <p className="section-title" id="config-application-plan-title">
            {t('workloads.config.application_plan.title')}
          </p>
          <p className="config-plan-help">
            {blocked
              ? t('workloads.config.application_plan.blocked_help')
              : t('workloads.config.application_plan.ready_help')}
          </p>
        </div>
        <div className={`config-plan-status ${blocked ? 'config-plan-status-blocked' : ''}`}>
          {blocked
            ? t('workloads.config.application_plan.status.blocked')
            : t('workloads.config.application_plan.status.ready')}
        </div>
      </div>

      <div
        className="config-plan-counter-grid"
        aria-label={t('workloads.config.application_plan.counter_aria')}
      >
        {counters.map(([label, value]) => (
          <div className="config-plan-counter" key={label}>
            <span>{label}</span>
            <strong>{value}</strong>
          </div>
        ))}
      </div>

      {summary.high_risk_change_count > 0 && (
        <div className="config-plan-warning" role="note">
          {t('workloads.config.application_plan.high_risk_note')}
        </div>
      )}

      {plan.hard_failures.length > 0 && (
        <div className="config-plan-failures" role="alert">
          <strong>{t('workloads.config.application_plan.hard_failures')}</strong>
          <ul>
            {plan.hard_failures.map((failure) => (
              <li key={failure}>{humanizePlanReason(failure)}</li>
            ))}
          </ul>
        </div>
      )}

      <div className="config-plan-target-list">
        {plan.targets.map((target) => {
          const reasons = [...target.exclusion_reasons, ...target.hard_failures]
          return (
            <article
              className={`config-plan-target ${target.excluded ? 'config-plan-target-excluded' : ''}`}
              key={target.workload_id}
            >
              <div className="config-plan-target-topline">
                <div>
                  <h3>{target.display_name || target.workload_id}</h3>
                  <code>{target.workload_id}</code>
                </div>
                <span
                  className={`config-plan-target-status config-plan-target-${target.validation_status}`}
                >
                  {target.excluded
                    ? t('workloads.config.application_plan.target.excluded')
                    : target.validation_status === 'ok'
                      ? t('workloads.config.application_plan.target.valid')
                      : t('workloads.config.application_plan.target.invalid')}
                </span>
              </div>
              <div className="config-plan-target-meta">
                <span>
                  {target.accepts_remote_config
                    ? t('workloads.config.application_plan.target.remote_capable')
                    : t('workloads.config.application_plan.target.readonly')}
                </span>
                <span>
                  {t('workloads.config.application_plan.target.components_missing', {
                    count: target.components_missing_count,
                  })}
                </span>
                <span>
                  {t('workloads.config.application_plan.target.high_risk', {
                    count: target.high_risk_change_count,
                  })}
                </span>
              </div>
              {(reasons.length > 0 || (target.validation_errors ?? []).length > 0) && (
                <ul className="config-plan-reason-list">
                  {Array.from(new Set(reasons)).map((reason) => (
                    <li key={reason}>{humanizePlanReason(reason)}</li>
                  ))}
                  {(target.validation_errors ?? []).map((error) => (
                    <li key={error}>{error}</li>
                  ))}
                </ul>
              )}
            </article>
          )
        })}
      </div>

      <div className="config-plan-actions">
        <button
          className="btn"
          onClick={onExport}
          disabled={isExporting}
          aria-describedby="config-plan-export-status"
        >
          {isExporting
            ? t('workloads.config.application_plan.action.exporting')
            : t('workloads.config.application_plan.action.export')}
        </button>
        <button
          className="btn"
          disabled
          title={t('workloads.config.application_plan.action.rollout_title')}
        >
          {t('workloads.config.application_plan.action.save_rollout')}
        </button>
        <span id="config-plan-export-status" className="config-plan-action-status" role="status">
          {exportStatus === 'ready'
            ? t('workloads.config.application_plan.export_ready')
            : exportStatus === 'error'
              ? t('workloads.config.application_plan.export_error')
              : hasBackendExport
                ? t('workloads.config.application_plan.rollout_not_persisted')
                : t('workloads.config.application_plan.export_fallback')}
        </span>
      </div>
    </section>
  )
}

export default function WorkloadConfigSection({ workload }: Props) {
  const { t } = useTranslation()
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
  const [applicationPlan, setApplicationPlan] = useState<ConfigApplicationPlan | null>(null)
  const [planExportStatus, setPlanExportStatus] = useState<PlanExportStatus>('idle')
  const [selectedConfigId, setSelectedConfigId] = useState('')
  const [scopeMode, setScopeMode] = useState<PushScopeMode>('single')
  const [selectedPushGroupId, setSelectedPushGroupId] = useState('')
  const [dynamicSelector, setDynamicSelector] = useState<DynamicSelectorState>(emptyDynamicSelector)
  const [pushPreview, setPushPreview] = useState<PushPreview | null>(null)
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

  const {
    data: pushGroups,
    isLoading: pushGroupsLoading,
    isError: pushGroupsError,
  } = useQuery({
    queryKey: ['push-groups'],
    queryFn: pushesAPI.groups,
    enabled: editMode && scopeMode === 'saved',
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

  const {
    data: readOnlyPlan,
    isLoading: readOnlyPlanLoading,
    isError: readOnlyPlanError,
  } = useQuery({
    queryKey: ['workload-config-plan-readonly', workload.id, activeContent],
    queryFn: () => workloadsAPI.planConfig(workload.id, activeContent),
    enabled: workload.type === 'collector' && isReadOnlyCollector(workload) && !!activeContent,
    retry: false,
  })

  const validateMutation = useMutation({
    mutationFn: () => workloadsAPI.validateConfig(workload.id, draftYaml),
    onSuccess: (result) => {
      setValidation(result)
      setApplicationPlan(null)
      setPlanExportStatus('idle')
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Validation request failed'
      setPushError(msg)
    },
  })

  const planMutation = useMutation({
    mutationFn: () => workloadsAPI.planConfig(workload.id, draftYaml),
    onSuccess: (plan) => {
      setApplicationPlan(plan)
      setPlanExportStatus('idle')
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to generate config safety plan'
      setPushError(msg)
    },
  })

  const exportPlanMutation = useMutation({
    mutationFn: () =>
      workloadsAPI.exportConfigPlanMarkdown(workload.id, draftYaml || activeContent),
    onSuccess: (blob) => {
      downloadBlob(blob, 'config-safety-plan.md')
      setPlanExportStatus('ready')
    },
    onError: () => {
      setPlanExportStatus('error')
    },
  })

  function exportApplicationPlan(plan: ConfigApplicationPlan) {
    if (plan.export.supported && plan.export.formats.includes('markdown')) {
      exportPlanMutation.mutate()
      return
    }

    downloadBlob(
      new Blob([JSON.stringify(plan, null, 2)], { type: 'application/json' }),
      'config-safety-plan.json',
    )
    setPlanExportStatus('ready')
  }

  const pushMutation = useMutation({
    mutationFn: () => workloadsAPI.pushConfig(workload.id, draftYaml),
    onSuccess: (res) => {
      const nextHash = res.config_hash || res.config_id || null
      setPendingHash(nextHash)
      setPendingPush(res)
      setTimedOut(false)
      setPushPreview(null)
      setPushError(null)
      setPlanExportStatus('idle')
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

  const previewMutation = useMutation({
    mutationFn: (request: PushPreviewRequest) => pushesAPI.preview(request),
    onSuccess: (preview) => {
      setPushPreview(preview)
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : t('workloads.config.scope.preview_failed')
      setPushError(msg)
      setPushPreview(null)
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
      setApplicationPlan(null)
      setPushPreview(null)
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
    setApplicationPlan(null)
    setPlanExportStatus('idle')
    setPushPreview(null)
    setPushError(null)
  }

  function cancelEdit() {
    setEditMode(false)
    setDraftYaml('')
    setValidation(null)
    setApplicationPlan(null)
    setPlanExportStatus('idle')
    setPushPreview(null)
    setScopeMode('single')
    setSelectedPushGroupId('')
    setDynamicSelector(emptyDynamicSelector)
    setPushError(null)
  }

  function onDraftChange(next: string) {
    setDraftYaml(next)
    if (validation !== null) setValidation(null)
    if (applicationPlan !== null) setApplicationPlan(null)
    if (pushPreview !== null) setPushPreview(null)
    if (planExportStatus !== 'idle') setPlanExportStatus('idle')
  }

  function updateScopeMode(next: PushScopeMode) {
    setScopeMode(next)
    setPushPreview(null)
    setPushError(null)
  }

  function updateDynamicSelector(field: keyof DynamicSelectorState, value: string) {
    setDynamicSelector((current) => ({ ...current, [field]: value }))
    setPushPreview(null)
  }

  function previewTargets() {
    if (!canPreview) {
      setPushError(previewDisabledReason || t('workloads.config.scope.preview_failed'))
      return
    }
    const request: PushPreviewRequest = { config_content: draftYaml }
    if (scopeMode === 'saved') {
      request.group_id = selectedPushGroupId
    } else if (scopeMode === 'dynamic') {
      request.selector = buildDynamicSelector(dynamicSelector)
    } else {
      return
    }
    previewMutation.mutate(request)
  }

  function validateConfig() {
    if (!canValidateConfig) {
      setPushError(t('workloads.config.permission.validate_blocked'))
      return
    }
    if (!draftYaml || validateMutation.isPending || pendingHash) return
    validateMutation.mutate()
  }

  function generateApplicationPlan() {
    if (!canGeneratePlan) {
      setPushError(planDisabledReason || t('workloads.config.scope.disabled.generate_valid_plan'))
      return
    }
    planMutation.mutate()
  }

  function submitPush() {
    if (!canPush) {
      setPushError(pushDisabledReason || t('workloads.config.scope.disabled.plan_blocks_push'))
      return
    }
    pushMutation.mutate()
  }

  function formatPushGroupName(group: PushGroup): string {
    return t(`workloads.config.scope.group.${translationLookupKey(group.id)}`, {
      defaultValue: group.name,
    })
  }

  function formatPreviewReason(reason: string): string {
    return t(`workloads.config.scope.reason.${translationLookupKey(reason)}`, {
      defaultValue: reason,
    })
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

  const hasPushPermission = hasPerm(me?.groups, 'workload:push_config')
  const canValidateConfig = hasPerm(me?.groups, 'workload:validate_config')
  const canGeneratePlan =
    canValidateConfig &&
    !!draftYaml &&
    !pendingHash &&
    !planMutation.isPending &&
    validation !== null &&
    validation.valid === true
  const canPush =
    hasPushPermission &&
    scopeMode === 'single' &&
    canGeneratePlan &&
    !!applicationPlan &&
    applicationPlan.can_push &&
    applicationPlan.apply_allowed &&
    applicationPlan.hard_failures.length === 0 &&
    !pushMutation.isPending
  const canPreview =
    canValidateConfig &&
    hasPushPermission &&
    !!draftYaml &&
    validation?.valid === true &&
    !pendingHash &&
    !previewMutation.isPending &&
    ((scopeMode === 'saved' && !!selectedPushGroupId) ||
      (scopeMode === 'dynamic' && hasDynamicSelector(dynamicSelector)))
  const isBulkScope = scopeMode !== 'single'
  const blockedCount = previewBlockedCount(pushPreview)
  const savedGroupsEmpty =
    scopeMode === 'saved' &&
    !pushGroupsLoading &&
    !pushGroupsError &&
    (pushGroups?.length ?? 0) === 0
  const validateDisabledReason = !canValidateConfig
    ? t('workloads.config.permission.validate_blocked')
    : !draftYaml
      ? t('workloads.config.scope.disabled.enter_yaml_validate')
      : pendingHash
        ? t('workloads.config.scope.disabled.wait_current_validate')
        : ''
  const previewDisabledReason = !hasPushPermission
    ? t('workloads.config.permission.push_blocked')
    : !canValidateConfig
      ? t('workloads.config.permission.validate_blocked')
      : !draftYaml
        ? t('workloads.config.scope.disabled.enter_yaml_preview')
        : validation === null
          ? t('workloads.config.scope.disabled.validate_first')
          : !validation.valid
            ? t('workloads.config.scope.disabled.fix_validation_preview')
            : pendingHash
              ? t('workloads.config.scope.disabled.wait_current_preview')
              : scopeMode === 'saved' && !selectedPushGroupId
                ? t('workloads.config.scope.disabled.select_saved_group')
                : scopeMode === 'dynamic' && !hasDynamicSelector(dynamicSelector)
                  ? t('workloads.config.scope.disabled.enter_selector')
                  : ''
  const planDisabledReason =
    validation === null
      ? t('workloads.config.scope.disabled.validate_first')
      : !validation.valid
        ? t('workloads.config.scope.disabled.fix_validation_plan')
        : pendingHash
          ? t('workloads.config.scope.disabled.wait_current_plan')
          : !canValidateConfig
            ? t('workloads.config.permission.validate_blocked')
            : ''
  const pushDisabledReason = !hasPushPermission
    ? t('workloads.config.permission.push_blocked')
    : validation === null
      ? t('workloads.config.scope.disabled.validate_first')
      : !validation.valid
        ? t('workloads.config.scope.disabled.fix_validation_push')
        : isBulkScope
          ? t('workloads.config.scope.bulk_push_unavailable')
          : !applicationPlan
            ? t('workloads.config.scope.disabled.generate_plan')
            : applicationPlan.hard_failures.length > 0 ||
                !applicationPlan.can_push ||
                !applicationPlan.apply_allowed
              ? t('workloads.config.scope.disabled.plan_blocks_push')
              : pendingHash
                ? t('workloads.config.scope.disabled.wait_current_push')
                : ''

  const canRollback = hasPushPermission
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

  const visiblePlan = applicationPlan ?? readOnlyPlan ?? null
  const applicationPlanPanel = visiblePlan ? (
    <ConfigApplicationPlanPanel
      plan={visiblePlan}
      isExporting={exportPlanMutation.isPending}
      exportStatus={planExportStatus}
      onExport={() => exportApplicationPlan(visiblePlan)}
    />
  ) : readOnlyPlanLoading ? (
    <div className="loading">Loading config safety plan...</div>
  ) : readOnlyPlanError ? (
    <div className="error-text">Failed to load config safety plan.</div>
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
        {applicationPlanPanel}
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

      <ManualCanaryPanel
        workloadId={workload.id}
        draftYaml={draftYaml}
        disabled={!!pendingHash || pushMutation.isPending}
        canPush={hasPushPermission}
        safetyPlanReady={canPush}
      />

      <section className="push-scope-panel" aria-labelledby="push-scope-title">
        <div className="push-scope-header">
          <div>
            <h3 id="push-scope-title">{t('workloads.config.scope.title')}</h3>
            <p>{t('workloads.config.scope.help')}</p>
          </div>
          {isBulkScope && (
            <span className="push-scope-mode-badge">
              {t('workloads.config.scope.preview_only')}
            </span>
          )}
        </div>

        <div className="push-scope-controls">
          <label>
            <span>{t('workloads.config.scope.mode_label')}</span>
            <select
              className="filter-select push-scope-mode-select"
              value={scopeMode}
              onChange={(e) => updateScopeMode(e.target.value as PushScopeMode)}
              disabled={!!pendingHash || !hasPushPermission}
              title={!hasPushPermission ? t('workloads.config.permission.push_blocked') : ''}
              aria-describedby={!hasPushPermission ? 'push-scope-permission-note' : undefined}
            >
              <option value="single">{t('workloads.config.scope.single')}</option>
              <option value="saved">{t('workloads.config.scope.saved')}</option>
              <option value="dynamic">{t('workloads.config.scope.dynamic')}</option>
            </select>
          </label>

          {scopeMode === 'saved' && (
            <label>
              <span>{t('workloads.config.scope.saved_label')}</span>
              <select
                className="filter-select push-saved-group-select"
                value={selectedPushGroupId}
                onChange={(e) => {
                  if (!hasPushPermission) return
                  setSelectedPushGroupId(e.target.value)
                  setPushPreview(null)
                }}
                disabled={!!pendingHash || !hasPushPermission || pushGroupsError}
                title={!hasPushPermission ? t('workloads.config.permission.push_blocked') : ''}
                aria-describedby={!hasPushPermission ? 'push-scope-permission-note' : undefined}
              >
                <option value="">
                  {pushGroupsError
                    ? t('workloads.config.scope.groups_error')
                    : pushGroupsLoading
                      ? t('workloads.config.scope.groups_loading')
                      : savedGroupsEmpty
                        ? t('workloads.config.scope.groups_empty')
                        : t('workloads.config.scope.saved_placeholder')}
                </option>
                {(pushGroups ?? []).map((group) => (
                  <option key={group.id} value={group.id}>
                    {formatPushGroupName(group)}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>

        {scopeMode === 'dynamic' && (
          <div className="push-dynamic-grid">
            <label>
              <span>{t('workloads.config.scope.field.cluster')}</span>
              <input
                value={dynamicSelector.cluster}
                onChange={(e) => updateDynamicSelector('cluster', e.target.value)}
                disabled={!!pendingHash || !hasPushPermission}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.namespace')}</span>
              <input
                value={dynamicSelector.namespace}
                onChange={(e) => updateDynamicSelector('namespace', e.target.value)}
                disabled={!!pendingHash || !hasPushPermission}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.env')}</span>
              <input
                value={dynamicSelector.env}
                onChange={(e) => updateDynamicSelector('env', e.target.value)}
                disabled={!!pendingHash || !hasPushPermission}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.team')}</span>
              <input
                value={dynamicSelector.team}
                onChange={(e) => updateDynamicSelector('team', e.target.value)}
                disabled={!!pendingHash || !hasPushPermission}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.workload_type')}</span>
              <input
                value={dynamicSelector.workloadType}
                onChange={(e) => updateDynamicSelector('workloadType', e.target.value)}
                disabled={!!pendingHash || !hasPushPermission}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.version')}</span>
              <input
                value={dynamicSelector.version}
                onChange={(e) => updateDynamicSelector('version', e.target.value)}
                placeholder={t('workloads.config.scope.placeholder.version')}
                disabled={!!pendingHash || !hasPushPermission}
              />
            </label>
            <label className="push-dynamic-wide">
              <span>{t('workloads.config.scope.field.capabilities')}</span>
              <input
                value={dynamicSelector.capabilities}
                onChange={(e) => updateDynamicSelector('capabilities', e.target.value)}
                placeholder={t('workloads.config.scope.placeholder.capabilities')}
                disabled={!!pendingHash || !hasPushPermission}
              />
            </label>
          </div>
        )}

        {pushPreview && (
          <div className="push-preview-panel" aria-live="polite">
            <div className="push-preview-counts">
              <span>
                {t('workloads.config.scope.count.targeted', { count: pushPreview.targeted_count })}
              </span>
              <span>
                {t('workloads.config.scope.count.capable', {
                  count: pushPreview.breakdown.remote_config_capable,
                })}
              </span>
              <span>
                {t('workloads.config.scope.count.readonly', {
                  count: pushPreview.breakdown.read_only,
                })}
              </span>
              <span>
                {t('workloads.config.scope.count.incompatible', {
                  count: pushPreview.breakdown.incompatible,
                })}
              </span>
              <span>
                {t('workloads.config.scope.count.offline', {
                  count: pushPreview.breakdown.offline,
                })}
              </span>
            </div>
            {blockedCount > 0 ? (
              <>
                <div className="push-preview-warning">
                  {t('workloads.config.scope.blocked_warning')}
                </div>
                <ul className="push-preview-blocked">
                  {pushPreview.targets
                    .filter((target) => target.bucket !== 'remote_config_capable')
                    .slice(0, 5)
                    .map((target) => (
                      <li key={target.workload_id}>
                        <strong>{target.display_name || target.workload_id}</strong>
                        <span>{t(`workloads.config.scope.bucket.${target.bucket}`)}</span>
                        <span>
                          {t(
                            `workloads.config.scope.status.${translationLookupKey(target.status)}`,
                            {
                              defaultValue: target.status,
                            },
                          )}
                        </span>
                        {target.reason && <em>{formatPreviewReason(target.reason)}</em>}
                      </li>
                    ))}
                </ul>
              </>
            ) : (
              <div className="push-preview-ready">{t('workloads.config.scope.ready')}</div>
            )}
          </div>
        )}
        {!hasPushPermission && (
          <div className="push-scope-permission-note" id="push-scope-permission-note" role="note">
            {t('workloads.config.permission.push_blocked')}
          </div>
        )}
      </section>
      {applicationPlanPanel}

      {pushError && <div className="error-text error-text-push">{pushError}</div>}

      <div className="btn-row">
        <button
          className="btn"
          onClick={validateConfig}
          disabled={!canValidateConfig || !draftYaml || validateMutation.isPending || !!pendingHash}
          title={validateDisabledReason}
        >
          {validateMutation.isPending ? 'Validating...' : 'Validate for this collector'}
        </button>
        <button
          className="btn"
          onClick={previewTargets}
          disabled={!canPreview}
          title={previewDisabledReason}
        >
          {previewMutation.isPending
            ? t('workloads.config.scope.previewing')
            : t('workloads.config.scope.preview_button')}
        </button>
        <button
          className="btn btn-primary"
          onClick={generateApplicationPlan}
          disabled={!canGeneratePlan}
          title={planDisabledReason}
        >
          {planMutation.isPending ? 'Generating plan...' : 'Generate safety plan'}
        </button>
        <button
          className="btn btn-primary"
          onClick={submitPush}
          disabled={!canPush}
          title={pushDisabledReason}
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
  let placeholderLabel = t('workloads.config.apply.placeholder')
  if (configsListError) {
    placeholderLabel = t('workloads.config.apply.error')
  } else if (isConfigsEmpty) {
    placeholderLabel = t('workloads.config.apply.empty')
  }

  const applySelector = (
    <select
      className="filter-select apply-config-select"
      value={selectedConfigId}
      onChange={(e) => {
        if (!hasPushPermission) return
        const id = e.target.value
        if (!id) return
        setSelectedConfigId(id)
        loadConfigMutation.mutate(id)
      }}
      aria-label={t('workloads.config.apply.aria')}
      aria-describedby={!hasPushPermission ? 'config-permission-note' : undefined}
      title={!hasPushPermission ? t('workloads.config.permission.push_blocked') : ''}
      disabled={
        !hasPushPermission ||
        loadConfigMutation.isPending ||
        !!pendingHash ||
        isConfigsEmpty ||
        configsListError
      }
    >
      <option value="">{placeholderLabel}</option>
      {(savedConfigs ?? []).map((c) => (
        <option key={c.id} value={c.id}>
          {c.id === workload.active_config_id
            ? t('workloads.config.apply.currently_applied', { name: c.name })
            : c.name}
        </option>
      ))}
    </select>
  )

  const permissionNote = !hasPushPermission ? (
    <div className="config-permission-note" id="config-permission-note" role="note">
      {t('workloads.config.permission.push_blocked')}
    </div>
  ) : null

  // ── Collector without active config ──────────────────────────────────────
  if (!workload.active_config_id) {
    return (
      <>
        {safetySection}
        {recoveryPanel}
        {defaultRollbackDialog}
        <p className="section-title">Configuration</p>
        {applySelector}
        {permissionNote}
        {editMode ? (
          editorPanel
        ) : (
          <button
            className="btn"
            onClick={() => enterEditMode('')}
            disabled={!hasPushPermission}
            title={!hasPushPermission ? t('workloads.config.permission.push_blocked') : ''}
            aria-describedby={!hasPushPermission ? 'config-permission-note' : undefined}
          >
            {t('workloads.config.action.push_config')}
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
      {permissionNote}

      {!editMode ? (
        <div>
          <YamlEditor value={activeContent} readOnly />
          <div className="btn-row btn-row-top">
            <button
              className="btn"
              onClick={() => enterEditMode(activeContent)}
              disabled={!hasPushPermission}
              title={!hasPushPermission ? t('workloads.config.permission.push_blocked') : ''}
              aria-describedby={!hasPushPermission ? 'config-permission-note' : undefined}
            >
              {t('common.edit')}
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

function humanizePlanReason(reason: string) {
  switch (reason) {
    case 'read_only':
      return 'Read-only'
    case 'validation_failed':
      return 'Validation failed'
    case 'all_targets_excluded':
      return 'All targets excluded'
    case 'empty_config':
      return 'Empty config'
    case 'non_collector':
      return 'Non-collector target'
    default:
      return reason
        .split('_')
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(' ')
  }
}

function downloadBlob(blob: Blob, filename: string) {
  const url = window.URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  window.URL.revokeObjectURL(url)
}

function humanizeCheckId(id: string) {
  return id
    .split('_')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')
}
