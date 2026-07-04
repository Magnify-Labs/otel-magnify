import { useTranslation } from 'react-i18next'
import type { ConfigPolicyEvaluation, ConfigPolicyFinding } from '../../types'

interface Props {
  policy?: ConfigPolicyEvaluation | null
  loading?: boolean
  unavailable?: boolean
}

export default function ConfigPolicyPanel({ policy, loading = false, unavailable = false }: Props) {
  const { t } = useTranslation()

  if (loading) {
    return (
      <section className="config-policy-panel otel-impact-panel" aria-busy="true">
        <h2>{t('workloads.config.policy.title')}</h2>
        <p className="otel-impact-muted">{t('workloads.config.policy.loading')}</p>
      </section>
    )
  }

  if (unavailable || !policy) {
    return (
      <section className="config-policy-panel otel-impact-panel otel-impact-panel-unavailable">
        <div className="otel-impact-header">
          <div>
            <h2>{t('workloads.config.policy.title')}</h2>
            <p className="otel-impact-muted">{t('workloads.config.policy.unavailable_body')}</p>
          </div>
          <span className="otel-risk-badge otel-risk-badge-none">
            {t('workloads.config.policy.status.unavailable')}
          </span>
        </div>
      </section>
    )
  }

  const blocked = !policy.allowed || policy.decision === 'block'
  const warned = !blocked && (policy.decision === 'warn' || policy.summary.warn_count > 0)
  const risk = blocked ? 'high' : warned ? 'medium' : 'low'
  const statusKey = blocked ? 'blocked' : warned ? 'warning' : 'allowed'
  const findings = policy.findings ?? []

  return (
    <section className={`config-policy-panel otel-impact-panel otel-impact-panel-${risk}`}>
      <div className="otel-impact-header">
        <div>
          <h2>{t('workloads.config.policy.title')}</h2>
          <p className="otel-impact-muted">{t('workloads.config.policy.helper')}</p>
        </div>
        <span className={`otel-risk-badge otel-risk-badge-${risk}`}>
          {t(`workloads.config.policy.status.${statusKey}`)}
        </span>
      </div>

      <div className="otel-impact-counts" aria-label={t('workloads.config.policy.counts_label')}>
        <ConfigPolicyCountPill
          label={t('workloads.config.policy.count.block')}
          value={policy.summary.block_count}
          risk="high"
        />
        <ConfigPolicyCountPill
          label={t('workloads.config.policy.count.warn')}
          value={policy.summary.warn_count}
          risk="medium"
        />
        <ConfigPolicyCountPill
          label={t('workloads.config.policy.count.pass')}
          value={policy.summary.pass_count}
          risk="low"
        />
      </div>

      <section className="otel-impact-section">
        <h3>{t('workloads.config.policy.findings_title')}</h3>
        {findings.length === 0 ? (
          <p className="otel-impact-empty">{t('workloads.config.policy.empty')}</p>
        ) : (
          <div className="otel-impact-list">
            {findings.map((finding) => (
              <PolicyFindingRow key={`${finding.rule_id}:${finding.path}`} finding={finding} />
            ))}
          </div>
        )}
      </section>
    </section>
  )
}

function ConfigPolicyCountPill({
  label,
  value,
  risk,
}: {
  label: string
  value: number
  risk: 'high' | 'medium' | 'low'
}) {
  return (
    <span className={`otel-count-pill otel-risk-badge-${risk}`}>
      {label}: {value}
    </span>
  )
}

function PolicyFindingRow({ finding }: { finding: ConfigPolicyFinding }) {
  const { t } = useTranslation()
  const risk = finding.decision === 'block' || finding.severity === 'critical' ? 'high' : 'medium'
  const paths =
    finding.paths && finding.paths.length > 0 ? finding.paths : finding.path ? [finding.path] : []
  return (
    <article className={`otel-impact-row otel-impact-row-${risk}`}>
      <span className={`otel-risk-badge otel-risk-badge-${risk}`}>{finding.severity}</span>
      <div>
        <div className="otel-impact-row-heading">
          <strong>{finding.rule_id}</strong>
          <span className="otel-impact-policy-name">{finding.policy_name}</span>
        </div>
        <div className="otel-impact-meta" aria-label={t('workloads.config.policy.edition_label')}>
          <span>{policyPackagingLabel(finding.packaging, finding.tier, t)}</span>
          {finding.environment && <span>{finding.environment}</span>}
          {finding.target_scope && <span>{finding.target_scope}</span>}
        </div>
        <p>{finding.message}</p>
        {finding.remediation && <p>{finding.remediation}</p>}
        <div className="otel-impact-meta">
          {paths.map((path) => (
            <code key={path}>{path}</code>
          ))}
          {finding.rule_code && <code>{finding.rule_code}</code>}
        </div>
      </div>
    </article>
  )
}

function policyPackagingLabel(
  packaging: ConfigPolicyFinding['packaging'],
  tier: ConfigPolicyFinding['tier'],
  t: ReturnType<typeof useTranslation>['t'],
): string {
  const packagingKey = packaging === 'pro' || packaging === 'enterprise' ? packaging : 'community'
  const tierKey =
    tier === 'configurable' || tier === 'tenant_hook' || tier === 'core' ? tier : 'core'
  return t(`workloads.config.policy.packaging.${packagingKey}.${tierKey}`)
}
