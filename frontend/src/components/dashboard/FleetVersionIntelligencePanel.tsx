import { useTranslation } from 'react-i18next'
import type {
  FleetCompatibilityKnownIssue,
  FleetCompatibilityMatrixEntry,
  FleetCompatibilityReason,
  FleetVersionIntelligence,
  FleetVersionMatrixEntry,
  FleetVersionRecommendation,
  FleetVersionRecommendationAction,
  FleetUnsupportedComponentFinding,
  FleetVersionStatus,
} from '../../types'

interface Props {
  intelligence?: FleetVersionIntelligence
  isLoading?: boolean
  isError?: boolean
}

const STATUS_CLASS: Record<FleetVersionStatus, string> = {
  below_recommended: 'version-status-below',
  at_recommended: 'version-status-ok',
  above_recommended: 'version-status-above',
  unknown: 'version-status-unknown',
  not_applicable: 'version-status-muted',
}

type CompatibilityReasonCategory = 'version' | 'components' | 'opamp' | 'other'

const REASON_CATEGORY_BY_CODE: Record<string, CompatibilityReasonCategory> = {
  known_issue: 'version',
  invalid_version: 'version',
  unknown_version: 'version',
  unsupported_component: 'components',
  remote_config_not_accepted: 'opamp',
}

function recommendationByAction(
  recommendations: FleetVersionRecommendation[],
  action: FleetVersionRecommendationAction,
  workloadId: string,
  component?: string,
  configHash?: string,
) {
  return recommendations.find(
    (r) =>
      r.action === action &&
      r.workload_id === workloadId &&
      (!configHash || !r.config_hash || r.config_hash === configHash) &&
      (!component || !r.components || r.components.includes(component)),
  )
}

function actionItemKey(
  action: FleetVersionRecommendationAction,
  component: FleetUnsupportedComponentFinding,
) {
  return `${component.workload_id}:${component.config_hash}:${component.component_type}:${action}`
}

function normalizeSemver(version: string) {
  const normalized = version.trim().replace(/^v/, '').split('-', 1)[0].split('+', 1)[0]
  const parts = normalized.split('.')
  if (parts.length !== 3 || parts.some((part) => !/^\d+$/.test(part))) return null
  return parts.map(Number)
}

function compareSemver(left: string, right: string) {
  const a = normalizeSemver(left)
  const b = normalizeSemver(right)
  if (!a || !b) return null
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return a[i] - b[i]
  }
  return 0
}

function statusForRow(
  row: FleetVersionMatrixEntry,
  recommendedVersion?: string,
): FleetVersionStatus {
  if (row.version_status) return row.version_status
  if (row.type !== 'collector') return 'not_applicable'
  if (!recommendedVersion) return 'unknown'
  const compared = compareSemver(row.version, recommendedVersion)
  if (compared === null) return 'unknown'
  if (compared < 0) return 'below_recommended'
  if (compared > 0) return 'above_recommended'
  return 'at_recommended'
}

function matrixKey(row: FleetVersionMatrixEntry) {
  return [row.group, row.type, row.status, row.version].join(':')
}

function compatibilityKey(row: FleetCompatibilityMatrixEntry) {
  return [row.workload_id, row.config.hash ?? 'no-config', row.version.reported].join(':')
}

function compatibilityStatusKey(status: string) {
  return ['connected', 'degraded', 'disconnected'].includes(status)
    ? `inventory.filter.status.${status}`
    : 'dashboard.version_intelligence.compatibility.status_unknown'
}

function categorizeReason(reason: FleetCompatibilityReason): CompatibilityReasonCategory {
  return REASON_CATEGORY_BY_CODE[reason.code] ?? 'other'
}

function groupReasonsByCategory(reasons: FleetCompatibilityReason[]) {
  return reasons.reduce<Record<CompatibilityReasonCategory, FleetCompatibilityReason[]>>(
    (groups, reason) => {
      groups[categorizeReason(reason)].push(reason)
      return groups
    },
    { version: [], components: [], opamp: [], other: [] },
  )
}

