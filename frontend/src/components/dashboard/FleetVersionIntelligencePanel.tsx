import { useTranslation } from 'react-i18next'
import type {
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

function recommendationByAction(
  recommendations: FleetVersionRecommendation[],
  action: FleetVersionRecommendationAction,
  workloadId: string,
  component?: string,
) {
  return recommendations.find(
    (r) =>
      r.action === action &&
      r.workload_id === workloadId &&
      (!component || !r.components || r.components.includes(component)),
  )
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

export default function FleetVersionIntelligencePanel({ intelligence, isLoading, isError }: Props) {
  const { t } = useTranslation()

  const matrix = intelligence?.version_matrix ?? []
  const belowRecommended = intelligence?.collectors_below_recommended ?? []
  const unsupportedComponents = intelligence?.unsupported_config_components ?? []
  const recommendations = intelligence?.recommendations ?? []
  const recommendedVersion = intelligence?.recommended_version || undefined
  const recommendedLabel =
    recommendedVersion ?? t('dashboard.version_intelligence.no_recommendation')
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
      ) : matrix.length === 0 ? (
        <div className="versions-empty">{t('dashboard.version_intelligence.empty')}</div>
      ) : (
        <div className="version-intelligence-stack">
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
                    {row.type} · {row.status}
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
                      <p>{upgrade?.reason ?? t('dashboard.version_intelligence.below_reason')}</p>
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

function UnsupportedComponentFinding({
  component,
  recommendations,
}: {
  component: FleetUnsupportedComponentFinding
  recommendations: FleetVersionRecommendation[]
}) {
  const { t } = useTranslation()
  const chooseOlder = recommendationByAction(
    recommendations,
    'choose_older_config',
    component.workload_id,
    component.component_type,
  )
  const removeComponent = recommendationByAction(
    recommendations,
    'remove_component',
    component.workload_id,
    component.component_type,
  )

  return (
    <article className="version-intelligence-finding version-intelligence-finding-warning">
      <div>
        <div className="version-intelligence-finding-title">
          {t('dashboard.version_intelligence.unsupported_title', {
            path: component.path,
            component: component.component_type,
          })}
        </div>
        <p>
          {chooseOlder?.reason ??
            removeComponent?.reason ??
            t('dashboard.version_intelligence.unsupported_reason')}
        </p>
      </div>
      <span className="version-intelligence-action">
        {t('dashboard.version_intelligence.action.unsupported_component', {
          component: component.component_type,
        })}
      </span>
    </article>
  )
}
