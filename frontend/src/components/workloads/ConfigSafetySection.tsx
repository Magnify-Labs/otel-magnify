import { useTranslation } from 'react-i18next'
import type { AutoRollbackEvent, RemoteConfigStatus, ValidationResult, Workload } from '../../types'
import { isReadOnlyCollector, isSupervised } from '../../lib/workloadCapabilities'

interface Props {
  workload: Workload
  validation: ValidationResult | null
  isValidating: boolean
  activeConfigLoading: boolean
  activeConfigError: boolean
  activeConfigRestricted?: boolean
  pendingHash: string | null
  timedOut: boolean
  configStatus?: RemoteConfigStatus
  rollback?: AutoRollbackEvent
  canPush: boolean
}

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'active'

interface SafetyStep {
  title: string
  body: string
  badge: string
  helper?: string
  tone: Tone
}

export default function ConfigSafetySection({
  workload,
  validation,
  isValidating,
  activeConfigLoading,
  activeConfigError,
  activeConfigRestricted = false,
  pendingHash,
  timedOut,
  configStatus,
  rollback,
  canPush,
}: Props) {
  const { t } = useTranslation()

  if (workload.type !== 'collector') return null

  const readOnly = isReadOnlyCollector(workload)
  const supervised = isSupervised(workload)
  const hasActiveConfig = !!workload.active_config_id
  const visibleStatus = pendingHash
    ? ({
        status: 'applying',
        config_hash: pendingHash,
        updated_at: new Date().toISOString(),
      } as const)
    : configStatus

  const validateStep = buildValidateStep(t, validation, isValidating)
  const compareStep = buildCompareStep(
    t,
    hasActiveConfig,
    activeConfigLoading,
    activeConfigError,
    activeConfigRestricted,
  )
  const pushStep = buildPushStep(t, readOnly, validation, canPush, visibleStatus, timedOut)
  const rollbackStep = buildRollbackStep(t, hasActiveConfig, rollback)
  const steps = [validateStep, compareStep, pushStep, rollbackStep]

  return (
    <section
      className={`config-safety-card ${readOnly ? 'config-safety-card-readonly' : ''}`}
      aria-labelledby="config-safety-title"
    >
      <div className="config-safety-head">
        <div>
          <p className="section-title" id="config-safety-title">
            {t('workloads.config.safety.title')}
          </p>
          <p className="config-safety-subtitle">{t('workloads.config.safety.subtitle')}</p>
        </div>
        <div className="config-safety-flow" aria-label={t('workloads.config.safety.flow_aria')}>
          <span>{t('workloads.config.safety.flow.validate')}</span>
          <span aria-hidden>→</span>
          <span>{t('workloads.config.safety.flow.compare')}</span>
          <span aria-hidden>→</span>
          <span>{t('workloads.config.safety.flow.push')}</span>
          <span aria-hidden>→</span>
          <span>{t('workloads.config.safety.flow.rollback')}</span>
        </div>
      </div>

      {readOnly && (
        <div className="config-safety-readonly-note" role="note">
          <strong>{t('workloads.config.safety.readonly.title')}</strong>
          <span>{t('workloads.config.safety.readonly.body')}</span>
        </div>
      )}

      <ol className="config-safety-steps">
        {steps.map((step, index) => (
          <li className="config-safety-step" key={step.title}>
            <div className="config-safety-step-index" aria-hidden>
              {index + 1}
            </div>
            <div className="config-safety-step-body">
              <div className="config-safety-step-topline">
                <h3 className="config-safety-step-title">{step.title}</h3>
                <span className={`config-safety-badge config-safety-badge-${step.tone}`}>
                  {step.badge}
                </span>
              </div>
              <p>{step.body}</p>
              {step.helper && <p className="config-safety-helper">{step.helper}</p>}
            </div>
          </li>
        ))}
      </ol>

      {supervised && (
        <div className="config-safety-footer">
          <span>{t('workloads.config.safety.footer')}</span>
        </div>
      )}
    </section>
  )
}

function buildValidateStep(
  t: ReturnType<typeof useTranslation>['t'],
  validation: ValidationResult | null,
  isValidating: boolean,
): SafetyStep {
  if (isValidating) {
    return {
      title: t('workloads.config.safety.validate.title'),
      body: t('workloads.config.safety.validate.body'),
      badge: t('workloads.config.safety.validate.badge.loading'),
      tone: 'active',
    }
  }
  if (validation?.valid) {
    return {
      title: t('workloads.config.safety.validate.title'),
      body: t('workloads.config.safety.validate.body'),
      badge: t('workloads.config.safety.validate.badge.success'),
      helper: t('workloads.config.safety.validate.helper.success'),
      tone: 'success',
    }
  }
  if (validation && !validation.valid) {
    return {
      title: t('workloads.config.safety.validate.title'),
      body: t('workloads.config.safety.validate.body'),
      badge: t('workloads.config.safety.validate.badge.error'),
      helper: t('workloads.config.safety.validate.helper.error'),
      tone: 'danger',
    }
  }
  return {
    title: t('workloads.config.safety.validate.title'),
    body: t('workloads.config.safety.validate.body'),
    badge: t('workloads.config.safety.validate.badge.initial'),
    helper: t('workloads.config.safety.validate.helper.initial'),
    tone: 'warning',
  }
}