function compatibilityWarnings(knownIssues: FleetCompatibilityKnownIssue[]) {
  return knownIssues.filter((issue) => issue.severity !== 'blocking')
}

export default function FleetVersionIntelligencePanel({ intelligence, isLoading, isError }: Props) {
  const { t, i18n } = useTranslation()
  const canUseBackendReason =
    i18n.resolvedLanguage?.startsWith('en') ?? i18n.language.startsWith('en')

  const matrix = intelligence?.version_matrix ?? []
  const compatibilityMatrix = intelligence?.compatibility_matrix ?? []
  const compatibilitySummary = intelligence?.compatibility_summary
  const belowRecommended = intelligence?.collectors_below_recommended ?? []
  const unsupportedComponents = intelligence?.unsupported_config_components ?? []
  const recommendations = intelligence?.recommendations ?? []
  const recommendedVersion = intelligence?.recommended_version || undefined
  const recommendedLabel =
    recommendedVersion ?? t('dashboard.version_intelligence.no_recommendation')
  const hasVersionMatrix = matrix.length > 0
  const hasCompatibilityMatrix = Boolean(compatibilitySummary && compatibilityMatrix.length > 0)
  const hasFindings = belowRecommended.length > 0 || unsupportedComponents.length > 0

  return (
    <section
      className="panel version-intelligence-panel"
      aria-labelledby="version-intelligence-title"
    >
      <header className="panel-head">
        <h2 id="version-intelligence-title" className="panel-title">
          {t('dashboard.version_intelligence.title')}
        </h2>
        <span className="panel-hint">
          {t('dashboard.version_intelligence.recommended', { version: recommendedLabel })}
        </span>
      </header>

      {isLoading ? (
        <div className="versions-empty">{t('dashboard.version_intelligence.loading')}</div>
      ) : isError ? (
        <div className="versions-empty">{t('dashboard.version_intelligence.error')}</div>
      ) : !hasVersionMatrix && !hasCompatibilityMatrix ? (
        <div className="versions-empty">{t('dashboard.version_intelligence.empty')}</div>
      ) : (
        <div className="version-intelligence-stack">
          {hasVersionMatrix && (
            <div
              className="version-intelligence-matrix"
              role="table"
              aria-label={t('dashboard.version_intelligence.matrix_label')}
            >
              {matrix.map((row) => {
                const rowStatus = statusForRow(row, recommendedVersion)
                return (
                  <div className="version-intelligence-row" role="row" key={matrixKey(row)}>
                    <span className="version-intelligence-group" role="cell">
                      {row.group}
                    </span>
                    <span className="version-intelligence-meta" role="cell">
                      {t(`inventory.filter.type.${row.type}`)} ·{' '}
                      {t(`inventory.filter.status.${row.status}`)}
                    </span>
                    <span className="version-intelligence-version" role="cell">
                      {row.version}
                    </span>
                    <span
                      className={`version-intelligence-badge ${STATUS_CLASS[rowStatus]}`}
                      role="cell"
                    >
                      {t(`dashboard.version_intelligence.status.${rowStatus}`)}
                    </span>
                    <span className="version-intelligence-count" role="cell">
                      {t('dashboard.version_intelligence.count', { count: row.count })}
                    </span>
                  </div>
                )
              })}
            </div>
          )}

          {compatibilitySummary && compatibilityMatrix.length > 0 && (
            <CompatibilityMatrix
              matrix={compatibilityMatrix}
              notRunnableCount={compatibilitySummary.not_runnable_count}
            />
          )}

          {hasFindings ? (
            <div className="version-intelligence-findings">
              {belowRecommended.map((collector) => {
                const upgrade = recommendationByAction(
                  recommendations,
                  'upgrade_collector',
                  collector.workload_id,
                )
                return (
                  <article className="version-intelligence-finding" key={collector.workload_id}>
                    <div>
                      <div className="version-intelligence-finding-title">
                        {t('dashboard.version_intelligence.below_title', {
                          name: collector.display_name,
                          version: collector.version,
                        })}
                      </div>
                      <p>
                        {canUseBackendReason && upgrade?.reason
                          ? upgrade.reason
                          : t('dashboard.version_intelligence.below_reason')}
                      </p>
                    </div>
                    <span className="version-intelligence-action">
                      {t('dashboard.version_intelligence.action.upgrade_collector', {
                        version: collector.recommended_version,
                      })}
                    </span>
                  </article>
                )
              })}

              {unsupportedComponents.map((component) => (
                <UnsupportedComponentFinding
                  component={component}
                  key={`${component.workload_id}:${component.config_hash}:${component.path}`}
                  recommendations={recommendations}
                />
              ))}
            </div>
          ) : (
            <div className="versions-empty">{t('dashboard.version_intelligence.no_findings')}</div>
          )}
        </div>
      )}
    </section>
  )
}

