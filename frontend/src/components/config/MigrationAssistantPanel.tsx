import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { configsAPI, getAPIErrorDetails } from '../../api/client'
import YamlEditor from './YamlEditor'
import type { Config, ConfigMigrationPreviewResponse, ConfigMigrationVendor } from '../../types'

type MigrationAssistantPanelProps = {
  onDraftSaved: (config: Config) => void
}

type VendorSample = {
  vendor: ConfigMigrationVendor
  sourceFormat: string
  source: string
}

const VENDOR_SAMPLES: VendorSample[] = [
  {
    vendor: 'datadog_agent',
    sourceFormat: 'yaml',
    source:
      'logs_enabled: true\napi_key: ${DATADOG_API_KEY}\napm_config:\n  enabled: true\nlogs_config:\n  container_collect_all: true\n',
  },
  {
    vendor: 'fluent_bit',
    sourceFormat: 'conf',
    source:
      '[INPUT]\n    Name tail\n    Path /var/log/containers/*.log\n[OUTPUT]\n    Name opensearch\n    Host logs.example.com\n',
  },
  {
    vendor: 'splunk_forwarder',
    sourceFormat: 'conf',
    source:
      '[monitor:///var/log/app.log]\nsourcetype = app:json\nindex = observability\n[tcpout]\ndefaultGroup = prod\n',
  },
  {
    vendor: 'new_relic_infra',
    sourceFormat: 'yaml',
    source:
      'license_key: ${NEW_RELIC_LICENSE_KEY}\nlog:\n  file: /var/log/newrelic-infra/newrelic-infra.log\ncustom_attributes:\n  env: prod\n',
  },
]

function sampleForVendor(vendor: ConfigMigrationVendor) {
  return VENDOR_SAMPLES.find((sample) => sample.vendor === vendor) ?? VENDOR_SAMPLES[0]
}

