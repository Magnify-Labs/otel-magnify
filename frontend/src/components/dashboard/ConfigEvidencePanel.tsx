import { useState, type ReactNode } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configSafetyAPI, getAPIErrorDetails } from '../../api/client'
import { useFeature } from '../../hooks/useFeature'
import { hasPerm } from '../../lib/perm'
import { useStore } from '../../store'
import type {
  EvidenceAuditTrailEntry,
  EvidenceConfigChange,
  EvidenceReport,
  EvidenceReportDownload,
  EvidenceReportDownloadFormat,
  EvidenceRollback,
  EvidenceValidationFailure,
  FleetCollectorVersionFinding,
  ConfigDriftItem,
} from '../../types'

const EXPORT_FORMATS: EvidenceReportDownloadFormat[] = ['markdown', 'csv', 'pdf']

export default function ConfigEvidencePanel({ workloadIds }: { workloadIds: string[] }) {
  const { t } = useTranslation()
  const me = useStore((s) => s.me)
  const { enabled: evidencePackEnabled, isLoading: evidencePackLoading } =
    useFeature('reports.evidence_pack')
  const canExport = hasPerm(me?.groups, 'reports:export')
  const [exportStatus, setExportStatus] = useState<string | null>(null)

  const reportQuery = useQuery({
    queryKey: ['config-safety-evidence-report', workloadIds],
    queryFn: () => configSafetyAPI.report({ workloadIds }),
    enabled: evidencePackEnabled && canExport && workloadIds.length > 0,
    staleTime: 60_000,
    retry: false,
  })

  const exportMutation = useMutation({
    mutationFn: (format: EvidenceReportDownloadFormat) =>
      configSafetyAPI.exportReportDownload(format, { workloadIds }),
    onMutate: () => setExportStatus(null),
    onSuccess: (download) => {
      downloadEvidenceReport(download)
      setExportStatus(t('dashboard.config_safety.evidence.export_ready'))
    },
    onError: (err) => {
      setExportStatus(
        getAPIErrorDetails(err, t('dashboard.config_safety.evidence.export_error')).message,
      )
    },
  })

  if (evidencePackLoading || !evidencePackEnabled || !canExport || workloadIds.length === 0)
    return null

  const report = reportQuery.data
  const empty = report ? isReportEmpty(report) : false

  return (
    <section className="panel config-evidence-panel" aria-labelledby="config-evidence-title">
      <header className="panel-head config-evidence-head">
        <div>
          <h2 className="panel-title" id="config-evidence-title">
            {t('dashboard.config_safety.evidence.title')}
          </h2>
          <p className="panel-hint">{t('dashboard.config_safety.evidence.hint')}</p>
        </div>
        <div
          className="config-evidence-actions"
          aria-label={t('dashboard.config_safety.evidence.exports_aria')}
        >
          {EXPORT_FORMATS.map((format) => (
            <button
              key={format}
              className="btn btn-small"
              type="button"
              onClick={() => exportMutation.mutate(format)}
              disabled={reportQuery.isLoading || exportMutation.isPending || empty}
              title={empty ? t('dashboard.config_safety.evidence.empty') : undefined}
            >
              {exportMutation.isPending && exportMutation.variables === format
                ? t('dashboard.config_safety.evidence.exporting')
                : t(`dashboard.config_safety.evidence.export.${format}`)}
            </button>
          ))}
        </div>
      </header>

      {reportQuery.isLoading ? (
        <p className="config-evidence-muted" aria-busy="true">
          {t('dashboard.config_safety.evidence.loading')}
        </p>
      ) : reportQuery.isError ? (
        <p className="config-evidence-error" role="alert">
          {t('dashboard.config_safety.evidence.error')}
        </p>
      ) : !report || empty ? (
        <p className="config-evidence-muted">{t('dashboard.config_safety.evidence.empty')}</p>
      ) : (
        <EvidenceReportPreview report={report} />
      )}

      {exportStatus && (
        <p
          className={exportMutation.isError ? 'config-evidence-error' : 'config-evidence-success'}
          role="status"
        >
          {exportStatus}
        </p>
      )}
    </section>
  )
}

