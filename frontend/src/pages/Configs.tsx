import { useMemo, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configsAPI } from '../api/client'
import MigrationAssistantPanel from '../components/config/MigrationAssistantPanel'
import YamlEditor from '../components/config/YamlEditor'
import type { Config, ConfigKind, ConfigVariable } from '../types'

type LibraryFilter = 'all' | ConfigKind

type NormalizedConfig = Config & {
  kind: ConfigKind
  status: string
  variables: ConfigVariable[]
  tags: string[]
}

const KIND_ORDER: LibraryFilter[] = ['all', 'saved', 'template', 'draft', 'known_good']

function normalizeKind(config: Config): ConfigKind {
  if (config.kind === 'template' || config.kind === 'draft' || config.kind === 'known_good') {
    return config.kind
  }
  return 'saved'
}

function normalizeConfig(config: Config): NormalizedConfig {
  return {
    ...config,
    kind: normalizeKind(config),
    status: config.status ?? (config.kind === 'draft' ? 'draft' : 'ready'),
    variables: config.variables ?? [],
    tags: config.tags ?? [],
    built_in: config.built_in ?? false,
  }
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export default function Configs() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data: rawConfigs, isLoading } = useQuery({
    queryKey: ['configs'],
    queryFn: configsAPI.list,
  })

  const [name, setName] = useState('')
  const [content, setContent] = useState('')
  const [showForm, setShowForm] = useState(false)
  const [activeFilter, setActiveFilter] = useState<LibraryFilter>('all')
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null)

  const configs = useMemo(() => (rawConfigs ?? []).map(normalizeConfig), [rawConfigs])
  const templates = configs.filter((config) => config.kind === 'template')
  const savedConfigs = configs.filter((config) => config.kind === 'saved')
  const drafts = configs.filter((config) => config.kind === 'draft' || config.status === 'draft')
  const knownGood = configs.filter((config) => config.kind === 'known_good')

  const filteredConfigs = configs.filter((config) => {
    if (activeFilter === 'all') return true
    if (activeFilter === 'draft') return config.kind === 'draft' || config.status === 'draft'
    return config.kind === activeFilter
  })

  const createMutation = useMutation({
    mutationFn: () =>
      configsAPI.create(name, content, { kind: 'draft', status: 'draft', source_type: 'manual' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configs'] })
      setName('')
      setContent('')
      setSelectedTemplateId(null)
      setShowForm(false)
    },
  })

  const handleMigrationDraftSaved = () => {
    queryClient.invalidateQueries({ queryKey: ['configs'] })
    setActiveFilter('draft')
  }

  const startBlankConfig = () => {
    setName('')
    setContent('')
    setSelectedTemplateId(null)
    setShowForm((value) => !value)
  }

  const applyTemplate = (template: NormalizedConfig) => {
    setName(`${t('configs.form.draft_from')} ${template.name}`)
    setContent(template.content)
    setSelectedTemplateId(template.id)
    setShowForm(true)
  }

  const conceptCards = [
    { key: 'saved' as const, count: savedConfigs.length },
    { key: 'template' as const, count: templates.length },
    { key: 'draft' as const, count: drafts.length },
    { key: 'known_good' as const, count: knownGood.length },
  ]

  return (
    <div>
      <div className="page-header configs-library-header">
        <div>
          <h1 className="page-title">{t('configs.title')}</h1>
          <p className="configs-library-subtitle">{t('configs.subtitle')}</p>
        </div>
        <button
          className={`btn ${showForm ? '' : 'btn-primary'}`}
          onClick={startBlankConfig}
          type="button"
        >
          {showForm ? t('common.cancel') : t('configs.new_button')}
        </button>
      </div>

      <section className="configs-library-concepts" aria-label={t('configs.concepts_aria')}>
        {conceptCards.map((card) => (
          <button
            className={`configs-concept-card ${activeFilter === card.key ? 'configs-concept-card-active' : ''}`}
            key={card.key}
            onClick={() => setActiveFilter(card.key)}
            type="button"
          >
            <span className="configs-concept-count">{card.count}</span>
            <span className="configs-concept-title">{t(`configs.concepts.${card.key}.title`)}</span>
            <span className="configs-concept-copy">{t(`configs.concepts.${card.key}.body`)}</span>
          </button>
        ))}
      </section>

      <MigrationAssistantPanel onDraftSaved={handleMigrationDraftSaved} />

      <div className="configs-filter-bar" role="tablist" aria-label={t('configs.filter.label')}>
        {KIND_ORDER.map((kind) => (
          <button
            key={kind}
            className={`configs-filter-tab ${activeFilter === kind ? 'configs-filter-tab-active' : ''}`}
            onClick={() => setActiveFilter(kind)}
            role="tab"
            aria-selected={activeFilter === kind}
            type="button"
          >
            {t(`configs.filter.${kind}`)}
          </button>
        ))}
      </div>

      {showForm && (
        <div className="configs-form">
          <div className="configs-form-header">
            {selectedTemplateId ? t('configs.form.template_header') : t('configs.form.header')}
          </div>
          <div className="configs-form-body">
            <div className="field">
              <label className="field-label" htmlFor="config-name">
                {t('configs.form.name')}
              </label>
              <input
                id="config-name"
                className="field-input"
                placeholder={t('configs.form.name_placeholder')}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="field configs-editor-field">
              <label className="field-label" htmlFor="config-content-editor">
                {t('configs.form.content')}
              </label>
              <div id="config-content-editor">
                <YamlEditor value={content} onChange={setContent} />
              </div>
              <p className="configs-form-help">{t('configs.form.secret_help')}</p>
            </div>
          </div>
          <div className="configs-form-footer">
            <button
              className="btn btn-primary"
              onClick={() => createMutation.mutate()}
              disabled={!name || !content || createMutation.isPending}
              type="button"
            >
              {createMutation.isPending ? t('configs.form.saving') : t('configs.form.submit')}
            </button>
          </div>
        </div>
      )}

      {isLoading ? (
        <div className="loading">{t('configs.loading')}</div>
      ) : configs.length === 0 ? (
        <div className="empty-state">{t('configs.empty')}</div>
      ) : (
        <div className="configs-library-layout">
          {(activeFilter === 'all' || activeFilter === 'template') && (
            <section className="configs-library-section" aria-labelledby="templates-heading">
              <div className="configs-section-header">
                <div>
                  <h2 id="templates-heading">{t('configs.templates.title')}</h2>
                  <p>{t('configs.templates.subtitle')}</p>
                </div>
                <span className="configs-section-count">{templates.length}</span>
              </div>
              <div className="configs-template-grid">
                {templates.map((template) => (
                  <article className="configs-template-card" key={template.id}>
                    <div className="configs-template-heading">
                      <div>
                        <h3>{template.name}</h3>
                        <p>{template.description || t('configs.templates.no_description')}</p>
                      </div>
                      <span className="configs-kind-pill">{t('configs.kind.template')}</span>
                    </div>
                    <div className="configs-template-meta">
                      {template.category && <span>{template.category}</span>}
                      {template.stack && <span>{template.stack}</span>}
                      {template.built_in && <span>{t('configs.templates.built_in')}</span>}
                    </div>
                    <div
                      className="configs-variable-list"
                      aria-label={t('configs.templates.variables')}
                    >
                      {template.variables.length > 0 ? (
                        template.variables.map((variable) => (
                          <div className="configs-variable" key={`${template.id}-${variable.name}`}>
                            <div>
                              <span className="configs-variable-name">
                                {variable.label || variable.name}
                              </span>
                              {variable.required && (
                                <span className="configs-variable-required">
                                  {t('configs.templates.required')}
                                </span>
                              )}
                            </div>
                            <code>
                              {variable.placeholder || '${' + variable.name.toUpperCase() + '}'}
                            </code>
                          </div>
                        ))
                      ) : (
                        <p className="configs-muted-copy">{t('configs.templates.no_variables')}</p>
                      )}
                    </div>
                    {template.tags.length > 0 && (
                      <div className="configs-tag-list">
                        {template.tags.map((tag) => (
                          <span className="configs-tag" key={`${template.id}-${tag}`}>
                            {tag}
                          </span>
                        ))}
                      </div>
                    )}
                    <button
                      className="btn btn-primary configs-template-use"
                      onClick={() => applyTemplate(template)}
                      aria-label={`${t('configs.templates.use_aria')}: ${template.name}`}
                      type="button"
                    >
                      {t('configs.templates.use')}
                    </button>
                  </article>
                ))}
              </div>
            </section>
          )}

          {(activeFilter === 'all' || activeFilter !== 'template') && (
            <section className="configs-library-section" aria-labelledby="configs-heading">
              <div className="configs-section-header">
                <div>
                  <h2 id="configs-heading">{t('configs.saved.title')}</h2>
                  <p>{t('configs.saved.subtitle')}</p>
                </div>
                <span className="configs-section-count">{filteredConfigs.length}</span>
              </div>
              <div className="configs-table-wrap">
                <table className="data-table configs-library-table">
                  <thead>
                    <tr>
                      <th>{t('configs.table.name')}</th>
                      <th>{t('configs.table.kind')}</th>
                      <th>{t('configs.table.category')}</th>
                      <th>{t('configs.table.created_by')}</th>
                      <th>{t('configs.table.created_at')}</th>
                      <th>{t('configs.table.id')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredConfigs.map((config) => (
                      <tr key={config.id}>
                        <td className="configs-name-cell">{config.name}</td>
                        <td>
                          <span className="configs-kind-pill">
                            {t(`configs.kind.${config.kind}`)}
                          </span>
                        </td>
                        <td>{config.category || config.stack || t('configs.table.none')}</td>
                        <td className="configs-mono-cell">
                          {config.created_by || t('configs.table.system')}
                        </td>
                        <td className="configs-date-cell">{formatDate(config.created_at)}</td>
                        <td>
                          <code>{config.id.substring(0, 12)}...</code>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>
          )}
        </div>
      )}
    </div>
  )
}
