import { useEffect, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { MergeView } from '@codemirror/merge'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { basicSetup } from 'codemirror'
import { yaml } from '@codemirror/lang-yaml'
import { signalDeckYaml } from '../config/yamlTheme'
import ConfigPolicyPanel from './ConfigPolicyPanel'
import { buildBlastRadiusDisplaySections } from '../../lib/blastRadiusDisplay'
import type {
  ConfigPolicyEvaluation,
  OTelBlastRadius,
  OTelComponentDiff,
  OTelConfigDiffResponse,
  OTelDiffRisk,
  OTelEndpointDiff,
  OTelPipelineDiff,
  OTelRiskItem,
  OTelSecurityDiff,
} from '../../types'

interface Props {
  oldYaml: string
  newYaml: string
  otelDiff?: OTelConfigDiffResponse
  otelDiffLoading?: boolean
  otelDiffUnavailable?: boolean
  policy?: ConfigPolicyEvaluation
  policyLoading?: boolean
  policyUnavailable?: boolean
}

const riskOrder: OTelDiffRisk[] = ['high', 'medium', 'low', 'none']

export default function ConfigDiffView({
  oldYaml,
  newYaml,
  otelDiff,
  otelDiffLoading = false,
  otelDiffUnavailable = false,
  policy,
  policyLoading = false,
  policyUnavailable = false,
}: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const viewRef = useRef<MergeView | null>(null)
  const { t } = useTranslation()

  useEffect(() => {
    if (!ref.current) return
    const extensions = [
      basicSetup,
      yaml(),
      signalDeckYaml,
      EditorState.readOnly.of(true),
      EditorView.theme({ '&': { height: '400px' } }),
    ]
    const mv = new MergeView({
      a: { doc: oldYaml, extensions },
      b: { doc: newYaml, extensions },
      parent: ref.current,
    })
    viewRef.current = mv
    return () => {
      mv.destroy()
      viewRef.current = null
    }
  }, [oldYaml, newYaml])

  return (
    <div className="config-diff-stack">
      <OtelImpactSummary
        diff={otelDiff}
        loading={otelDiffLoading}
        unavailable={otelDiffUnavailable}
      />
      <ConfigPolicyPanel policy={policy} loading={policyLoading} unavailable={policyUnavailable} />
      <div className="raw-diff-warning" role="note">
        {t('workloads.config.versioning.raw_diff_warning')}
      </div>
      <div ref={ref} className="config-diff-view" />
    </div>
  )
}

function OtelImpactSummary({
  diff,
  loading,
  unavailable,
}: {
  diff?: OTelConfigDiffResponse
  loading: boolean
  unavailable: boolean
}) {
  const { t } = useTranslation()
  const dangerousItems = useMemo(() => getDangerousItems(diff), [diff])
  const samplingItems = useMemo(() => getSamplingItems(diff), [diff])

  if (loading) {
    return (
      <section className="otel-impact-panel" aria-busy="true">
        <h2>{t('workloads.config.versioning.otel.title')}</h2>
        <p className="otel-impact-muted">{t('workloads.config.versioning.otel.loading')}</p>
      </section>
    )
  }

  if (unavailable || !diff) {
    return (
      <section className="otel-impact-panel otel-impact-panel-unavailable">
        <h2>{t('workloads.config.versioning.otel.title')}</h2>
        <p className="otel-impact-muted">{t('workloads.config.versioning.otel.unavailable')}</p>
      </section>
    )
  }

  return (
    <section className={`otel-impact-panel otel-impact-panel-${diff.summary.overall_risk}`}>
      <div className="otel-impact-header">
        <div>
          <h2>{t('workloads.config.versioning.otel.title')}</h2>
          <p className="otel-impact-muted">{t('workloads.config.versioning.otel.helper')}</p>
        </div>
        <RiskBadge risk={diff.summary.overall_risk} />
      </div>
      <p className="otel-impact-headline">{safeText(diff.summary.headline)}</p>
      <div
        className="otel-impact-counts"
        aria-label={t('workloads.config.versioning.otel.counts_label')}
      >
        <CountPill label="High" value={diff.summary.counts.high_risk} risk="high" />
        <CountPill label="Medium" value={diff.summary.counts.medium_risk} risk="medium" />
        <CountPill label="Low" value={diff.summary.counts.low_risk} risk="low" />
      </div>

      <BlastRadiusSummary radius={diff.blast_radius} />

      <OtelSection
        title={t('workloads.config.versioning.otel.dangerous')}
        empty={t('workloads.config.versioning.otel.empty_dangerous')}
      >
        {dangerousItems.map((item) => (
          <RiskItemRow key={item.id} item={item} />
        ))}
      </OtelSection>

      <OtelSection
        title={t('workloads.config.versioning.otel.components')}
        empty={t('workloads.config.versioning.otel.empty_components')}
      >
        {diff.components.map((component) => (
          <ComponentRow key={component.id} component={component} />
        ))}
      </OtelSection>

      <OtelSection
        title={t('workloads.config.versioning.otel.pipelines')}
        empty={t('workloads.config.versioning.otel.empty_pipelines')}
      >
        {diff.pipelines.map((pipeline) => (
          <PipelineRow key={pipeline.id} pipeline={pipeline} />
        ))}
      </OtelSection>

      <OtelSection
        title={t('workloads.config.versioning.otel.endpoints')}
        empty={t('workloads.config.versioning.otel.empty_endpoints')}
      >
        {diff.endpoints.map((endpoint) => (
          <EndpointRow key={endpoint.id} endpoint={endpoint} />
        ))}
      </OtelSection>

      <OtelSection
        title={t('workloads.config.versioning.otel.sampling')}
        empty={t('workloads.config.versioning.otel.empty_sampling')}
      >
        {samplingItems.map((item) => (
          <RiskItemRow key={item.id} item={item} />
        ))}
      </OtelSection>

      <OtelSection
        title={t('workloads.config.versioning.otel.auth')}
        empty={t('workloads.config.versioning.otel.empty_auth')}
      >
        {diff.security.map((item) => (
          <SecurityRow key={item.id} item={item} />
        ))}
      </OtelSection>

      {diff.diagnostics.length > 0 && (
        <OtelSection title={t('workloads.config.versioning.otel.diagnostics')}>
          {diff.diagnostics.map((diagnostic) => (
            <div
              key={`${diagnostic.side}:${diagnostic.path}:${diagnostic.code}`}
              className="otel-impact-row"
            >
              <RiskBadge
                risk={
                  diagnostic.severity === 'error'
                    ? 'high'
                    : diagnostic.severity === 'warning'
                      ? 'medium'
                      : 'low'
                }
              />
              <span>{safeText(diagnostic.message)}</span>
              {diagnostic.path && <code>{safeText(diagnostic.path)}</code>}
            </div>
          ))}
        </OtelSection>
      )}
    </section>
  )
}

function BlastRadiusSummary({ radius }: { radius?: OTelBlastRadius }) {
  const { t } = useTranslation()
  const sections = buildBlastRadiusDisplaySections(
    radius,
    {
      impactedServices: t('workloads.config.versioning.blast_radius.impacted_services'),
      impactedClusters: t('workloads.config.versioning.blast_radius.impacted_clusters'),
      affectedSignals: t('workloads.config.versioning.blast_radius.affected_signals'),
      touchedExporters: t('workloads.config.versioning.blast_radius.touched_exporters'),
      criticalCollectors: t('workloads.config.versioning.blast_radius.critical_collectors'),
    },
    {
      impactedServices: t('workloads.config.versioning.blast_radius.empty_services'),
      impactedClusters: t('workloads.config.versioning.blast_radius.empty_clusters'),
      affectedSignals: t('workloads.config.versioning.blast_radius.empty_signals'),
      touchedExporters: t('workloads.config.versioning.blast_radius.empty_exporters'),
      criticalCollectors: t('workloads.config.versioning.blast_radius.empty_collectors'),
    },
  )

  return (
    <section className="otel-impact-section blast-radius-section">
      <div className="blast-radius-header">
        <h3>{t('workloads.config.versioning.blast_radius.title')}</h3>
        <p className="otel-impact-muted">{t('workloads.config.versioning.blast_radius.helper')}</p>
      </div>
      <div className="blast-radius-grid">
        {sections.map((section) => (
          <article className="blast-radius-card" key={section.key}>
            <strong>{section.label}</strong>
            {section.items.length > 0 ? (
              <div className="otel-impact-meta">
                {section.items.map((item, index) => (
                  <code key={`${section.key}:${item}:${index}`}>{safeText(item)}</code>
                ))}
              </div>
            ) : (
              <p className="otel-impact-empty">{section.emptyText}</p>
            )}
          </article>
        ))}
      </div>
    </section>
  )
}

function OtelSection({
  title,
  empty,
  children,
}: {
  title: string
  empty?: string
  children?: React.ReactNode
}) {
  const hasChildren = Array.isArray(children) ? children.length > 0 : Boolean(children)
  return (
    <section className="otel-impact-section">
      <h3>{title}</h3>
      {hasChildren ? (
        <div className="otel-impact-list">{children}</div>
      ) : (
        <p className="otel-impact-empty">{empty}</p>
      )}
    </section>
  )
}

function RiskBadge({ risk }: { risk: OTelDiffRisk }) {
  const label = risk === 'none' ? 'low' : risk
  return (
    <span
      className={`otel-risk-badge otel-risk-badge-${label}`}
      aria-label={`Risk level: ${label}`}
    >
      {label} risk
    </span>
  )
}

function CountPill({ label, value, risk }: { label: string; value: number; risk: OTelDiffRisk }) {
  return (
    <span className={`otel-count-pill otel-risk-badge-${risk}`}>
      <strong>{value}</strong> {label}
    </span>
  )
}

function RiskItemRow({ item }: { item: OTelRiskItem }) {
  return (
    <article className={`otel-impact-row otel-impact-row-${item.risk}`}>
      <RiskBadge risk={item.risk} />
      <div>
        <strong>{safeText(item.title)}</strong>
        <p>{safeText(item.description)}</p>
        <MetaList values={[...item.affected_pipelines, ...item.affected_paths]} />
      </div>
    </article>
  )
}

function ComponentRow({ component }: { component: OTelComponentDiff }) {
  return (
    <article className={`otel-impact-row otel-impact-row-${component.risk}`}>
      <RiskBadge risk={component.risk} />
      <div>
        <strong>{safeText(component.title)}</strong>
        <p>
          {component.kind} · {component.component.category} ·{' '}
          <code>{safeText(component.component.id)}</code>
        </p>
        <MetaList
          values={[component.component.path, ...component.impacted_pipelines, ...component.rules]}
        />
      </div>
    </article>
  )
}

function PipelineRow({ pipeline }: { pipeline: OTelPipelineDiff }) {
  return (
    <article className={`otel-impact-row otel-impact-row-${pipeline.risk}`}>
      <RiskBadge risk={pipeline.risk} />
      <div>
        <strong>{safeText(pipeline.pipeline_key)}</strong>
        <p>
          {pipeline.kind} · {pipeline.signal}
        </p>
        <MetaList
          values={[
            ...pipeline.component_ref_changes.map(
              (change) => `${change.kind} ${change.section}: ${change.component_id}`,
            ),
            ...pipeline.rules,
          ]}
        />
      </div>
    </article>
  )
}

function EndpointRow({ endpoint }: { endpoint: OTelEndpointDiff }) {
  return (
    <article className={`otel-impact-row otel-impact-row-${endpoint.risk}`}>
      <RiskBadge risk={endpoint.risk} />
      <div>
        <strong>{safeText(endpoint.component.id)}</strong>
        <p>
          {endpoint.kind} · {safeText(endpoint.field_path)}
        </p>
        <MetaList
          values={[
            endpoint.before?.normalized ?? endpoint.before?.raw,
            '→',
            endpoint.after?.normalized ?? endpoint.after?.raw,
            ...endpoint.rules,
          ]}
        />
      </div>
    </article>
  )
}

function SecurityRow({ item }: { item: OTelSecurityDiff }) {
  const displayValues = [displaySafeValue(item.before), '→', displaySafeValue(item.after)].filter(
    Boolean,
  )
  return (
    <article className={`otel-impact-row otel-impact-row-${item.risk}`}>
      <RiskBadge risk={item.risk} />
      <div>
        <strong>{safeText(item.message)}</strong>
        <p>
          {item.kind} · {item.field} · {safeText(item.path)}
        </p>
        <MetaList values={[...displayValues, ...item.rules]} />
      </div>
    </article>
  )
}

function MetaList({ values }: { values: Array<string | undefined> }) {
  const safeValues = values.map((value) => safeText(value)).filter(Boolean)
  if (safeValues.length === 0) return null
  return (
    <div className="otel-impact-meta">
      {safeValues.map((value, index) => (
        <code key={`${value}:${index}`}>{value}</code>
      ))}
    </div>
  )
}

function getDangerousItems(diff?: OTelConfigDiffResponse): OTelRiskItem[] {
  if (!diff) return []
  return [...diff.risk_items]
    .filter(
      (item) =>
        item.risk === 'high' ||
        /removed|weakened|auth|endpoint|pipeline|memory_limiter|batch/i.test(item.rule),
    )
    .sort((a, b) => riskOrder.indexOf(a.risk) - riskOrder.indexOf(b.risk))
}

function getSamplingItems(diff?: OTelConfigDiffResponse): OTelRiskItem[] {
  if (!diff) return []
  return diff.risk_items.filter((item) =>
    /sampling|tail_sampling|probabilistic/i.test(`${item.rule} ${item.title}`),
  )
}

function displaySafeValue(value: unknown): string | undefined {
  if (!value || typeof value !== 'object') return safeText(value)
  const record = value as Record<string, unknown>
  if (record.redaction_state === 'redacted' || record.kind === 'redacted') {
    return typeof record.display === 'string' ? record.display : '••••masked••••'
  }
  if (record.kind === 'placeholder' && typeof record.value === 'string') return record.value
  if (record.redaction_state && record.redaction_state !== 'not_sensitive') return '••••masked••••'
  if (typeof record.display === 'string') return safeText(record.display)
  if (typeof record.value === 'string') return safeText(record.value)
  return undefined
}

function safeText(value: unknown): string {
  if (typeof value !== 'string') return value == null ? '' : String(value)
  return value
    .replace(/(Bearer\s+)[^\s]+/gi, '$1••••masked••••')
    .replace(/([?&](?:token|api_key|apikey|password|secret)=)[^&#]+/gi, '$1••••masked••••')
}
