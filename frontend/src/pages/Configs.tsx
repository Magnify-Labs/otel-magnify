import { useMemo, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { configsAPI, getAPIErrorDetails } from '../api/client'
import YamlEditor from '../components/config/YamlEditor'
import type { Config, ConfigKind } from '../types'

type Tab = 'saved' | 'new'

type NormalizedConfig = Config & {
  kind: ConfigKind
  status: string
  tags: string[]
}

interface ConfigsListLabels {
  loading: string
  loadError: string
  empty: string
  name: string
  kind: string
  category: string
  createdBy: string
  createdAt: string
  id: string
  none: string
  system: string
  kindLabel: (kind: ConfigKind) => string
}

interface ConfigsListProps {
  configs: NormalizedConfig[]
  isLoading: boolean
  isError: boolean
  labels: ConfigsListLabels
}

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
    tags: config.tags ?? [],
  }
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function ConfigsList({ configs, isLoading, isError, labels }: ConfigsListProps) {
  if (isLoading) {
    return <div className="loading">{labels.loading}</div>
  }

  if (isError) {
    return <div className="error-text">{labels.loadError}</div>
  }

  if (configs.length === 0) {
    return <div className="empty-state">{labels.empty}</div>
  }

  return (
    <table className="data-table configs-library-table">
      <thead>
        <tr>
          <th>{labels.name}</th>
          <th>{labels.kind}</th>
          <th>{labels.category}</th>
          <th>{labels.createdBy}</th>
          <th>{labels.createdAt}</th>
          <th>{labels.id}</th>
        </tr>
      </thead>
      <tbody>
        {configs.map((config) => (
          <tr key={config.id}>
            <td className="configs-name-cell">{config.name}</td>
            <td>
              <span className="configs-kind-pill">{labels.kindLabel(config.kind)}</span>
            </td>
            <td>{config.category || config.stack || labels.none}</td>
            <td className="configs-mono-cell">{config.created_by || labels.system}</td>
            <td className="configs-date-cell">{formatDate(config.created_at)}</td>
            <td>
              <code>{config.id.substring(0, 12)}...</code>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export default function Configs() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const {
    data: rawConfigs = [],
    isLoading,
    isError,
  } = useQuery({
    queryKey: ['configs'],
    queryFn: configsAPI.list,
  })

  const [name, setName] = useState('')
  const [content, setContent] = useState('')
  const [activeTab, setActiveTab] = useState<Tab>('saved')
  const [createError, setCreateError] = useState<string | null>(null)

  const savedConfigs = useMemo(
    () => rawConfigs.map(normalizeConfig).filter((config) => config.kind === 'saved'),
    [rawConfigs],
  )

  const createMutation = useMutation({
    mutationFn: () =>
      configsAPI.create(name, content, { kind: 'saved', status: 'ready', source_type: 'manual' }),
    onMutate: () => {
      setCreateError(null)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configs'] })
      setName('')
      setContent('')
      setActiveTab('saved')
    },
    onError: (err: unknown) => {
      setCreateError(getAPIErrorDetails(err, 'Failed to create configuration').message)
    },
  })

  const labels: ConfigsListLabels = {
    loading: t('configs.loading'),
    loadError: t('configs.load_error', { defaultValue: 'Failed to load configs' }),
    empty: t('configs.empty'),
    name: t('configs.table.name'),
    kind: t('configs.table.kind'),
    category: t('configs.table.category'),
    createdBy: t('configs.table.created_by'),
    createdAt: t('configs.table.created_at'),
    id: t('configs.table.id'),
    none: t('configs.table.none'),
    system: t('configs.table.system'),
    kindLabel: (kind) => t(`configs.kind.${kind}`),
  }

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">{t('configs.title')}</h1>
      </div>

      <nav
        className="configs-tab-bar"
        role="tablist"
        aria-label={t('configs.tabs_aria', { defaultValue: 'Config sections' })}
      >
        <button
          type="button"
          id="configs-tab-saved"
          role="tab"
          aria-selected={activeTab === 'saved'}
          aria-controls="configs-panel-saved"
          className={activeTab === 'saved' ? 'configs-tab configs-tab-active' : 'configs-tab'}
          onClick={() => setActiveTab('saved')}
        >
          {t('configs.filter.saved')}
        </button>
        <button
          type="button"
          id="configs-tab-new"
          role="tab"
          aria-selected={activeTab === 'new'}
          aria-controls="configs-panel-new"
          className={activeTab === 'new' ? 'configs-tab configs-tab-active' : 'configs-tab'}
          onClick={() => setActiveTab('new')}
        >
          {t('configs.new_button')}
        </button>
      </nav>

      {activeTab === 'saved' && (
        <section
          id="configs-panel-saved"
          role="tabpanel"
          aria-labelledby="configs-tab-saved"
          className="configs-tab-panel"
        >
          <ConfigsList
            configs={savedConfigs}
            isLoading={isLoading}
            isError={isError}
            labels={labels}
          />
        </section>
      )}

      {activeTab === 'new' && (
        <section
          id="configs-panel-new"
          role="tabpanel"
          aria-labelledby="configs-tab-new"
          className="configs-form"
        >
          <div className="configs-form-header">{t('configs.form.header')}</div>
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
            <div className="field field-flush">
              <label className="field-label" htmlFor="config-content-editor">
                {t('configs.form.content')}
              </label>
              <div id="config-content-editor">
                <YamlEditor value={content} onChange={setContent} />
              </div>
              <p className="configs-form-help">{t('configs.form.secret_help')}</p>
            </div>
            {createError && <div className="error-text error-text-push">{createError}</div>}
          </div>
          <div className="configs-form-footer">
            <button
              className="btn btn-primary"
              onClick={() => createMutation.mutate()}
              disabled={!name || !content || createMutation.isPending}
              type="button"
            >
              {createMutation.isPending
                ? t('configs.form.saving')
                : t('configs.form.create', { defaultValue: 'Create' })}
            </button>
          </div>
        </section>
      )}
    </div>
  )
}