function EvidenceReportPreview({ report }: { report: EvidenceReport }) {
  const { t } = useTranslation()
  const summaryItems = [
    ['config_changes', report.summary.config_changes],
    ['validation_failures', report.summary.validation_failures],
    ['rollbacks', report.summary.rollbacks],
    ['drifted_collectors', report.summary.drifted_collectors],
    ['outdated_collectors', report.summary.outdated_collectors],
    ['audit_events', report.summary.audit_events],
  ] as const

  return (
    <div className="config-evidence-stack">
      <div className="config-evidence-meta">
        <span>
          {t('dashboard.config_safety.evidence.generated_at', {
            date: formatDate(report.generated_at),
          })}
        </span>
        <code>{report.report_id}</code>
      </div>

      <div
        className="config-evidence-summary"
        aria-label={t('dashboard.config_safety.evidence.summary_aria')}
      >
        {summaryItems.map(([key, value]) => (
          <div className="config-evidence-summary-card" key={key}>
            <span>{t(`dashboard.config_safety.evidence.summary.${key}`)}</span>
            <strong>{value}</strong>
          </div>
        ))}
      </div>

      <PreviewSection
        title={t('dashboard.config_safety.evidence.sections.config_changes')}
        empty={t('dashboard.config_safety.evidence.no_config_changes')}
        items={report.config_changes.slice(0, 3)}
        renderItem={(item) => <ConfigChangeRow item={item} />}
      />
      <PreviewSection
        title={t('dashboard.config_safety.evidence.sections.validation_failures')}
        empty={t('dashboard.config_safety.evidence.no_validation_failures')}
        items={report.validation_failures.slice(0, 3)}
        renderItem={(item) => <ValidationFailureRow item={item} />}
      />
      <PreviewSection
        title={t('dashboard.config_safety.evidence.sections.rollbacks')}
        empty={t('dashboard.config_safety.evidence.no_rollbacks')}
        items={report.rollbacks.slice(0, 3)}
        renderItem={(item) => <RollbackRow item={item} />}
      />
      <PreviewSection
        title={t('dashboard.config_safety.evidence.sections.drift')}
        empty={t('dashboard.config_safety.evidence.no_drift')}
        items={report.drift.items.slice(0, 3)}
        renderItem={(item) => <DriftRow item={item} />}
      />
      <PreviewSection
        title={t('dashboard.config_safety.evidence.sections.outdated_collectors')}
        empty={t('dashboard.config_safety.evidence.no_outdated_collectors')}
        items={report.outdated_collectors.slice(0, 3)}
        renderItem={(item) => <OutdatedCollectorRow item={item} />}
      />
      <PreviewSection
        title={t('dashboard.config_safety.evidence.sections.audit_trail')}
        empty={t('dashboard.config_safety.evidence.no_audit_trail')}
        items={report.audit_trail.slice(0, 3)}
        renderItem={(item) => <AuditTrailRow item={item} />}
      />

      {report.signature && (
        <section className="config-evidence-signature">
          <h3>{t('dashboard.config_safety.evidence.signature.title')}</h3>
          <dl>
            <div>
              <dt>{t('dashboard.config_safety.evidence.signature.algorithm')}</dt>
              <dd>{report.signature.algorithm}</dd>
            </div>
            {report.signature.key_id && (
              <div>
                <dt>{t('dashboard.config_safety.evidence.signature.key_id')}</dt>
                <dd>{report.signature.key_id}</dd>
              </div>
            )}
            <div>
              <dt>{t('dashboard.config_safety.evidence.signature.digest')}</dt>
              <dd>{shortText(report.signature.payload_digest_sha256)}</dd>
            </div>
            <div>
              <dt>{t('dashboard.config_safety.evidence.signature.hint')}</dt>
              <dd>{report.signature.verification_hint}</dd>
            </div>
          </dl>
        </section>
      )}
    </div>
  )
}

