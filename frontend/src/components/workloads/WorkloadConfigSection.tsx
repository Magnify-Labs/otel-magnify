import { useEffect, useState } from 'react'
import axios from 'axios'
import { useQuery, useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configsAPI, pushesAPI, workloadsAPI } from '../../api/client'
import { DOCS_BASE_URL } from '../../constants'
import YamlEditor from '../config/YamlEditor'
import PushStatusBanner from './PushStatusBanner'
import ConfigDiffView from './ConfigDiffView'
import ConfigPolicyPanel from './ConfigPolicyPanel'
import PushHistoryTable from './PushHistoryTable'
import ConfigSafetySection from './ConfigSafetySection'
import GuidedRollbackDialog from './GuidedRollbackDialog'
import ManualCanaryPanel from './ManualCanaryPanel'
import { useStore } from '../../store'
import { hasPerm } from '../../lib/perm'
import { isReadOnlyCollector } from '../../lib/workloadCapabilities'
import { buildSafeOTelDiffContext } from '../../lib/blastRadiusDisplay'
import { useCapability } from '../../hooks/useCapability'
import type {
  PushGroup,
  PushGroupSelector,
  PushPreview,
  PushPreviewRequest,
  ValidationCheck,
  Config,
  ConfigApplicationPlan,
  ConfigRiskScore,
  ConfigApprovalRequest,
  GitImportConfigRequest,
  GitOpsExportRequest,
  GitOpsExportResponse,
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

const emptyGitImportForm: GitImportConfigRequest = {
  name: '',
  git_url: '',
  git_ref: '',
  git_path: '',
}

const emptyGitExportForm: GitOpsExportRequest = {
  provider: 'github',
  repository: '',
  path: '',
  base_branch: 'main',
  branch: '',
  title: '',
  body: '',
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

function isForbiddenError(err: unknown) {
  return axios.isAxiosError(err) && err.response?.status === 403
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

function contentIsAvailable(row?: Pick<WorkloadConfig, 'content' | 'content_available'> | null) {
  if (!row) return false
  return row.content_available ?? !!row.content
}

function isGitConfig(config?: Config | null): config is Config {
  return config?.source_type === 'git'
}

function displayGitURL(raw?: string): string {
  if (!raw) return '—'
  try {
    const parsed = new URL(raw)
    parsed.username = ''
    parsed.password = ''
    return parsed.toString()
  } catch {
    return raw.replace(/^(https?:\/\/)[^/@\s]+@/i, '$1')
  }
}

function shortCommit(sha?: string): string {
  return sha ? sha.substring(0, 8) : '—'
}

function gitExportFormReady(form: GitOpsExportRequest): boolean {
  return (
    !!form.provider &&
    !!form.repository.trim() &&
    !!form.path.trim() &&
    !!form.base_branch.trim() &&
    !!form.branch.trim() &&
    !!form.title.trim()
  )
}

interface RecoveryPanelProps {
  history: WorkloadConfig[]
  knownGood?: WorkloadKnownGoodConfig
  knownGoodMissing: boolean
  knownGoodError?: unknown
  loading: boolean
  canRollback: boolean
  featureDisabledReason?: string
  onRollback: (target: WorkloadConfig) => void
}

function ConfigProvenanceCard({ config }: { config?: Config | null }) {
  const { t } = useTranslation()
  if (!isGitConfig(config)) return null
  const sanitizedURL = displayGitURL(config.git_url)
  return (
    <section
      className="config-provenance-card"
      aria-label={t('workloads.config.git.provenance_title')}
    >
      <div className="config-provenance-header">
        <p className="section-title">{t('workloads.config.git.provenance_title')}</p>
        <span className="status-pill status-applied">{config.git_provider || 'git'}</span>
      </div>
      <dl className="config-provenance-grid">
        <div>
          <dt>{t('workloads.config.git.source_url')}</dt>
          <dd title={sanitizedURL}>{sanitizedURL}</dd>
        </div>
        <div>
          <dt>{t('workloads.config.git.ref')}</dt>
          <dd>{config.git_ref || '—'}</dd>
        </div>
        <div>
          <dt>{t('workloads.config.git.path')}</dt>
          <dd>{config.git_path || '—'}</dd>
        </div>
        <div>
          <dt>{t('workloads.config.git.commit')}</dt>
          <dd>
            <code title={config.commit_sha}>{shortCommit(config.commit_sha)}</code>
          </dd>
        </div>
        <div>
          <dt>{t('workloads.config.git.imported_at')}</dt>
          <dd>{formatDate(config.imported_at)}</dd>
        </div>
      </dl>
    </section>
  )
}

function GitImportPanel({
  open,
  form,
  canImport,
  disabledReason,
  isPending,
  error,
  onToggle,
  onChange,
  onSubmit,
}: {
  open: boolean
  form: GitImportConfigRequest
  canImport: boolean
  disabledReason: string
  isPending: boolean
  error?: string | null
  onToggle: () => void
  onChange: (next: GitImportConfigRequest) => void
  onSubmit: () => void
}) {
  const { t } = useTranslation()
  const formReady = !!form.name.trim() && !!form.git_url.trim() && !!form.git_path.trim()
  if (!open) {
    return (
      <button className="btn" onClick={onToggle} disabled={!canImport} title={disabledReason}>
        {t('workloads.config.git.import_open')}
      </button>
    )
  }
  return (
    <section className="git-import-panel" aria-labelledby="git-import-title">
      <div className="config-provenance-header">
        <p className="section-title" id="git-import-title">
          {t('workloads.config.git.import_title')}
        </p>
        <button className="btn btn-small" onClick={onToggle} disabled={isPending}>
          {t('common.cancel')}
        </button>
      </div>
      <div className="gitops-form-grid">
        <label>
          <span>{t('workloads.config.git.import_name')}</span>
          <input
            value={form.name}
            onChange={(e) => onChange({ ...form, name: e.target.value })}
            aria-label={t('workloads.config.git.import_name')}
          />
        </label>
        <label className="gitops-wide">
          <span>{t('workloads.config.git.url')}</span>
          <input
            value={form.git_url}
            onChange={(e) => onChange({ ...form, git_url: e.target.value })}
            aria-label={t('workloads.config.git.url')}
            placeholder="https://github.com/acme/collectors.git"
          />
        </label>
        <label>
          <span>{t('workloads.config.git.ref')}</span>
          <input
            value={form.git_ref}
            onChange={(e) => onChange({ ...form, git_ref: e.target.value })}
            aria-label={t('workloads.config.git.ref')}
            placeholder="main"
          />
        </label>
        <label>
          <span>{t('workloads.config.git.file_path')}</span>
          <input
            value={form.git_path}
            onChange={(e) => onChange({ ...form, git_path: e.target.value })}
            aria-label={t('workloads.config.git.file_path')}
            placeholder="otel/prod.yaml"
          />
        </label>
      </div>
      {error && <div className="error-text error-text-push">{error}</div>}
      <div className="btn-row">
        <button
          className="btn btn-primary"
          onClick={onSubmit}
          disabled={!canImport || !formReady || isPending}
          title={
            !canImport ? disabledReason : !formReady ? t('workloads.config.git.import_missing') : ''
          }
        >
          {isPending
            ? t('workloads.config.git.importing')
            : t('workloads.config.git.import_submit')}
        </button>
      </div>
    </section>
  )
}

function GitOpsExportPanel({
  config,
  form,
  result,
  isPending,
  disabledReason,
  error,
  onChange,
  onSubmit,
}: {
  config?: Config | null
  form: GitOpsExportRequest
  result?: GitOpsExportResponse | null
  isPending: boolean
  disabledReason: string
  error?: string | null
  onChange: (next: GitOpsExportRequest) => void
  onSubmit: () => void
}) {
  const { t } = useTranslation()
  const canSubmit = !disabledReason && gitExportFormReady(form) && !!config?.id && !isPending
  return (
    <section className="gitops-export-panel" aria-labelledby="gitops-export-title">
      <div className="config-provenance-header">
        <div>
          <p className="section-title" id="gitops-export-title">
            {t('workloads.config.git.export_title')}
          </p>
          <p className="config-recovery-help">{t('workloads.config.git.export_help')}</p>
        </div>
      </div>
      <div className="gitops-form-grid">
        <label>
          <span>{t('workloads.config.git.provider')}</span>
          <select
            className="filter-select"
            value={form.provider}
            onChange={(e) => onChange({ ...form, provider: e.target.value as 'github' | 'gitlab' })}
            aria-label={t('workloads.config.git.provider')}
          >
            <option value="github">GitHub</option>
            <option value="gitlab">GitLab</option>
          </select>
        </label>
        <label>
          <span>{t('workloads.config.git.repository')}</span>
          <input
            value={form.repository}
            onChange={(e) => onChange({ ...form, repository: e.target.value })}
            aria-label={t('workloads.config.git.repository')}
            placeholder="acme/collectors"
          />
        </label>
        <label>
          <span>{t('workloads.config.git.target_path')}</span>
          <input
            value={form.path}
            onChange={(e) => onChange({ ...form, path: e.target.value })}
            aria-label={t('workloads.config.git.target_path')}
          />
        </label>
        <label>
          <span>{t('workloads.config.git.base_branch')}</span>
          <input
            value={form.base_branch}
            onChange={(e) => onChange({ ...form, base_branch: e.target.value })}
            aria-label={t('workloads.config.git.base_branch')}
          />
        </label>
        <label>
          <span>{t('workloads.config.git.export_branch')}</span>
          <input
            value={form.branch}
            onChange={(e) => onChange({ ...form, branch: e.target.value })}
            aria-label={t('workloads.config.git.export_branch')}
          />
        </label>
        <label>
          <span>{t('workloads.config.git.pr_title')}</span>
          <input
            value={form.title}
            onChange={(e) => onChange({ ...form, title: e.target.value })}
            aria-label={t('workloads.config.git.pr_title')}
          />
        </label>
        <label className="gitops-wide">
          <span>{t('workloads.config.git.pr_body')}</span>
          <textarea
            value={form.body}
            onChange={(e) => onChange({ ...form, body: e.target.value })}
            aria-label={t('workloads.config.git.pr_body')}
          />
        </label>
      </div>
      <div className="config-plan-action-status" role="status">
        {disabledReason || t('workloads.config.git.export_ready')}
      </div>
      {error && <div className="error-text error-text-push">{error}</div>}
      {result && (
        <div className="gitops-export-result">
          <a href={result.result.url} target="_blank" rel="noreferrer">
            {t('workloads.config.git.open_pr')}
          </a>
          <code title={result.result.commit_sha}>{shortCommit(result.result.commit_sha)}</code>
        </div>
      )}
      <button
        className="btn btn-primary"
        onClick={onSubmit}
        disabled={!canSubmit}
        title={
          disabledReason ||
          (!gitExportFormReady(form) ? t('workloads.config.git.export_missing') : '')
        }
      >
        {isPending ? t('workloads.config.git.exporting') : t('workloads.config.git.export_submit')}
      </button>
    </section>
  )
}

function ReadOnlyGitComparison({
  config,
  workload,
}: {
  config?: Config | null
  workload: Workload
}) {
  const { t } = useTranslation()
  if (!isGitConfig(config)) return null
  return (
    <section className="git-readonly-comparison" aria-labelledby="git-readonly-title">
      <div className="config-provenance-header">
        <div>
          <p className="section-title" id="git-readonly-title">
            {t('workloads.config.git.readonly_compare_title')}
          </p>
          <p className="config-recovery-help">{t('workloads.config.git.readonly_compare_help')}</p>
        </div>
        <div className="config-state-card">
          <span className="config-state-title">
            {t('workloads.config.git.opamp_effective_hash')}
          </span>
          <code>{shortHash(workload.active_config_hash)}</code>
        </div>
      </div>
      <ConfigProvenanceCard config={config} />
      <ConfigDiffView oldYaml={config.content ?? ''} newYaml={config.content ?? ''} />
    </section>
  )
}

function ConfigRecoveryPanel({
  history,
  knownGood,
  knownGoodMissing,
  knownGoodError,
  loading,
  canRollback,
  featureDisabledReason,
  onRollback,
}: RecoveryPanelProps) {
  const { t } = useTranslation()
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
              label: t('workloads.config.recovery.last_known_good'),
              is_last_known_good: true,
              content_available: knownGood.content_available,
            } satisfies WorkloadConfig)
          : null))
      : null
  const rollbackTarget = knownGoodTarget ?? (previousAvailable ? previous : null)
  const rollbackKind = knownGoodTarget ? 'last_known_good' : previousAvailable ? 'previous' : null
  const rollbackLabel =
    rollbackKind === 'last_known_good'
      ? t('workloads.config.recovery.rollback_last_known_good')
      : t('workloads.config.recovery.rollback_previous')
  const disableReason = featureDisabledReason
    ? featureDisabledReason
    : !canRollback
      ? t('workloads.config.recovery.requires_push_permission')
      : hasKnownGood && !knownGoodAvailable
        ? t('workloads.config.recovery.known_good_unavailable')
        : t('workloads.config.recovery.no_recovery_target')

  return (
    <section
      className="config-recovery-panel"
      role="region"
      aria-label={t('workloads.config.recovery.aria_label')}
    >
      <div className="config-recovery-header">
        <div>
          <p className="section-title">{t('workloads.config.recovery.title')}</p>
          <p className="config-recovery-help">{t('workloads.config.recovery.help')}</p>
          {featureDisabledReason && <p className="config-recovery-help">{featureDisabledReason}</p>}
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
        <div className="loading">{t('workloads.config.recovery.loading')}</div>
      ) : (
        <div className="config-state-grid">
          <ConfigStateCard
            title={t('workloads.config.recovery.current')}
            hash={current?.config_id}
            meta={current?.status ?? t('workloads.config.recovery.no_current_config')}
          />
          <ConfigStateCard
            title={t('workloads.config.recovery.previous')}
            hash={previous?.config_id}
            meta={
              previous ? formatDate(previous.applied_at) : t('workloads.config.recovery.none_yet')
            }
          />
          <ConfigStateCard
            title={t('workloads.config.recovery.last_known_good')}
            hash={knownGoodHash}
            meta={
              hasKnownGood
                ? knownGoodAvailable
                  ? `${knownGood?.marked_by ?? t('workloads.config.recovery.unknown_marker')} · ${formatDate(knownGood?.marked_at)}`
                  : t('workloads.config.recovery.content_unavailable')
                : knownGoodMissing
                  ? t('workloads.config.recovery.last_known_good_none')
                  : t('workloads.config.recovery.not_loaded')
            }
            detail={!hasKnownGood ? t('workloads.config.recovery.previous_fallback') : undefined}
            tone={hasKnownGood && !knownGoodAvailable ? 'danger' : 'default'}
          />
          {failedCandidate && (
            <ConfigStateCard
              title={t('workloads.config.recovery.failed_candidate')}
              hash={failedCandidate.config_id}
              meta={
                failedCandidate.error_message || t('workloads.config.recovery.candidate_failed')
              }
              tone="danger"
            />
          )}
        </div>
      )}
      {!!knownGoodError && !knownGoodMissing && (
        <div className="error-text config-recovery-error">
          {t('workloads.config.recovery.known_good_load_error')}
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
  exportDisabledReason,
  onExport,
}: {
  plan: ConfigApplicationPlan
  isExporting: boolean
  exportStatus: PlanExportStatus
  exportDisabledReason?: string
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

      {plan.risk_score && <ConfigRiskScorePanel riskScore={plan.risk_score} />}

      {plan.hard_failures.length > 0 && (
        <div className="config-plan-failures" role="alert">
          <strong>{t('workloads.config.application_plan.hard_failures')}</strong>
          <ul>
            {plan.hard_failures.map((failure) => (
              <li key={failure}>{humanizePlanReason(failure, t)}</li>
            ))}
          </ul>
        </div>
      )}

      <ConfigPolicyPanel policy={plan.policy} />

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
                    <li key={reason}>{humanizePlanReason(reason, t)}</li>
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
          disabled={isExporting || !!exportDisabledReason}
          title={exportDisabledReason}
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
              : exportDisabledReason
                ? exportDisabledReason
                : hasBackendExport
                  ? t('workloads.config.application_plan.rollout_not_persisted')
                  : t('workloads.config.application_plan.export_fallback')}
        </span>
      </div>
    </section>
  )
}

