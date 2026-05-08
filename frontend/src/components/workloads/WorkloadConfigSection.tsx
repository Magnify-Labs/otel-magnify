import { useEffect, useState } from 'react'
import axios from 'axios'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation } from '@tanstack/react-query'
import { configsAPI, workloadsAPI } from '../../api/client'
import { DOCS_BASE_URL } from '../../constants'
import YamlEditor from '../config/YamlEditor'
import PushStatusBanner from './PushStatusBanner'
import ConfigDiffView from './ConfigDiffView'
import PushHistoryTable from './PushHistoryTable'
import { useStore } from '../../store'
import { isReadOnlyCollector } from '../../lib/workloadCapabilities'
import type { Workload, ValidationResult, ValidationError } from '../../types'

interface Props {
  workload: Workload
}

type Tab = 'edit' | 'diff'

const PUSH_TIMEOUT_MS = 30_000

export default function WorkloadConfigSection({ workload }: Props) {
  const { t } = useTranslation()
  const configStatus = useStore((s) => s.configStatus[workload.id])
  const rollback = useStore((s) => s.lastRollback[workload.id])
  const clearRollback = useStore((s) => s.clearAutoRollback)

  const [editMode, setEditMode] = useState(false)
  const [tab, setTab] = useState<Tab>('edit')
  const [draftYaml, setDraftYaml] = useState('')
  const [pendingHash, setPendingHash] = useState<string | null>(null)
  const [timedOut, setTimedOut] = useState(false)
  const [validation, setValidation] = useState<ValidationResult | null>(null)
  const [pushError, setPushError] = useState<string | null>(null)
  const [selectedConfigId, setSelectedConfigId] = useState('')
  // Operator escape hatch: when the validator wrongly rejects a known-good
  // config, the user can flip Override to push anyway. The backend logs a
  // WARN and emits an audit event with detail=override=true so this can be
  // attributed post-incident. Reset whenever the draft changes.
  const [override, setOverride] = useState(false)

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

  const activeContent = config?.content ?? ''

  const validateMutation = useMutation({
    // Run both validators in parallel:
    //   - workload-scoped: structural shape + AvailableComponents check against
    //     the target agent (knows what's installed there)
    //   - global: otelcol-binary validate on the server (knows component schemas)
    // Both must pass for the result to be considered valid; errors from each
    // are merged so the operator sees the full picture in one panel.
    mutationFn: async () => {
      // The deep validator is purely additive: any failure to reach it
      // (503 unconfigured, network blip, dev environment without the
      // binary) falls back silently to the workload-scoped result. This
      // way the panel never regresses when otelcol-contrib isn't wired
      // in — the user still sees the structural + availability check.
      const [workloadResult, configResult] = await Promise.all([
        workloadsAPI.validateConfig(workload.id, draftYaml),
        configsAPI
          .validate(draftYaml)
          .catch(() => ({ valid: true, errors: [] }) as ValidationResult),
      ])
      const errors: ValidationError[] = [
        ...(workloadResult.errors ?? []),
        ...(configResult.errors ?? []),
      ]
      return {
        valid: workloadResult.valid && configResult.valid && errors.length === 0,
        errors,
      } satisfies ValidationResult
    },
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
    mutationFn: () => workloadsAPI.pushConfig(workload.id, draftYaml, { override }),
    onSuccess: (res) => {
      setPendingHash(res.config_hash)
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
      setTimedOut(false)
      setEditMode(false)
      setDraftYaml('')
      setValidation(null)
      setOverride(false)
    } else if (configStatus.status === 'failed') {
      // keep editMode + draftYaml so the user can fix and retry
      setPendingHash(null)
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
    setOverride(false)
  }

  function cancelEdit() {
    setEditMode(false)
    setDraftYaml('')
    setValidation(null)
    setPushError(null)
    setOverride(false)
  }

  function onDraftChange(next: string) {
    setDraftYaml(next)
    // Any edit invalidates the previous validation pass and any override
    // the user had set: a re-edit can change which lines the safety net
    // would catch, so the operator must re-validate (or re-arm override)
    // before the next push.
    if (validation !== null) setValidation(null)
    if (override) setOverride(false)
  }

  const derivedStatus = pendingHash
    ? {
        status: 'applying' as const,
        config_hash: pendingHash,
        updated_at: new Date().toISOString(),
      }
    : configStatus

  const validationPassed = validation !== null && validation.valid === true
  const canPush =
    !!draftYaml && !pendingHash && !pushMutation.isPending && (validationPassed || override)

  // ── SDK workloads: labels as "configuration" ──────────────────────────────
  if (workload.type === 'sdk') {
    const hasLabels = Object.keys(workload.labels).length > 0
    if (!hasLabels) return null
    return (
      <>
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

      {validation && (
        <div
          className={`validation-block ${validation.valid ? 'validation-ok' : 'validation-errors'}`}
          data-testid="validation-block"
        >
          {validation.valid ? (
            <span>{t('workloads.config.validation.valid')}</span>
          ) : (
            <>
              <span className="validation-errors-title">
                {t('workloads.config.validation.invalid')}
              </span>
              <ul className="validation-error-list">
                {(validation.errors ?? []).map((e, i) => (
                  <li key={`${e.code}:${e.path}:${i}`}>
                    <strong>{e.code}</strong>
                    {e.path ? <code className="validation-error-path">{e.path}</code> : null}
                    <span className="validation-error-msg">— {e.message}</span>
                  </li>
                ))}
              </ul>
            </>
          )}
        </div>
      )}

      {pushError && <div className="error-text error-text-push">{pushError}</div>}

      <div className="btn-row">
        <button
          className="btn"
          onClick={() => validateMutation.mutate()}
          disabled={!draftYaml || validateMutation.isPending || !!pendingHash}
        >
          {validateMutation.isPending
            ? t('workloads.config.validation.loading')
            : t('workloads.config.validation.idle')}
        </button>
        <button
          className="btn btn-primary"
          onClick={() => pushMutation.mutate()}
          disabled={!canPush}
          title={
            validation === null && !override
              ? t('workloads.config.validation.errors.must_validate')
              : !validationPassed && !override
                ? t('workloads.config.validation.errors.fix_first')
                : override
                  ? t('workloads.config.validation.override_active')
                  : ''
          }
          data-testid="push-button"
        >
          {pendingHash
            ? t('workloads.config.applying')
            : pushMutation.isPending
              ? t('workloads.config.pushing')
              : override
                ? t('workloads.config.push_with_override')
                : t('workloads.config.push')}
        </button>
        <button className="btn" onClick={cancelEdit} disabled={!!pendingHash}>
          {t('common.cancel')}
        </button>
        {/* Operator escape hatch — only surfaced when normal push is blocked
            (no validation pass yet, or validation failed). Hidden once the
            user has a green light to avoid muscle-memory misuse. */}
        {!validationPassed && (
          <button
            type="button"
            className="link-button validation-override"
            onClick={() => setOverride((v) => !v)}
            disabled={!!pendingHash}
            data-testid="override-link"
            aria-pressed={override}
          >
            {override
              ? t('workloads.config.validation.override_disable')
              : t('workloads.config.validation.override_enable')}
          </button>
        )}
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
    placeholderLabel = '— No saved configs (create one in Configs) —'
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
        <p className="section-title">Configuration</p>
        <div className="loading">Loading configuration...</div>
      </>
    )
  }
  if (isError) {
    return (
      <>
        <p className="section-title">Configuration</p>
        <div className="error-text">Failed to load configuration</div>
      </>
    )
  }

  return (
    <>
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
        rollback={rollback}
        onDismissRollback={() => clearRollback(workload.id)}
      />

      <PushHistoryTable workloadId={workload.id} />
    </>
  )
}