function PreviewSection<T>({
  title,
  empty,
  items,
  renderItem,
}: {
  title: string
  empty: string
  items: T[]
  renderItem: (item: T) => ReactNode
}) {
  return (
    <section className="config-evidence-preview-section">
      <h3>{title}</h3>
      {items.length > 0 ? (
        <div className="config-evidence-preview-list">
          {items.map((item, index) => (
            <div key={index}>{renderItem(item)}</div>
          ))}
        </div>
      ) : (
        <p className="config-evidence-muted">{empty}</p>
      )}
    </section>
  )
}

function ConfigChangeRow({ item }: { item: EvidenceConfigChange }) {
  const { t } = useTranslation()
  return (
    <div className="config-evidence-row">
      <strong>{item.display_name ?? item.workload_id}</strong>
      <span>
        {shortText(item.previous_hash) || '—'} → {shortText(item.config_hash)} · {item.status}
      </span>
      {item.diff_summary && <span>{item.diff_summary}</span>}
      {!item.content_available && <em>{t('dashboard.config_safety.evidence.redacted_content')}</em>}
    </div>
  )
}

function ValidationFailureRow({ item }: { item: EvidenceValidationFailure }) {
  return (
    <div className="config-evidence-row config-evidence-row-danger">
      <strong>{item.display_name ?? item.workload_id}</strong>
      <span>{item.error}</span>
      <code>{shortText(item.config_hash)}</code>
    </div>
  )
}

function RollbackRow({ item }: { item: EvidenceRollback }) {
  return (
    <div className="config-evidence-row">
      <strong>{item.display_name ?? item.workload_id}</strong>
      <span>
        {item.status} · {shortText(item.config_hash)}
      </span>
    </div>
  )
}

function DriftRow({ item }: { item: ConfigDriftItem }) {
  const reasons = item.drift_reasons?.join(', ')
  return (
    <div className="config-evidence-row config-evidence-row-warning">
      <strong>{item.collector}</strong>
      <span>
        {item.drift_status} · {item.env} · {shortText(item.effective_config_hash) || '—'}
      </span>
      {reasons && <span>{reasons}</span>}
    </div>
  )
}

function OutdatedCollectorRow({ item }: { item: FleetCollectorVersionFinding }) {
  return (
    <div className="config-evidence-row config-evidence-row-warning">
      <strong>{item.display_name}</strong>
      <span>
        {item.display_name} {item.version} → {item.recommended_version}
      </span>
    </div>
  )
}

function AuditTrailRow({ item }: { item: EvidenceAuditTrailEntry }) {
  return (
    <div className="config-evidence-row">
      <strong>{item.action}</strong>
      <span>
        {item.resource}
        {item.resource_id ? `/${item.resource_id}` : ''}
      </span>
      {item.detail && <span>{item.detail}</span>}
    </div>
  )
}

function isReportEmpty(report: EvidenceReport) {
  return (
    report.summary.config_changes === 0 &&
    report.summary.validation_failures === 0 &&
    report.summary.rollbacks === 0 &&
    report.summary.drifted_collectors === 0 &&
    report.summary.outdated_collectors === 0 &&
    report.summary.audit_events === 0 &&
    report.config_changes.length === 0 &&
    report.validation_failures.length === 0 &&
    report.rollbacks.length === 0 &&
    report.outdated_collectors.length === 0 &&
    report.audit_trail.length === 0
  )
}

function downloadEvidenceReport(download: EvidenceReportDownload) {
  const url = URL.createObjectURL(download.blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = download.filename
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
}

function formatDate(value: string) {
  return new Date(value).toLocaleString()
}

function shortText(value?: string) {
  if (!value) return ''
  return value.length > 16 ? `${value.slice(0, 16)}…` : value
}