function ConfigRiskScorePanel({ riskScore }: { riskScore: ConfigRiskScore }) {
  const { t } = useTranslation()
  const severity = normalizeRiskSeverity(riskScore.severity)
  const reasons = riskScore.reasons.map((reason) => safeRiskText(reason)).filter(Boolean)

  return (
    <section
      className={`config-risk-score-panel config-risk-score-panel-${severity}`}
      aria-labelledby="config-risk-score-title"
      role="region"
    >
      <div className="config-risk-score-header">
        <div>
          <p className="section-title" id="config-risk-score-title">
            {t('workloads.config.application_plan.risk_score.title')}
          </p>
          <p className="config-plan-help">
            {t('workloads.config.application_plan.risk_score.help')}
          </p>
        </div>
        <span
          className={`config-risk-score-badge config-risk-score-badge-${severity}`}
          aria-label={t('workloads.config.application_plan.risk_score.badge_aria', {
            severity: t(`workloads.config.application_plan.risk_score.severity.${severity}`),
          })}
        >
          {t('workloads.config.application_plan.risk_score.badge', {
            severity: t(`workloads.config.application_plan.risk_score.severity.${severity}`),
          })}
        </span>
      </div>
      <p className="config-risk-score-targets">
        {t('workloads.config.application_plan.risk_score.applies_to', {
          count: riskScore.applies_to_count,
        })}
      </p>
      {reasons.length > 0 ? (
        <ol
          className="config-risk-score-reasons"
          aria-label={t('workloads.config.application_plan.risk_score.reasons_aria')}
        >
          {reasons.map((reason, index) => (
            <li key={`${reason}:${index}`}>{reason}</li>
          ))}
        </ol>
      ) : (
        <p className="config-risk-score-empty">
          {t('workloads.config.application_plan.risk_score.no_reasons')}
        </p>
      )}
    </section>
  )
}

