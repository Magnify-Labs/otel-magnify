import { useMemo, useState } from 'react'
import axios from 'axios'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { workloadsAPI } from '../../api/client'
import type { CanarySelection, CanaryStatus, CanaryValidationResult, Instance } from '../../types'

interface Props {
  workloadId: string
  draftYaml: string
  disabled: boolean
  disabledReason?: string
  canPush: boolean
  safetyPlanReady: boolean
}

type Strategy = CanarySelection['strategy']

const TERMINAL_STATUSES = new Set(['promoted', 'aborted', 'rollback_started', 'failed'])

function shortHash(hash?: string) {
  return hash ? hash.substring(0, 8) : '—'
}

function elapsedLabel(start?: string, end?: string) {
  if (!start) return '—'
  const startMs = new Date(start).getTime()
  const endMs = end ? new Date(end).getTime() : Date.now()
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) return '—'
  const seconds = Math.max(0, Math.round((endMs - startMs) / 1000))
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

function labelsFromInput(input: string) {
  return input
    .split('\n')
    .map((row) => row.trim())
    .filter(Boolean)
    .reduce<Record<string, string>>((acc, row) => {
      const [key, ...rest] = row.split('=')
      if (key?.trim() && rest.length > 0) {
        acc[key.trim()] = rest.join('=').trim()
      }
      return acc
    }, {})
}

function apiErrorPayload(err: unknown): CanaryValidationResult | null {
  if (axios.isAxiosError(err) && err.response?.data && typeof err.response.data === 'object') {
    return err.response.data as CanaryValidationResult
  }
  return null
}

function useCanaryActionMutation(
  fn: () => Promise<CanaryStatus>,
  fallback: string,
  onStatus: (status: CanaryStatus) => void,
  onErrorText: (message: string) => void,
) {
  return useMutation({
    mutationFn: fn,
    onSuccess: (result) => {
      onStatus(result)
      onErrorText('')
    },
    onError: (err: unknown) => onErrorText(errorText(err, fallback)),
  })
}