function CompatibilityMatrix({
  matrix,
  notRunnableCount,
}: {
  matrix: FleetCompatibilityMatrixEntry[]
  notRunnableCount: number
}) {
  const { t, i18n } = useTranslation()
  const canUseBackendReason =
    i18n.resolvedLanguage?.startsWith('en') ?? i18n.language.startsWith('en')
  const categories: CompatibilityReasonCategory[] = ['version', 'components', 'opamp', 'other']

  return (
    <div className="compatibility-matrix-section">
      <div className="compatibility-matrix-summary">
        {t('dashboard.version_intelligence.compatibility.summary', {
          count: notRunnableCount,
        })}
      </div>
      <div
        className="compatibility-matrix"
        role="table"
        aria-label={t('dashboard.version_intelligence.compatibility.matrix_label')}
      >
        {matrix.map((row) => {
          const reasonGroups = groupReasonsByCategory(row.blocking_reasons)
          const warnings = compatibilityWarnings(row.known_issues)
          return (
            <article className="compatibility-matrix-row" role="row" key={compatibilityKey(row)}>
              <div className="compatibility-matrix-main" role="cell">
                <div>
                  <div className="compatibility-matrix-name">{row.display_name}</div>
                  <div className="compatibility-matrix-meta">
                    {row.group} · {t(compatibilityStatusKey(row.status))} · {row.version.reported}
                  </div>
                </div>
                <span
                  className={`compatibility-matrix-badge ${
                    row.runnable
                      ? 'compatibility-matrix-badge-ok'
                      : 'compatibility-matrix-badge-blocked'
                  }`}
                >
                  {t(
                    row.runnable
                      ? 'dashboard.version_intelligence.compatibility.can_run'
                      : 'dashboard.version_intelligence.compatibility.cannot_run',
                  )}
                </span>
              </div>

              <div className="compatibility-matrix-hashes" role="cell">
                {row.config.hash && (
                  <span>
                    {t('dashboard.version_intelligence.compatibility.config_hash', {
                      hash: row.config.hash,
                    })}
                  </span>
                )}
                {row.available_components.hash && (
                  <span>
                    {t('dashboard.version_intelligence.compatibility.capabilities_hash', {
                      hash: row.available_components.hash,
                    })}
                  </span>
                )}
              </div>

              <div className="compatibility-matrix-reasons" role="cell">
                {row.blocking_reasons.length === 0 ? (
                  <span className="compatibility-matrix-empty">
                    {t('dashboard.version_intelligence.compatibility.no_blockers')}
                  </span>
                ) : (
                  categories.map((category) =>
                    reasonGroups[category].length > 0 ? (
                      <ReasonCategory
                        canUseBackendReason={canUseBackendReason}
                        category={category}
                        key={`${row.workload_id}:${category}`}
                        reasons={reasonGroups[category]}
                      />
                    ) : null,
                  )
                )}
              </div>

              {warnings.length > 0 && (
                <div className="compatibility-matrix-warnings" role="cell">
                  <div className="compatibility-matrix-category-title">
                    {t('dashboard.version_intelligence.compatibility.warnings')}
                  </div>
                  <ul>
                    {warnings.map((warning) => (
                      <li key={`${warning.code}:${warning.affected_version}`}>{warning.message}</li>
                    ))}
                  </ul>
                </div>
              )}
            </article>
          )
        })}
      </div>
    </div>
  )
}