export default function WorkloadConfigSection({ workload }: Props) {
  const { t } = useTranslation()
  const configStatus = useStore((s) => s.configStatus[workload.id])
  const rollback = useStore((s) => s.lastRollback[workload.id])
  const clearRollback = useStore((s) => s.clearAutoRollback)
  const me = useStore((s) => s.me)
  const canReadConfigContent = hasPerm(me?.groups, 'config:read_content')
  const { enabled: guidedRollbackEnabled, isLoading: guidedRollbackLoading } = useCapability(
    'config_safety.guided_rollback',
  )
  const { enabled: canaryEnabled, isLoading: canaryLoading } = useCapability(
    'config_safety.canary_rollout',
  )
  const { enabled: scopedPushEnabled, isLoading: scopedPushLoading } = useCapability(
    'config_safety.scoped_push',
  )
  const { enabled: approvalsEnabled, isLoading: approvalsLoading } =
    useCapability('config_safety.approvals')
  const { enabled: gitOpsExportEnabled, isLoading: gitOpsExportLoading } = useCapability(
    'config_safety.gitops_export',
  )

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
  const [activeApproval, setActiveApproval] = useState<ConfigApprovalRequest | null>(null)
  const [approvalRequestComment, setApprovalRequestComment] = useState('')
  const [prodApprovalConfirmed, setProdApprovalConfirmed] = useState(false)
  const [approvalComment, setApprovalComment] = useState('')
  const [pushComment, setPushComment] = useState('')
  const [prodImpactConfirmed, setProdImpactConfirmed] = useState(false)
  const [prodSafetyConfirmed, setProdSafetyConfirmed] = useState(false)
  const [breakGlass, setBreakGlass] = useState(false)
  const [breakGlassReason, setBreakGlassReason] = useState('')
  const [gitImportOpen, setGitImportOpen] = useState(false)
  const [gitImportForm, setGitImportForm] = useState<GitImportConfigRequest>(emptyGitImportForm)
  const [gitImportError, setGitImportError] = useState<string | null>(null)
  const [importedGitConfig, setImportedGitConfig] = useState<Config | null>(null)
  const [gitExportForm, setGitExportForm] = useState<GitOpsExportRequest>(emptyGitExportForm)
  const [gitExportResult, setGitExportResult] = useState<GitOpsExportResponse | null>(null)
  const [gitExportError, setGitExportError] = useState<string | null>(null)

  const {
    data: config,
    isLoading,
    isError,
    error: configError,
  } = useQuery({
    queryKey: ['workload-config', workload.active_config_id],
    queryFn: () => configsAPI.get(workload.active_config_id!),
    enabled: workload.type === 'collector' && !!workload.active_config_id && canReadConfigContent,
    retry: (failureCount, err) => !isForbiddenError(err) && failureCount < 3,
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
    enabled: editMode && hasPerm(me?.groups, 'workload:push_config') && scopeMode === 'saved',
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
    enabled: workload.type === 'collector' && guidedRollbackEnabled,
    retry: false,
  })

  const { data: approvalRequests = [] } = useQuery({
    queryKey: ['workload-config-approvals', workload.id],
    queryFn: () => workloadsAPI.listConfigApprovals(workload.id),
    enabled: workload.type === 'collector' && editMode && approvalsEnabled,
    retry: false,
  })

  const { data: knownWorkloads = [] } = useQuery({
    queryKey: ['workloads', 'blast-radius-context'],
    queryFn: () => workloadsAPI.list(),
    enabled: workload.type === 'collector' && editMode && tab === 'diff',
    retry: false,
  })

  const activeConfigContentRestricted = !canReadConfigContent || isForbiddenError(configError)
  const activeConfigLoadFailed = isError && !activeConfigContentRestricted
  const activeContent = canReadConfigContent ? (config?.content ?? '') : ''

  const {
    data: otelDiff,
    isLoading: otelDiffLoading,
    isError: otelDiffUnavailable,
  } = useQuery({
    queryKey: ['otel-config-diff', workload.id, activeContent, draftYaml, knownWorkloads],
    queryFn: () =>
      configsAPI.diff({
        base_yaml: activeContent,
        target_yaml: draftYaml,
        context: buildSafeOTelDiffContext(workload, knownWorkloads, { include_raw_paths: true }),
      }),
    enabled:
      workload.type === 'collector' &&
      editMode &&
      tab === 'diff' &&
      activeContent.length > 0 &&
      draftYaml.length > 0,
    retry: false,
  })

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
        : t('workloads.config.editor.validation_request_failed')
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
        : t('workloads.config.editor.plan_failed')
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

  const requestApprovalMutation = useMutation({
    mutationFn: () =>
      workloadsAPI.requestConfigApproval(workload.id, {
        draft_yaml: draftYaml,
        target_group: 'single',
        target_env: workload.labels.env ?? '',
        comment: approvalRequestComment.trim(),
        prod_confirmation: prodApprovalConfirmed,
      }),
    onSuccess: (approval) => {
      setActiveApproval(approval)
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : t('workloads.config.approval.request_failed')
      setPushError(msg)
    },
  })

  const approveApprovalMutation = useMutation({
    mutationFn: (approval: ConfigApprovalRequest) =>
      workloadsAPI.approveConfigApproval(workload.id, approval.id, approvalComment.trim()),
    onSuccess: (approval) => {
      setActiveApproval(approval)
      setApprovalComment('')
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : t('workloads.config.approval.approve_failed')
      setPushError(msg)
    },
  })

  const pushApprovalMutation = useMutation({
    mutationFn: ({
      approval,
      emergency,
    }: {
      approval: ConfigApprovalRequest
      emergency: boolean
    }) => {
      const reason = breakGlassReason.trim()
      return workloadsAPI.pushConfigApproval(workload.id, approval.id, {
        comment: emergency ? reason : pushComment.trim(),
        prod_double_confirmed: prodImpactConfirmed && prodSafetyConfirmed,
        break_glass: emergency,
        break_glass_reason: emergency ? reason : '',
      })
    },
    onSuccess: (approval) => {
      setActiveApproval(approval)
      setPendingHash(approval.config_hash ?? null)
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : t('workloads.config.approval.push_failed')
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
      enterEditMode(cfg.content ?? '', workload.active_config_id ? 'diff' : 'edit')
      setImportedGitConfig(isGitConfig(cfg) ? cfg : null)
      setSelectedConfigId('')
    },
    onError: (err: unknown) => {
      if (isForbiddenError(err)) {
        setPushError(t('workloads.config.permission.content_restricted'))
        setSelectedConfigId('')
        return
      }
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : 'Failed to load configuration'
      setPushError(`Failed to load configuration: ${msg}`)
      setSelectedConfigId('')
    },
  })

  const gitImportMutation = useMutation({
    mutationFn: () => configsAPI.importFromGit(gitImportForm),
    onSuccess: ({ config: imported, validation: importValidation }) => {
      enterEditMode(imported.content ?? '', workload.active_config_id ? 'diff' : 'edit')
      setImportedGitConfig(imported)
      setValidation(importValidation)
      setGitImportOpen(false)
      setGitImportForm(emptyGitImportForm)
      setGitImportError(null)
      setPushError(null)
    },
    onError: (err: unknown) => {
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : t('workloads.config.git.import_failed')
      setGitImportError(msg)
    },
  })

  const gitExportMutation = useMutation({
    mutationFn: (sourceConfig: Config) => configsAPI.exportToGit(sourceConfig.id, gitExportForm),
    onSuccess: (result) => {
      setGitExportResult(result)
      setGitExportError(null)
    },
    onError: (err: unknown) => {
      const status = axios.isAxiosError(err) ? err.response?.status : undefined
      const msg = axios.isAxiosError(err)
        ? (err.response?.data?.error ?? err.message)
        : t('workloads.config.git.export_failed')
      setGitExportError(
        status === 501 ? t('workloads.config.git.provider_unconfigured', { message: msg }) : msg,
      )
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
    setActiveApproval(null)
    setApprovalRequestComment('')
    setProdApprovalConfirmed(false)
    setApprovalComment('')
    setPushComment('')
    setProdImpactConfirmed(false)
    setProdSafetyConfirmed(false)
    setBreakGlass(false)
    setBreakGlassReason('')
    setImportedGitConfig(null)
    setGitExportResult(null)
    setGitExportError(null)
  }

  function onDraftChange(next: string) {
    setDraftYaml(next)
    if (validation !== null) setValidation(null)
    if (applicationPlan !== null) setApplicationPlan(null)
    if (pushPreview !== null) setPushPreview(null)
    if (activeApproval !== null) setActiveApproval(null)
    if (planExportStatus !== 'idle') setPlanExportStatus('idle')
    if (gitExportResult !== null) setGitExportResult(null)
    if (gitExportError !== null) setGitExportError(null)
  }

  function updateScopeMode(next: PushScopeMode) {
    if (scopeDisabled) {
      setPushError(scopedPushDisabledReason || t('workloads.config.permission.push_blocked'))
      return
    }
    setScopeMode(next)
    setPushPreview(null)
    setPushError(null)
  }

  function updateDynamicSelector(field: keyof DynamicSelectorState, value: string) {
    if (scopeDisabled) {
      setPushError(scopedPushDisabledReason || t('workloads.config.permission.push_blocked'))
      return
    }
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

  function importFromGit() {
    if (!canImportFromGit) {
      setGitImportError(gitImportDisabledReason)
      return
    }
    gitImportMutation.mutate()
  }

  function exportToGit() {
    if (!gitExportSourceConfig || gitExportDisabledReasonForPanel) return
    gitExportMutation.mutate(gitExportSourceConfig)
  }

  function requestApproval() {
    if (!canRequestApproval) return
    requestApprovalMutation.mutate()
  }

  function approveApproval() {
    if (!currentApproval || !canApproveApproval) return
    approveApprovalMutation.mutate(currentApproval)
  }

  function pushApprovedConfig() {
    if (!currentApproval || !canPushApprovedConfig) return
    pushApprovalMutation.mutate({ approval: currentApproval, emergency: false })
  }

  function pushBreakGlassConfig() {
    if (!currentApproval || !canBreakGlassPush) return
    pushApprovalMutation.mutate({ approval: currentApproval, emergency: true })
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
  const guidedRollbackDisabledReason = guidedRollbackLoading
    ? t('workloads.config.recovery.loading')
    : !guidedRollbackEnabled
      ? t('workloads.config.recovery.feature_disabled')
      : ''
  const canaryDisabledReason = canaryLoading
    ? t('workloads.config.canary.feature_loading')
    : !canaryEnabled
      ? t('workloads.config.canary.feature_disabled')
      : ''
  const scopedPushDisabledReason = scopedPushLoading
    ? t('workloads.config.scope.loading')
    : !scopedPushEnabled
      ? t('workloads.config.scope.feature_disabled')
      : ''
  const approvalsDisabledReason = approvalsLoading
    ? t('workloads.config.approval.loading')
    : !approvalsEnabled
      ? t('workloads.config.approval.feature_disabled')
      : ''
  const gitOpsExportDisabledReason = gitOpsExportLoading
    ? t('workloads.config.application_plan.export_loading')
    : !gitOpsExportEnabled
      ? t('workloads.config.application_plan.gitops_export_disabled')
      : ''
  const canImportFromGit = hasPerm(me?.groups, 'config:create') && gitOpsExportEnabled
  const gitImportDisabledReason = gitOpsExportDisabledReason
    ? gitOpsExportDisabledReason
    : !hasPerm(me?.groups, 'config:create')
      ? t('workloads.config.git.import_permission_blocked')
      : ''
  const gitExportSourceConfig = importedGitConfig ?? config ?? null
  const gitExportDisabledReasonForPanel = gitOpsExportDisabledReason
    ? gitOpsExportDisabledReason
    : !hasPushPermission
      ? t('workloads.config.permission.push_blocked')
      : validation === null || !applicationPlan
        ? t('workloads.config.git.export_needs_validation')
        : !validation.valid
          ? t('workloads.config.scope.disabled.fix_validation_push')
          : !applicationPlan.can_push ||
              !applicationPlan.apply_allowed ||
              applicationPlan.hard_failures.length > 0
            ? t('workloads.config.scope.disabled.plan_blocks_push')
            : ''
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
  const currentApproval =
    activeApproval ??
    approvalRequests.find((approval) => approval.status === 'approved') ??
    approvalRequests.find((approval) => approval.status === 'pending') ??
    approvalRequests[0] ??
    null
  const isProdTarget = (currentApproval?.prod_target ?? workload.labels.env === 'prod') === true
  const approvalIsApproved = currentApproval?.status === 'approved'
  const approvalIsPushed = currentApproval?.status === 'pushed'
  const canRequestApproval =
    canPush &&
    !currentApproval &&
    approvalRequestComment.trim().length > 0 &&
    (!isProdTarget || prodApprovalConfirmed) &&
    !requestApprovalMutation.isPending
  const canApproveApproval =
    hasPushPermission &&
    currentApproval?.status === 'pending' &&
    approvalComment.trim().length > 0 &&
    !approveApprovalMutation.isPending
  const prodDoubleConfirmed = !isProdTarget || (prodImpactConfirmed && prodSafetyConfirmed)
  const canPushApprovedConfig =
    hasPushPermission &&
    approvalIsApproved &&
    pushComment.trim().length > 0 &&
    prodDoubleConfirmed &&
    !pushApprovalMutation.isPending
  const canBreakGlassPush =
    hasPushPermission &&
    !!currentApproval &&
    !approvalIsPushed &&
    breakGlass &&
    breakGlassReason.trim().length > 0 &&
    prodDoubleConfirmed &&
    !pushApprovalMutation.isPending
  const canPreview =
    scopedPushEnabled &&
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
  const previewDisabledReason = scopedPushDisabledReason
    ? scopedPushDisabledReason
    : !hasPushPermission
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

  const canRollback = hasPushPermission && canReadConfigContent && guidedRollbackEnabled
  const scopeDisabled = !scopedPushEnabled || !hasPushPermission
  const scopePermissionDescription = scopeDisabled ? 'push-scope-permission-note' : undefined
  const scopePermissionTitle =
    scopedPushDisabledReason ||
    (!hasPushPermission ? t('workloads.config.permission.push_blocked') : '')
  const scopeInputReadOnlyProps = scopeDisabled
    ? {
        title: scopePermissionTitle,
        'aria-describedby': scopePermissionDescription,
      }
    : {}
  const knownGoodMissing = knownGoodIsError && isNotFoundError(knownGoodError)
  const recoveryPanel = (
    <ConfigRecoveryPanel
      history={history}
      knownGood={knownGood}
      knownGoodMissing={knownGoodMissing}
      knownGoodError={knownGoodError}
      loading={historyLoading || (guidedRollbackEnabled && knownGoodLoading)}
      canRollback={canRollback}
      featureDisabledReason={guidedRollbackDisabledReason}
      onRollback={setDefaultRollbackTarget}
    />
  )

  const defaultRollbackDialog =
    canReadConfigContent && defaultRollbackTarget && guidedRollbackEnabled ? (
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
      exportDisabledReason={gitOpsExportDisabledReason}
      onExport={() => exportApplicationPlan(visiblePlan)}
    />
  ) : readOnlyPlanLoading ? (
    <div className="loading">{t('workloads.config.editor.plan_loading')}</div>
  ) : readOnlyPlanError ? (
    <div className="error-text">{t('workloads.config.editor.plan_load_failed')}</div>
  ) : null

  const approvalUnavailablePanel =
    hasPushPermission && !approvalsEnabled && editMode && applicationPlan ? (
      <section className="config-approval-panel" aria-label={t('workloads.config.approval.title')}>
        <p>{approvalsDisabledReason}</p>
      </section>
    ) : null

  const approvalPanel =
    hasPushPermission && approvalsEnabled && editMode && applicationPlan ? (
      <section
        className={`config-approval-panel ${breakGlass ? 'config-approval-panel-break-glass' : ''}`}
        aria-labelledby="config-approval-title"
      >
        <div className="config-approval-header">
          <div>
            <h3 id="config-approval-title">{t('workloads.config.approval.title')}</h3>
            <p>{t('workloads.config.approval.help')}</p>
          </div>
          <span
            className={`config-approval-status config-approval-status-${currentApproval?.status ?? 'draft'}`}
          >
            {approvalStatusLabel(currentApproval, t)}
          </span>
        </div>
        {currentApproval?.approved_by && (
          <p className="config-approval-meta">
            {t('workloads.config.approval.approved_by', { user: currentApproval.approved_by })}
          </p>
        )}
        {approvalIsPushed && (
          <p className="config-approval-meta">
            {currentApproval?.break_glass
              ? t('workloads.config.approval.break_glass_pushed')
              : t('workloads.config.approval.pushed')}
          </p>
        )}
        {!currentApproval && (
          <div className="config-approval-grid">
            <label className="config-approval-wide">
              <span>{t('workloads.config.approval.request_comment')}</span>
              <textarea
                value={approvalRequestComment}
                onChange={(e) => setApprovalRequestComment(e.target.value)}
                aria-label={t('workloads.config.approval.request_comment')}
              />
            </label>
            {isProdTarget && (
              <label className="config-approval-checkbox config-approval-wide">
                <input
                  type="checkbox"
                  checked={prodApprovalConfirmed}
                  onChange={(e) => setProdApprovalConfirmed(e.target.checked)}
                />
                <span>{t('workloads.config.approval.prod_request_ack')}</span>
              </label>
            )}
            <button
              className="btn btn-primary"
              onClick={requestApproval}
              disabled={!canRequestApproval}
            >
              {requestApprovalMutation.isPending
                ? t('workloads.config.approval.requesting')
                : t('workloads.config.approval.request')}
            </button>
          </div>
        )}
        {currentApproval?.status === 'pending' && (
          <div className="config-approval-grid">
            <label className="config-approval-wide">
              <span>{t('workloads.config.approval.approval_comment')}</span>
              <textarea
                value={approvalComment}
                onChange={(e) => setApprovalComment(e.target.value)}
                aria-label={t('workloads.config.approval.approval_comment')}
              />
            </label>
            <button className="btn" onClick={approveApproval} disabled={!canApproveApproval}>
              {approveApprovalMutation.isPending
                ? t('workloads.config.approval.approving')
                : t('workloads.config.approval.approve')}
            </button>
          </div>
        )}
        {currentApproval && !approvalIsPushed && (
          <div className="config-approval-grid config-approval-push-grid">
            <label className="config-approval-checkbox config-approval-wide">
              <input
                type="checkbox"
                checked={breakGlass}
                onChange={(e) => setBreakGlass(e.target.checked)}
              />
              <span>{t('workloads.config.approval.break_glass_toggle')}</span>
            </label>
            {breakGlass && (
              <div className="config-break-glass-warning config-approval-wide" role="alert">
                <strong>{t('workloads.config.approval.break_glass_title')}</strong>
                <span>{t('workloads.config.approval.break_glass_help')}</span>
              </div>
            )}
            {breakGlass ? (
              <label className="config-approval-wide">
                <span>{t('workloads.config.approval.break_glass_reason')}</span>
                <textarea
                  value={breakGlassReason}
                  onChange={(e) => setBreakGlassReason(e.target.value)}
                  aria-label={t('workloads.config.approval.break_glass_reason')}
                />
              </label>
            ) : (
              <label className="config-approval-wide">
                <span>{t('workloads.config.approval.push_comment')}</span>
                <textarea
                  value={pushComment}
                  onChange={(e) => setPushComment(e.target.value)}
                  aria-label={t('workloads.config.approval.push_comment')}
                />
              </label>
            )}
            {isProdTarget && (
              <>
                <label className="config-approval-checkbox config-approval-wide">
                  <input
                    type="checkbox"
                    checked={prodImpactConfirmed}
                    onChange={(e) => setProdImpactConfirmed(e.target.checked)}
                  />
                  <span>{t('workloads.config.approval.prod_impact_ack')}</span>
                </label>
                <label className="config-approval-checkbox config-approval-wide">
                  <input
                    type="checkbox"
                    checked={prodSafetyConfirmed}
                    onChange={(e) => setProdSafetyConfirmed(e.target.checked)}
                  />
                  <span>{t('workloads.config.approval.prod_safety_ack')}</span>
                </label>
              </>
            )}
            {breakGlass ? (
              <button
                className="btn btn-danger"
                onClick={pushBreakGlassConfig}
                disabled={!canBreakGlassPush}
              >
                {t('workloads.config.approval.break_glass_push')}
              </button>
            ) : (
              <button
                className="btn btn-primary"
                onClick={pushApprovedConfig}
                disabled={!canPushApprovedConfig}
              >
                {pushApprovalMutation.isPending
                  ? t('workloads.config.editor.pushing')
                  : t('workloads.config.approval.push_approved')}
              </button>
            )}
          </div>
        )}
      </section>
    ) : null

  const safetySection = (
    <ConfigSafetySection
      workload={workload}
      validation={validation}
      isValidating={validateMutation.isPending}
      activeConfigLoading={isLoading}
      activeConfigError={activeConfigLoadFailed}
      activeConfigRestricted={activeConfigContentRestricted}
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
        <p className="section-title">{t('workloads.config.title')}</p>
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
        <p className="section-title">{t('workloads.config.title')}</p>
        {hasConfig && isLoading ? (
          <div className="loading">{t('workloads.config.editor.loading')}</div>
        ) : hasConfig && activeConfigContentRestricted ? (
          <div className="empty-state">{t('workloads.config.permission.content_restricted')}</div>
        ) : hasConfig && activeConfigLoadFailed ? (
          <div className="error-text">{t('workloads.config.editor.load_failed')}</div>
        ) : hasConfig && isGitConfig(config) ? (
          <ReadOnlyGitComparison config={config} workload={workload} />
        ) : hasConfig ? (
          <YamlEditor value={activeContent} readOnly />
        ) : (
          <div className="empty-state">{t('workloads.config.editor.no_reported_config')}</div>
        )}
        <div className="config-readonly-note">
          {t('workloads.config.readonly_note.before')} <code>opamp</code>{' '}
          {t('workloads.config.readonly_note.after')}
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

  const safeDraftYaml = canReadConfigContent ? draftYaml : ''
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

      {tab === 'edit' && (
        <YamlEditor
          value={safeDraftYaml}
          onChange={hasPushPermission ? onDraftChange : undefined}
          readOnly={!hasPushPermission}
        />
      )}
      {tab === 'diff' && (
        <ConfigDiffView
          oldYaml={activeContent}
          newYaml={safeDraftYaml}
          otelDiff={otelDiff}
          otelDiffLoading={otelDiffLoading}
          otelDiffUnavailable={otelDiffUnavailable}
        />
      )}

      {validation && <ValidationDetails validation={validation} />}

      <ConfigProvenanceCard config={gitExportSourceConfig} />

      <GitOpsExportPanel
        config={gitExportSourceConfig}
        form={gitExportForm}
        result={gitExportResult}
        isPending={gitExportMutation.isPending}
        disabledReason={gitExportDisabledReasonForPanel}
        error={gitExportError}
        onChange={setGitExportForm}
        onSubmit={exportToGit}
      />

      <ManualCanaryPanel
        workloadId={workload.id}
        draftYaml={safeDraftYaml}
        disabled={!!pendingHash || pushMutation.isPending || !canaryEnabled}
        disabledReason={canaryDisabledReason}
        canPush={hasPushPermission && canaryEnabled}
        safetyPlanReady={canPush}
      />

      <section
        className={`push-scope-panel ${scopeDisabled ? 'push-scope-panel-readonly' : ''}`}
        aria-labelledby="push-scope-title"
        aria-describedby={scopePermissionDescription}
      >
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
              disabled={!!pendingHash || scopeDisabled}
              title={scopePermissionTitle}
              aria-describedby={scopePermissionDescription}
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
                  if (scopeDisabled) return
                  setSelectedPushGroupId(e.target.value)
                  setPushPreview(null)
                }}
                disabled={!!pendingHash || scopeDisabled || pushGroupsError}
                title={scopePermissionTitle}
                aria-describedby={scopePermissionDescription}
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
                disabled={!!pendingHash || scopeDisabled}
                {...scopeInputReadOnlyProps}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.namespace')}</span>
              <input
                value={dynamicSelector.namespace}
                onChange={(e) => updateDynamicSelector('namespace', e.target.value)}
                disabled={!!pendingHash || scopeDisabled}
                {...scopeInputReadOnlyProps}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.env')}</span>
              <input
                value={dynamicSelector.env}
                onChange={(e) => updateDynamicSelector('env', e.target.value)}
                disabled={!!pendingHash || scopeDisabled}
                {...scopeInputReadOnlyProps}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.team')}</span>
              <input
                value={dynamicSelector.team}
                onChange={(e) => updateDynamicSelector('team', e.target.value)}
                disabled={!!pendingHash || scopeDisabled}
                {...scopeInputReadOnlyProps}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.workload_type')}</span>
              <input
                value={dynamicSelector.workloadType}
                onChange={(e) => updateDynamicSelector('workloadType', e.target.value)}
                disabled={!!pendingHash || scopeDisabled}
                {...scopeInputReadOnlyProps}
              />
            </label>
            <label>
              <span>{t('workloads.config.scope.field.version')}</span>
              <input
                value={dynamicSelector.version}
                onChange={(e) => updateDynamicSelector('version', e.target.value)}
                placeholder={t('workloads.config.scope.placeholder.version')}
                disabled={!!pendingHash || scopeDisabled}
                {...scopeInputReadOnlyProps}
              />
            </label>
            <label className="push-dynamic-wide">
              <span>{t('workloads.config.scope.field.capabilities')}</span>
              <input
                value={dynamicSelector.capabilities}
                onChange={(e) => updateDynamicSelector('capabilities', e.target.value)}
                placeholder={t('workloads.config.scope.placeholder.capabilities')}
                disabled={!!pendingHash || scopeDisabled}
                {...scopeInputReadOnlyProps}
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
        {scopeDisabled && (
          <div className="push-scope-permission-note" id="push-scope-permission-note" role="note">
            {scopePermissionTitle}
          </div>
        )}
      </section>
      {applicationPlanPanel}
      {approvalUnavailablePanel}
      {approvalPanel}

      {pushError && <div className="error-text error-text-push">{pushError}</div>}

      <div className="btn-row">
        <button
          className="btn"
          onClick={validateConfig}
          disabled={!canValidateConfig || !draftYaml || validateMutation.isPending || !!pendingHash}
          title={validateDisabledReason}
        >
          {validateMutation.isPending
            ? t('workloads.config.editor.validating')
            : t('workloads.config.editor.validate_for_collector')}
        </button>
        <button
          className="btn"
          onClick={previewTargets}
          disabled={!canPreview}
          title={previewDisabledReason}
          aria-describedby={scopeDisabled ? 'push-scope-permission-note' : undefined}
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
          {planMutation.isPending
            ? t('workloads.config.editor.generating_plan')
            : t('workloads.config.editor.generate_safety_plan')}
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

  const gitImportPanel = (
    <GitImportPanel
      open={gitImportOpen}
      form={gitImportForm}
      canImport={canImportFromGit}
      disabledReason={gitImportDisabledReason}
      isPending={gitImportMutation.isPending}
      error={gitImportError}
      onToggle={() => setGitImportOpen((open) => !open)}
      onChange={setGitImportForm}
      onSubmit={importFromGit}
    />
  )

  // ── Collector without active config ──────────────────────────────────────
  if (!workload.active_config_id) {
    return (
      <>
        {safetySection}
        {recoveryPanel}
        {defaultRollbackDialog}
        <p className="section-title">{t('workloads.config.title')}</p>
        {applySelector}
        {permissionNote}
        {gitImportPanel}
        {canReadConfigContent && editMode ? (
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
        <p className="section-title">{t('workloads.config.title')}</p>
        <div className="loading">{t('workloads.config.editor.loading')}</div>
      </>
    )
  }
  if (activeConfigLoadFailed) {
    return (
      <>
        {safetySection}
        {recoveryPanel}
        {defaultRollbackDialog}
        <p className="section-title">{t('workloads.config.title')}</p>
        <div className="error-text">{t('workloads.config.editor.load_failed')}</div>
      </>
    )
  }

  return (
    <>
      {safetySection}
      {recoveryPanel}
      {defaultRollbackDialog}
      <p className="section-title">{t('workloads.config.title')}</p>
      {applySelector}
      {permissionNote}
      {gitImportPanel}

      {!canReadConfigContent || !editMode ? (
        <div>
          {activeConfigContentRestricted ? (
            <div className="empty-state">{t('workloads.config.permission.content_restricted')}</div>
          ) : (
            <>
              <ConfigProvenanceCard config={config} />
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
            </>
          )}
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
  const { t } = useTranslation()
  const errors = validation.errors ?? []
  const warnings = validation.warnings ?? []
  const blockingMessages: ValidationMessage[] = errors.map((error) => ({
    code: error.code,
    severity: 'error',
    message: error.message,
    path: error.path,
    check_id: error.check_id,
  }))
  const checks = validation.checks ?? legacyChecksFromResult(validation, t)
  const runtimeCheck = checks.find((check) => check.id === 'otelcol_runtime')
  const binaryVersion = metadataString(runtimeCheck, 'binary_version')
  const targetVersion =
    validation.target_collector_version ?? metadataString(runtimeCheck, 'target_version')
  const statusLabel = validationStatusLabel(validation, t)

  return (
    <section
      className={`validation-block validation-details ${validation.valid ? 'validation-ok' : 'validation-errors'}`}
      aria-label={t('workloads.config.validation.aria_label')}
    >
      <div className="validation-details-header">
        <div>
          <p className="validation-details-title">{statusLabel}</p>
          {validation.summary && <p className="validation-details-summary">{validation.summary}</p>}
        </div>
        <div
          className="validation-version-row"
          aria-label={t('workloads.config.validation.versions_aria')}
        >
          {targetVersion && <span className="validation-version-pill">Target {targetVersion}</span>}
          {binaryVersion ? (
            <span className="validation-version-pill">otelcol {binaryVersion}</span>
          ) : runtimeCheck ? (
            <span className="validation-version-pill">
              {t('workloads.config.validation.otelcol_unavailable')}
            </span>
          ) : null}
        </div>
      </div>

      {(errors.length > 0 || warnings.length > 0) && (
        <div className="validation-message-groups">
          {errors.length > 0 && (
            <ValidationMessageGroup
              title={t('workloads.config.validation.blocking_errors')}
              tone="error"
              messages={blockingMessages}
            />
          )}
          {warnings.length > 0 && (
            <ValidationMessageGroup
              title={t('workloads.config.validation.warnings')}
              tone="warning"
              messages={warnings}
            />
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
                  {humanizeStatus(check.status, t)}
                </span>
                <span className="validation-required-badge">
                  {check.required
                    ? t('workloads.config.validation.required')
                    : t('workloads.config.validation.advisory')}
                </span>
              </div>
            </div>

            {check.id === 'otelcol_runtime' && (
              <dl className="validation-check-meta">
                {metadataString(check, 'binary_version') && (
                  <>
                    <dt>{t('workloads.config.validation.binary')}</dt>
                    <dd>otelcol {metadataString(check, 'binary_version')}</dd>
                  </>
                )}
                {metadataString(check, 'target_version') && (
                  <>
                    <dt>{t('workloads.config.validation.target')}</dt>
                    <dd>{metadataString(check, 'target_version')}</dd>
                  </>
                )}
                {metadataString(check, 'binary_path') && (
                  <>
                    <dt>{t('workloads.config.validation.path')}</dt>
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

function validationStatusLabel(
  validation: ValidationResult,
  t: ReturnType<typeof useTranslation>['t'],
) {
  if (!validation.valid) return t('workloads.config.validation.failed')
  if (validation.overall_status === 'warning' || (validation.warnings ?? []).length > 0) {
    return t('workloads.config.validation.passed_with_warnings')
  }
  return t('workloads.config.validation.passed')
}

function legacyChecksFromResult(
  validation: ValidationResult,
  t: ReturnType<typeof useTranslation>['t'],
): ValidationCheck[] {
  if (validation.valid) {
    return [
      {
        id: 'legacy_validation',
        label: t('workloads.config.validation.configuration_validation'),
        source: 'server.validation',
        status: 'passed',
        required: true,
        messages: [
          {
            code: 'validation_ok',
            severity: 'info',
            message: t('workloads.config.validation.configuration_valid'),
            check_id: 'legacy_validation',
          },
        ],
      },
    ]
  }
  return [
    {
      id: 'legacy_validation',
      label: t('workloads.config.validation.configuration_validation'),
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

function humanizeStatus(
  status: ValidationCheck['status'],
  t: ReturnType<typeof useTranslation>['t'],
) {
  switch (status) {
    case 'passed':
      return t('workloads.config.validation.status.passed')
    case 'warning':
      return t('workloads.config.validation.status.warning')
    case 'failed':
      return t('workloads.config.validation.status.failed')
    case 'skipped':
      return t('workloads.config.validation.status.skipped')
    default:
      return status
  }
}

function humanizePlanReason(reason: string, t: ReturnType<typeof useTranslation>['t']) {
  switch (reason) {
    case 'read_only':
      return t('workloads.config.apply.reason.read_only')
    case 'validation_failed':
      return t('workloads.config.apply.reason.validation_failed')
    case 'all_targets_excluded':
      return t('workloads.config.apply.reason.all_targets_excluded')
    case 'empty_config':
      return t('workloads.config.apply.reason.empty_config')
    case 'non_collector':
      return t('workloads.config.apply.reason.non_collector_target')
    case 'workload_offline':
      return t('workloads.config.apply.reason.workload_not_connected')
    default:
      return reason
        .split('_')
        .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
        .join(' ')
  }
}

function normalizeRiskSeverity(severity: string): 'none' | 'low' | 'medium' | 'high' {
  if (severity === 'high' || severity === 'medium' || severity === 'low' || severity === 'none') {
    return severity
  }
  return 'low'
}

function safeRiskText(value: string): string {
  return value
    .replace(/(Bearer\s+)[^\s]+/gi, '$1••••masked••••')
    .replace(/([?&](?:token|api_key|apikey|password|secret)=)[^&#\s]+/gi, '$1••••masked••••')
    .replace(/((?:token|api_key|apikey|password|secret)=)[^@\s]+@[^\s]+/gi, '$1••••masked••••')
    .replace(/(https?:\/\/)[^/@\s]+:[^/@\s]+@/gi, '$1••••masked••••@')
    .replace(/(https?:\/\/)[^/@\s:]+@/gi, '$1••••masked••••@')
}

function approvalStatusLabel(
  approval: ConfigApprovalRequest | null,
  t: ReturnType<typeof useTranslation>['t'],
) {
  if (!approval) return t('workloads.config.approval.status.draft')
  if (approval.break_glass && approval.status === 'pushed') {
    return t('workloads.config.approval.status.break_glass')
  }
  switch (approval.status) {
    case 'pending':
      return t('workloads.config.approval.status.pending')
    case 'approved':
      return t('workloads.config.approval.status.approved')
    case 'pushed':
      return t('workloads.config.approval.status.pushed')
    default:
      return approval.status
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