export default function ManualCanaryPanel({
  workloadId,
  draftYaml,
  disabled,
  disabledReason,
  canPush,
  safetyPlanReady,
}: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [strategy, setStrategy] = useState<Strategy>('one')
  const [instanceUid, setInstanceUid] = useState('')
  const [count, setCount] = useState(1)
  const [percentage, setPercentage] = useState(10)
  const [labels, setLabels] = useState('')
  const [validation, setValidation] = useState<CanaryValidationResult | null>(null)
  const [validatedFor, setValidatedFor] = useState('')
  const [canary, setCanary] = useState<CanaryStatus | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const { data: topology } = useQuery({
    queryKey: ['workload-topology', workloadId],
    queryFn: () => workloadsAPI.topology(workloadId),
    enabled: open,
  })
  const instanceOptions = useMemo(
    () => buildInstanceOptions(topology?.instances ?? []),
    [topology?.instances],
  )
  const firstEligibleInstance = instanceOptions.find((option) => !option.disabled)?.instance
    .instance_uid
  const selectedOption = instanceOptions.find(
    (option) => option.instance.instance_uid === instanceUid,
  )
  const selectedInstance =
    selectedOption && !selectedOption.disabled ? instanceUid : firstEligibleInstance || ''
  const selection = useMemo<CanarySelection>(() => {
    switch (strategy) {
      case 'one':
        return { strategy, instance_uid: selectedInstance }
      case 'count':
        return { strategy, count }
      case 'percentage':
        return { strategy, percentage }
      case 'label_selector':
        return { strategy, labels: labelsFromInput(labels) }
      default:
        return { strategy: 'one', instance_uid: selectedInstance }
    }
  }, [count, labels, percentage, selectedInstance, strategy])
  const validationKey = useMemo(
    () => JSON.stringify({ draftYaml, selection }),
    [draftYaml, selection],
  )
  const currentValidation = validatedFor === validationKey ? validation : null

  const validateMutation = useMutation({
    mutationFn: () => workloadsAPI.validateCanary(workloadId, draftYaml, selection),
    onSuccess: (result) => {
      setValidation(result)
      setValidatedFor(validationKey)
      setActionError(null)
    },
    onError: (err: unknown) => {
      setValidation(apiErrorPayload(err))
      setValidatedFor(validationKey)
      setActionError(null)
    },
  })

  const startMutation = useMutation({
    mutationFn: () => workloadsAPI.startCanary(workloadId, draftYaml, selection),
    onSuccess: (result) => {
      setCanary(result)
      setActionError(null)
    },
    onError: (err: unknown) =>
      setActionError(errorText(err, t('workloads.config.canary.errors.start'))),
  })

  const statusQuery = useQuery({
    queryKey: ['workload-canary-status', workloadId, canary?.id],
    queryFn: () => workloadsAPI.getCanary(workloadId, canary!.id),
    enabled: !!canary?.id && canary.status === 'running',
    refetchInterval: canary?.status === 'running' ? 5000 : false,
  })

  const status = statusQuery.data ?? canary
  const promoteMutation = useCanaryActionMutation(
    () => workloadsAPI.promoteCanary(workloadId, status!.id),
    t('workloads.config.canary.errors.action'),
    setCanary,
    (message) => setActionError(message || null),
  )
  const abortMutation = useCanaryActionMutation(
    () => workloadsAPI.abortCanary(workloadId, status!.id),
    t('workloads.config.canary.errors.action'),
    setCanary,
    (message) => setActionError(message || null),
  )
  const rollbackMutation = useCanaryActionMutation(
    () => workloadsAPI.rollbackCanary(workloadId, status!.id),
    t('workloads.config.canary.errors.action'),
    setCanary,
    (message) => setActionError(message || null),
  )

  const canaryBlocked = !canPush || !safetyPlanReady
  const oneInstanceUnavailable = strategy === 'one' && !selectedInstance
  const canSubmit =
    !canaryBlocked &&
    !oneInstanceUnavailable &&
    !!currentValidation?.valid &&
    !startMutation.isPending
  const actionDisabled = !canPush || !status
  const promoteDisabled =
    actionDisabled ||
    status.status !== 'succeeded' ||
    status.counts.failed > 0 ||
    (status.stop_reasons?.length ?? 0) > 0 ||
    promoteMutation.isPending
  const abortDisabled =
    actionDisabled || TERMINAL_STATUSES.has(status.status) || abortMutation.isPending
  const rollbackDisabled =
    actionDisabled || status.status === 'promoted' || rollbackMutation.isPending

  return (
    <section className="canary-panel" aria-label={t('workloads.config.canary.title')}>
      <div className="canary-panel-header">
        <div>
          <p className="section-title">{t('workloads.config.canary.title')}</p>
          <p className="canary-help">{t('workloads.config.canary.subtitle')}</p>
        </div>
        <button
          className="btn"
          onClick={() => setOpen((next) => !next)}
          disabled={disabled || !draftYaml || canaryBlocked}
          title={
            disabledReason ||
            (!canPush
              ? t('workloads.config.canary.permission_required')
              : !safetyPlanReady
                ? t('workloads.config.canary.safety_plan_required')
                : undefined)
          }
        >
          {open ? t('workloads.config.canary.close') : t('workloads.config.canary.start')}
        </button>
      </div>

      {open && (
        <div className="canary-wizard">
          <div className="canary-grid">
            <label className="field-label">
              {t('workloads.config.canary.strategy.label')}
              <select
                className="filter-select"
                aria-label={t('workloads.config.canary.strategy.aria')}
                value={strategy}
                onChange={(e) => {
                  setStrategy(e.target.value as Strategy)
                  setValidation(null)
                }}
              >
                <option value="one">{t('workloads.config.canary.strategy.one')}</option>
                <option value="count">{t('workloads.config.canary.strategy.count')}</option>
                <option value="percentage">
                  {t('workloads.config.canary.strategy.percentage')}
                </option>
                <option value="label_selector">
                  {t('workloads.config.canary.strategy.label_selector')}
                </option>
              </select>
            </label>
            {strategy === 'one' && (
              <InstanceSelect
                options={instanceOptions}
                value={selectedInstance}
                onChange={setInstanceUid}
              />
            )}
            {strategy === 'count' && (
              <label className="field-label">
                {t('workloads.config.canary.fields.count')}
                <input
                  className="input"
                  min={1}
                  type="number"
                  value={count}
                  onChange={(e) => {
                    setCount(Number(e.target.value))
                    setValidation(null)
                  }}
                />
              </label>
            )}
            {strategy === 'percentage' && (
              <label className="field-label">
                {t('workloads.config.canary.fields.percentage')}
                <input
                  className="input"
                  aria-label={t('workloads.config.canary.fields.percentage')}
                  max={100}
                  min={1}
                  type="number"
                  value={percentage}
                  onChange={(e) => {
                    setPercentage(Number(e.target.value))
                    setValidation(null)
                  }}
                />
              </label>
            )}
            {strategy === 'label_selector' && (
              <label className="field-label canary-label-selector">
                {t('workloads.config.canary.fields.labels')}
                <textarea
                  className="input"
                  value={labels}
                  placeholder="env=prod"
                  onChange={(e) => {
                    setLabels(e.target.value)
                    setValidation(null)
                  }}
                />
              </label>
            )}
          </div>

          <div className="btn-row">
            <button
              className="btn"
              onClick={() => validateMutation.mutate()}
              disabled={validateMutation.isPending || canaryBlocked || oneInstanceUnavailable}
            >
              {validateMutation.isPending
                ? t('workloads.config.canary.validating')
                : t('workloads.config.canary.validate')}
            </button>
            <button
              className="btn btn-primary"
              onClick={() => startMutation.mutate()}
              disabled={!canSubmit}
            >
              {startMutation.isPending
                ? t('workloads.config.canary.pushing')
                : t('workloads.config.canary.push')}
            </button>
          </div>

          {currentValidation && <CanaryValidationPanel validation={currentValidation} />}
          {actionError && <div className="error-text">{actionError}</div>}
        </div>
      )}

      {status && (
        <CanaryStatusPanel
          status={status}
          promoteDisabled={promoteDisabled}
          abortDisabled={abortDisabled}
          rollbackDisabled={rollbackDisabled}
          onPromote={() => promoteMutation.mutate()}
          onAbort={() => abortMutation.mutate()}
          onRollback={() => rollbackMutation.mutate()}
        />
      )}
    </section>
  )
}