function ReasonCategory({
  canUseBackendReason,
  category,
  reasons,
}: {
  canUseBackendReason: boolean
  category: CompatibilityReasonCategory
  reasons: FleetCompatibilityReason[]
}) {
  const { t } = useTranslation()
  return (
    <div className="compatibility-matrix-category">
      <div className="compatibility-matrix-category-title">
        {t(`dashboard.version_intelligence.compatibility.category.${category}`)}
      </div>
      <ul>
        {reasons.map((reason) => (
          <li key={`${reason.code}:${reason.message}`}>
            {canUseBackendReason && reason.message
              ? reason.message
              : t(`dashboard.version_intelligence.compatibility.reason.${reason.code}`, {
                  defaultValue: t('dashboard.version_intelligence.compatibility.reason.default'),
                })}
          </li>
        ))}
      </ul>
    </div>
  )
}

function UnsupportedComponentFinding({
  component,
  recommendations,
}: {
  component: FleetUnsupportedComponentFinding
  recommendations: FleetVersionRecommendation[]
}) {
  const { t, i18n } = useTranslation()
  const canUseBackendReason =
    i18n.resolvedLanguage?.startsWith('en') ?? i18n.language.startsWith('en')
  const chooseOlder = recommendationByAction(
    recommendations,
    'choose_older_config',
    component.workload_id,
    component.component_type,
    component.config_hash,
  )
  const removeComponent = recommendationByAction(
    recommendations,
    'remove_component',
    component.workload_id,
    component.component_type,
    component.config_hash,
  )
  const upgradeCollector = recommendationByAction(
    recommendations,
    'upgrade_collector',
    component.workload_id,
    component.component_type,
  )
  const actionItems: Array<{
    action: FleetVersionRecommendationAction
    label: string
    reason: string
  }> = [
    {
      action: 'upgrade_collector',
      label: t('dashboard.version_intelligence.action_label.upgrade_collector'),
      reason:
        canUseBackendReason && upgradeCollector?.reason
          ? upgradeCollector.reason
          : t('dashboard.version_intelligence.recommendation.upgrade_collector'),
    },
    {
      action: 'choose_older_config',
      label: t('dashboard.version_intelligence.action_label.choose_older_config'),
      reason:
        canUseBackendReason && chooseOlder?.reason
          ? chooseOlder.reason
          : t('dashboard.version_intelligence.recommendation.choose_older_config'),
    },
    {
      action: 'remove_component',
      label: t('dashboard.version_intelligence.action_label.remove_component'),
      reason:
        canUseBackendReason && removeComponent?.reason
          ? removeComponent.reason
          : t('dashboard.version_intelligence.recommendation.remove_component', {
              component: component.component_type,
            }),
    },
  ]

  return (
    <article className="version-intelligence-finding version-intelligence-finding-warning">
      <div>
        <div className="version-intelligence-finding-title">
          {t('dashboard.version_intelligence.unsupported_title', {
            path: component.path,
            component: component.component_type,
          })}
        </div>
        <p>{t('dashboard.version_intelligence.unsupported_reason')}</p>
        <ul
          className="version-intelligence-recommendations"
          aria-label={t('dashboard.version_intelligence.recommendations_label')}
        >
          {actionItems.map((item) => (
            <li
              className="version-intelligence-recommendation"
              key={actionItemKey(item.action, component)}
            >
              <span className="version-intelligence-recommendation-label">{item.label}</span>
              <span>{item.reason}</span>
            </li>
          ))}
        </ul>
      </div>
      <span className="version-intelligence-action">
        {t('dashboard.version_intelligence.action.unsupported_component', {
          component: component.component_type,
        })}
      </span>
    </article>
  )
}