export default function MigrationAssistantPanel({ onDraftSaved }: MigrationAssistantPanelProps) {
  const { t } = useTranslation()
  const [vendor, setVendor] = useState<ConfigMigrationVendor>('datadog_agent')
  const [sourceFormat, setSourceFormat] = useState(sampleForVendor('datadog_agent').sourceFormat)
  const [source, setSource] = useState(sampleForVendor('datadog_agent').source)
  const [targetExporter, setTargetExporter] = useState('otlp')
  const [otlpEndpoint, setOtlpEndpoint] = useState('${OTLP_ENDPOINT}')
  const [preview, setPreview] = useState<ConfigMigrationPreviewResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [isPreviewing, setIsPreviewing] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [copyStatus, setCopyStatus] = useState<'idle' | 'copied' | 'failed'>('idle')
  const [savedDraftName, setSavedDraftName] = useState<string | null>(null)

  const selectedSample = useMemo(() => sampleForVendor(vendor), [vendor])

  const selectSample = (sample: VendorSample) => {
    setVendor(sample.vendor)
    setSourceFormat(sample.sourceFormat)
    setSource(sample.source)
    setPreview(null)
    setError(null)
    setCopyStatus('idle')
    setSavedDraftName(null)
  }

  const runPreview = async () => {
    setIsPreviewing(true)
    setError(null)
    setCopyStatus('idle')
    setSavedDraftName(null)
    try {
      const result = await configsAPI.previewConfigMigration({
        schema_version: 'config_migration_preview_request.v1',
        vendor,
        source,
        source_format: sourceFormat,
        context: {
          target_exporter: targetExporter,
          otlp_endpoint: otlpEndpoint,
        },
      })
      setPreview(result)
    } catch (err) {
      setPreview(null)
      setError(getAPIErrorDetails(err, t('configs.migration.error')).message)
    } finally {
      setIsPreviewing(false)
    }
  }

  const copyDraft = async () => {
    if (!preview) return
    try {
      await navigator.clipboard.writeText(preview.draft_yaml)
      setCopyStatus('copied')
    } catch {
      setCopyStatus('failed')
    }
  }

  const saveDraft = async () => {
    if (!preview) return
    setIsSaving(true)
    setError(null)
    try {
      const hint = preview.save_hint
      const config = await configsAPI.create(preview.draft_name, preview.draft_yaml, {
        kind: 'draft',
        status: 'draft',
        source_type: hint.source_type || 'migration_assistant',
        category: hint.category,
        stack: hint.stack,
        tags: hint.tags,
      })
      setSavedDraftName(config.name)
      onDraftSaved(config)
    } catch (err) {
      setError(getAPIErrorDetails(err, t('configs.migration.save_error')).message)
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <section className="migration-assistant-panel" aria-labelledby="migration-assistant-heading">
      <div className="migration-assistant-header">
        <div>
          <p className="configs-section-eyebrow">{t('configs.migration.eyebrow')}</p>
          <h2 id="migration-assistant-heading">{t('configs.migration.title')}</h2>
          <p>{t('configs.migration.subtitle')}</p>
        </div>
        <span className="migration-assistant-safe-badge">{t('configs.migration.safe_badge')}</span>
      </div>

      <div className="migration-assistant-samples" aria-label={t('configs.migration.samples_aria')}>
        {VENDOR_SAMPLES.map((sample) => (
          <button
            className={`configs-filter-tab ${sample.vendor === vendor ? 'configs-filter-tab-active' : ''}`}
            key={sample.vendor}
            onClick={() => selectSample(sample)}
            type="button"
          >
            {t(`configs.migration.vendor.${sample.vendor}`)}
          </button>
        ))}
      </div>

      <div className="migration-assistant-grid">
        <div className="migration-assistant-source">
          <div className="field">
            <label className="field-label" htmlFor="migration-vendor">
              {t('configs.migration.vendor_label')}
            </label>
            <select
              className="field-input"
              id="migration-vendor"
              onChange={(event) => {
                const nextVendor = event.target.value as ConfigMigrationVendor
                selectSample(sampleForVendor(nextVendor))
              }}
              value={vendor}
            >
              {VENDOR_SAMPLES.map((sample) => (
                <option key={sample.vendor} value={sample.vendor}>
                  {t(`configs.migration.vendor.${sample.vendor}`)}
                </option>
              ))}
            </select>
          </div>
          <div className="migration-assistant-inline-fields">
            <div className="field">
              <label className="field-label" htmlFor="migration-source-format">
                {t('configs.migration.source_format')}
              </label>
              <input
                className="field-input"
                id="migration-source-format"
                onChange={(event) => setSourceFormat(event.target.value)}
                value={sourceFormat}
              />
            </div>
            <div className="field">
              <label className="field-label" htmlFor="migration-target-exporter">
                {t('configs.migration.target_exporter')}
              </label>
              <input
                className="field-input"
                id="migration-target-exporter"
                onChange={(event) => setTargetExporter(event.target.value)}
                value={targetExporter}
              />
            </div>
          </div>
          <div className="field">
            <label className="field-label" htmlFor="migration-otlp-endpoint">
              {t('configs.migration.otlp_endpoint')}
            </label>
            <input
              className="field-input"
              id="migration-otlp-endpoint"
              onChange={(event) => setOtlpEndpoint(event.target.value)}
              value={otlpEndpoint}
            />
          </div>
          <div className="field configs-editor-field">
            <label className="field-label" htmlFor="migration-source-editor">
              {t('configs.migration.source_label')}
            </label>
            <div id="migration-source-editor">
              <YamlEditor value={source} onChange={setSource} />
            </div>
            <p className="configs-form-help">{t('configs.migration.source_help')}</p>
          </div>
          <button
            className="btn btn-primary migration-assistant-preview-button"
            disabled={!source.trim() || isPreviewing}
            onClick={runPreview}
            type="button"
          >
            {isPreviewing ? t('configs.migration.previewing') : t('configs.migration.preview')}
          </button>
        </div>

        <div className="migration-assistant-preview" aria-live="polite">
          {preview ? (
            <>
              <div className="migration-assistant-preview-head">
                <div>
                  <p className="configs-section-eyebrow">{t('configs.migration.preview_label')}</p>
                  <h3>{preview.draft_name}</h3>
                  <p>{preview.summary}</p>
                </div>
                <span className={`migration-confidence migration-confidence-${preview.confidence}`}>
                  {t(`configs.migration.confidence.${preview.confidence}`)}
                </span>
              </div>
              <div className="migration-status-row">
                <span>{t('configs.migration.validation')}</span>
                <strong>
                  {preview.validation?.summary ?? t('configs.migration.validation_unknown')}
                </strong>
              </div>
              <pre className="migration-draft-yaml">{preview.draft_yaml}</pre>
              <div className="migration-assistant-actions">
                <button className="btn" onClick={copyDraft} type="button">
                  {t('configs.migration.copy')}
                </button>
                <button
                  className="btn btn-primary"
                  disabled={isSaving}
                  onClick={saveDraft}
                  type="button"
                >
                  {isSaving ? t('configs.migration.saving') : t('configs.migration.save_draft')}
                </button>
              </div>
              {copyStatus !== 'idle' && (
                <p className="configs-form-help">
                  {copyStatus === 'copied'
                    ? t('configs.migration.copy_success')
                    : t('configs.migration.copy_failed')}
                </p>
              )}
              {savedDraftName && (
                <p className="migration-success" role="status">
                  {t('configs.migration.save_success', { name: savedDraftName })}
                </p>
              )}
              <div className="migration-findings-grid">
                <FindingList
                  empty={t('configs.migration.empty_warnings')}
                  items={preview.warnings.map((warning) => warning.message)}
                  title={t('configs.migration.warnings')}
                />
                <FindingList
                  empty={t('configs.migration.empty_unsupported')}
                  items={preview.unsupported_keys.map((item) => `${item.path}: ${item.reason}`)}
                  title={t('configs.migration.unsupported')}
                />
                <FindingList
                  empty={t('configs.migration.empty_redactions')}
                  items={preview.redactions.map((item) => `${item.path}: ${item.reason}`)}
                  title={t('configs.migration.redactions')}
                />
                <FindingList
                  empty={t('configs.migration.empty_evidence')}
                  items={preview.evidence.map(
                    (item) => `${item.source_path} → ${item.target_path} (${item.rule_id})`,
                  )}
                  title={t('configs.migration.evidence')}
                />
              </div>
            </>
          ) : (
            <div className="migration-assistant-empty">
              <h3>{t('configs.migration.empty_preview_title')}</h3>
              <p>
                {t('configs.migration.empty_preview_body', {
                  vendor: t(`configs.migration.vendor.${selectedSample.vendor}`),
                })}
              </p>
            </div>
          )}
        </div>
      </div>

      {error && (
        <div className="form-error migration-assistant-error" role="alert">
          {error}
        </div>
      )}
    </section>
  )
}

function FindingList({ empty, items, title }: { empty: string; items: string[]; title: string }) {
  return (
    <section className="migration-finding-card">
      <h4>{title}</h4>
      {items.length > 0 ? (
        <ul>
          {items.map((item) => (
            <li key={item}>{item}</li>
          ))}
        </ul>
      ) : (
        <p>{empty}</p>
      )}
    </section>
  )
}