function InstanceSelect({
  options,
  value,
  onChange,
}: {
  options: CanaryInstanceOption[]
  value: string
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  return (
    <label className="field-label">
      {t('workloads.config.canary.fields.instance')}
      <select
        className="filter-select"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-label={t('workloads.config.canary.fields.instance')}
      >
        {options.length === 0 ? (
          <option value="">{t('workloads.config.canary.fields.no_instances')}</option>
        ) : (
          options.map(({ instance, disabled, reason }) => (
            <option key={instance.instance_uid} value={instance.instance_uid} disabled={disabled}>
              {instance.pod_name || instance.instance_uid}
              {disabled && reason
                ? ` — ${t(`workloads.config.canary.ineligible.${reason}`, { defaultValue: reason })}`
                : ''}
            </option>
          ))
        )}
      </select>
    </label>
  )
}

type CanaryIneligibleReason = 'offline' | 'unhealthy' | 'read_only' | 'unsupported' | 'no_status'

interface CanaryInstanceOption {
  instance: Instance
  disabled: boolean
  reason?: CanaryIneligibleReason
}

function buildInstanceOptions(instances: Instance[]): CanaryInstanceOption[] {
  return instances.map((instance) => {
    const reason = canaryIneligibleReason(instance)
    return { instance, disabled: !!reason, reason }
  })
}

function canaryIneligibleReason(instance: Instance): CanaryIneligibleReason | undefined {
  const remoteStatus = instance.remote_config_status?.status as string | undefined
  if (!instance.last_message_at) return 'offline'
  if (!instance.healthy) return 'unhealthy'
  if (instance.accepts_remote_config === false) return 'read_only'
  if (remoteStatus === 'unsupported') return 'unsupported'
  if (!remoteStatus) return 'no_status'
  return undefined
}

function CanaryValidationPanel({ validation }: { validation: CanaryValidationResult }) {
  const { t } = useTranslation()
  const reasons = validation.stop_reasons ?? []
  return (
    <section
      className={`canary-validation-panel ${validation.valid ? 'validation-ok' : 'validation-errors'}`}
      aria-label={t('workloads.config.canary.validation.title')}
    >
      <p className="validation-details-title">
        {validation.valid
          ? t('workloads.config.canary.validation.valid')
          : t('workloads.config.canary.validation.invalid')}
      </p>
      <p>
        {t('workloads.config.canary.validation.target_count', {
          count: validation.targets?.length ?? 0,
        })}
      </p>
      {reasons.length > 0 && (
        <ul className="canary-reason-list">
          {reasons.map((reason) => (
            <li key={reason}>
              {t(`workloads.config.canary.stop_reasons.${reason}`, { defaultValue: reason })}
            </li>
          ))}
        </ul>
      )}
      {(validation.errors ?? []).length > 0 && (
        <ul className="validation-error-list">
          {validation.errors?.map((error) => (
            <li key={error}>{error}</li>
          ))}
        </ul>
      )}
      <CanaryTargetList targets={validation.targets ?? []} />
    </section>
  )
}

function CanaryStatusPanel({
  status,
  promoteDisabled,
  abortDisabled,
  rollbackDisabled,
  onPromote,
  onAbort,
  onRollback,
}: {
  status: CanaryStatus
  promoteDisabled: boolean
  abortDisabled: boolean
  rollbackDisabled: boolean
  onPromote: () => void
  onAbort: () => void
  onRollback: () => void
}) {
  const { t } = useTranslation()
  const reasons = status.stop_reasons ?? []
  return (
    <section className="canary-status-panel" aria-label={t('workloads.config.canary.status.title')}>
      <div className="canary-status-header">
        <div>
          <p className="validation-details-title">
            {t(`workloads.config.canary.statuses.${status.status}`, {
              defaultValue: status.status,
            })}
          </p>
          <p className="canary-help">
            {t('workloads.config.canary.status.hash_elapsed', {
              hash: shortHash(status.config_hash),
              elapsed: elapsedLabel(status.created_at, status.updated_at),
            })}
          </p>
        </div>
        <div className="canary-counts" aria-label={t('workloads.config.canary.status.counts')}>
          <span>
            {t('workloads.config.canary.status.applied', { count: status.counts.applied })}
          </span>
          <span>
            {t('workloads.config.canary.status.applying', { count: status.counts.applying })}
          </span>
          <span>
            {t('workloads.config.canary.status.pending', { count: status.counts.pending })}
          </span>
          <span>{t('workloads.config.canary.status.failed', { count: status.counts.failed })}</span>
        </div>
      </div>
      {reasons.length > 0 && (
        <div className="canary-stop-box">
          <strong>{t('workloads.config.canary.stop_title')}</strong>
          <ul>
            {reasons.map((reason) => (
              <li key={reason}>
                {t(`workloads.config.canary.stop_reasons.${reason}`, { defaultValue: reason })}
              </li>
            ))}
          </ul>
        </div>
      )}
      <CanaryTargetList targets={status.targets} />
      <div className="btn-row">
        <button className="btn btn-primary" disabled={promoteDisabled} onClick={onPromote}>
          {t('workloads.config.canary.actions.promote')}
        </button>
        <button className="btn" disabled={abortDisabled} onClick={onAbort}>
          {t('workloads.config.canary.actions.abort')}
        </button>
        <button className="btn" disabled={rollbackDisabled} onClick={onRollback}>
          {t('workloads.config.canary.actions.rollback')}
        </button>
      </div>
    </section>
  )
}

function CanaryTargetList({ targets }: { targets: CanaryValidationResult['targets'] }) {
  const { t } = useTranslation()
  if (!targets.length) return null
  return (
    <div className="canary-target-list">
      {targets.map((target) => (
        <div className="canary-target-row" key={target.instance_uid}>
          <span>{target.pod_name || target.instance_uid}</span>
          <code>{target.instance_uid}</code>
          <span>
            {t(`workloads.config.canary.target_status.${target.status}`, {
              defaultValue: target.status,
            })}
          </span>
          {target.stop_reason && (
            <span className="error-text">
              {t(`workloads.config.canary.stop_reasons.${target.stop_reason}`, {
                defaultValue: target.stop_reason,
              })}
            </span>
          )}
        </div>
      ))}
    </div>
  )
}

function errorText(err: unknown, fallback: string) {
  if (axios.isAxiosError(err)) {
    const data = err.response?.data
    if (data && typeof data === 'object' && 'error' in data && typeof data.error === 'string') {
      return data.error
    }
    return err.message
  }
  return fallback
}