function buildCompareStep(
  t: ReturnType<typeof useTranslation>['t'],
  hasActiveConfig: boolean,
  activeConfigLoading: boolean,
  activeConfigError: boolean,
  activeConfigRestricted: boolean,
): SafetyStep {
  if (activeConfigLoading) {
    return {
      title: t('workloads.config.safety.compare.title'),
      body: t('workloads.config.safety.compare.body'),
      badge: t('workloads.config.safety.compare.badge.loading'),
      tone: 'active',
    }
  }
  if (activeConfigError) {
    return {
      title: t('workloads.config.safety.compare.title'),
      body: t('workloads.config.safety.compare.body'),
      badge: t('workloads.config.safety.compare.badge.error'),
      helper: t('workloads.config.safety.compare.helper.error'),
      tone: 'danger',
    }
  }
  if (activeConfigRestricted) {
    return {
      title: t('workloads.config.safety.compare.title'),
      body: t('workloads.config.safety.compare.body'),
      badge: t('workloads.config.safety.compare.badge.restricted'),
      helper: t('workloads.config.safety.compare.helper.restricted'),
      tone: 'neutral',
    }
  }
  if (!hasActiveConfig) {
    return {
      title: t('workloads.config.safety.compare.title'),
      body: t('workloads.config.safety.compare.body'),
      badge: t('workloads.config.safety.compare.badge.first'),
      helper: t('workloads.config.safety.compare.helper.first'),
      tone: 'neutral',
    }
  }
  return {
    title: t('workloads.config.safety.compare.title'),
    body: t('workloads.config.safety.compare.body'),
    badge: t('workloads.config.safety.compare.badge.available'),
    helper: t('workloads.config.safety.compare.helper.available'),
    tone: 'success',
  }
}

function buildPushStep(
  t: ReturnType<typeof useTranslation>['t'],
  readOnly: boolean,
  validation: ValidationResult | null,
  canPush: boolean,
  status: RemoteConfigStatus | undefined,
  timedOut: boolean,
): SafetyStep {
  if (readOnly) {
    return {
      title: t('workloads.config.safety.push.title'),
      body: t('workloads.config.safety.push.body'),
      badge: t('workloads.config.safety.push.badge.unavailable'),
      helper: t('workloads.config.safety.push.helper.readonly'),
      tone: 'neutral',
    }
  }
  if (timedOut) {
    return {
      title: t('workloads.config.safety.push.title'),
      body: t('workloads.config.safety.push.body'),
      badge: t('workloads.config.safety.push.badge.applying'),
      helper: t('workloads.config.safety.push.helper.timeout'),
      tone: 'active',
    }
  }
  if (status?.status === 'applying') {
    return {
      title: t('workloads.config.safety.push.title'),
      body: t('workloads.config.safety.push.body'),
      badge: t('workloads.config.safety.push.badge.applying'),
      helper: t('workloads.config.safety.push.helper.applying'),
      tone: 'active',
    }
  }
  if (status?.status === 'failed') {
    return {
      title: t('workloads.config.safety.push.title'),
      body: t('workloads.config.safety.push.body'),
      badge: t('workloads.config.safety.push.badge.failed'),
      helper: t('workloads.config.safety.push.helper.failed'),
      tone: 'danger',
    }
  }
  if (status?.status === 'applied') {
    return {
      title: t('workloads.config.safety.push.title'),
      body: t('workloads.config.safety.push.body'),
      badge: t('workloads.config.safety.push.badge.applied'),
      helper: t('workloads.config.safety.push.helper.applied'),
      tone: 'success',
    }
  }
  return {
    title: t('workloads.config.safety.push.title'),
    body: t('workloads.config.safety.push.body'),
    badge: canPush
      ? t('workloads.config.safety.push.badge.ready')
      : t('workloads.config.safety.push.badge.locked'),
    helper:
      validation?.valid === true
        ? t('workloads.config.safety.push.cta')
        : validation?.valid === false
          ? t('workloads.config.safety.validate.helper.error')
          : t('workloads.config.safety.push.helper.locked'),
    tone: canPush ? 'success' : 'warning',
  }
}

function buildRollbackStep(
  t: ReturnType<typeof useTranslation>['t'],
  hasActiveConfig: boolean,
  rollback?: AutoRollbackEvent,
): SafetyStep {
  if (rollback) {
    return {
      title: t('workloads.config.safety.rollback.title'),
      body: t('workloads.config.safety.rollback.body'),
      badge: t('workloads.config.safety.rollback.badge.auto'),
      helper: t('workloads.config.safety.rollback.helper.auto', {
        hash: rollback.to_hash.slice(0, 12),
      }),
      tone: 'success',
    }
  }
  if (hasActiveConfig) {
    return {
      title: t('workloads.config.safety.rollback.title'),
      body: t('workloads.config.safety.rollback.body'),
      badge: t('workloads.config.safety.rollback.badge.history'),
      helper: t('workloads.config.safety.rollback.helper.history'),
      tone: 'success',
    }
  }
  return {
    title: t('workloads.config.safety.rollback.title'),
    body: t('workloads.config.safety.rollback.body'),
    badge: t('workloads.config.safety.rollback.badge.empty'),
    helper: t('workloads.config.safety.rollback.helper.empty'),
    tone: 'neutral',
  }
}
