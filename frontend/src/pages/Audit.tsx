import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { auditAPI, buildAuditEventsCSVUrl, getAPIErrorDetails } from '../api/client'
import { hasPerm } from '../lib/perm'
import { useStore } from '../store'
import { useCapability } from '../hooks/useCapability'
import type { AuditEventFilters, AuditRecord } from '../types'
import '../styles/audit.css'

type FilterKey = 'user' | 'action' | 'workload_id' | 'resource_id' | 'config_hash' | 'from' | 'to'
type AuditFormState = Record<FilterKey, string>

function filtersFromSearch(searchParams: URLSearchParams): AuditFormState {
  return {
    user:
      searchParams.get('user') ?? searchParams.get('email') ?? searchParams.get('user_id') ?? '',
    action: searchParams.get('action') ?? '',
    workload_id: searchParams.get('workload_id') ?? '',
    resource_id: searchParams.get('resource_id') ?? '',
    config_hash: searchParams.get('config_hash') ?? '',
    from: searchParams.get('from') ?? '',
    to: searchParams.get('to') ?? '',
  }
}

function toAuditFilters(form: AuditFormState): AuditEventFilters {
  return Object.fromEntries(
    Object.entries(form)
      .map(([key, value]) => [key, value.trim()])
      .filter(([, value]) => value !== ''),
  ) as AuditEventFilters
}

function shortHash(hash?: string) {
  if (!hash) return '—'
  return hash.length > 18 ? `${hash.slice(0, 18)}…` : hash
}

function formatDate(value: string) {
  if (!value) return '—'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'medium' })
}

function rowWorkloadID(event: AuditRecord) {
  return event.workload_id || event.resource_id || ''
}

function diffHref(event: AuditRecord) {
  const workloadID = rowWorkloadID(event)
  if (!workloadID || !event.config_hash) return undefined
  return `/workloads/${encodeURIComponent(workloadID)}?config_hash=${encodeURIComponent(event.config_hash)}#config-history`
}

function rollbackHref(event: AuditRecord) {
  const workloadID = rowWorkloadID(event)
  if (!workloadID || !event.config_hash) return undefined
  return `/workloads/${encodeURIComponent(workloadID)}?rollback_hash=${encodeURIComponent(event.config_hash)}#config-history`
}

function buildSearchParams(form: AuditFormState) {
  const next = new URLSearchParams()
  const filters = toAuditFilters(form)
  for (const [key, value] of Object.entries(filters)) {
    next.set(key, value)
  }
  return next
}

function AuditAccessDenied() {
  const { t } = useTranslation()
  return (
    <section className="panel audit-access-panel" aria-labelledby="audit-access-title">
      <h1 className="page-title" id="audit-access-title">
        {t('audit.access.title')}
      </h1>
      <p className="panel-hint">{t('audit.access.body')}</p>
    </section>
  )
}

function AuditRowActions({ event }: { event: AuditRecord }) {
  const { t } = useTranslation()
  const diff = diffHref(event)
  const rollback = rollbackHref(event)

  if (!diff || !rollback) {
    return <span className="audit-muted">{t('audit.table.no_links')}</span>
  }

  return (
    <div className="audit-row-actions">
      <Link className="button button-secondary audit-action-link" to={diff}>
        {t('audit.table.view_diff')}
      </Link>
      <Link className="button button-secondary audit-action-link" to={rollback}>
        {t('audit.table.rollback')}
      </Link>
    </div>
  )
}

function AuditFilterPanel({
  initialFilters,
  onApply,
}: {
  initialFilters: AuditFormState
  onApply: (form: AuditFormState) => void
}) {
  const { t } = useTranslation()
  const [form, setForm] = useState<AuditFormState>(initialFilters)

  const applyFilters = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    onApply(form)
  }

  return (
    <section className="panel audit-filter-panel" aria-labelledby="audit-filter-title">
      <div className="panel-head">
        <div>
          <h2 className="panel-title" id="audit-filter-title">
            {t('audit.filters.title')}
          </h2>
          <p className="panel-hint">{t('audit.filters.hint')}</p>
        </div>
      </div>
      <form className="audit-filter-grid" onSubmit={applyFilters}>
        <label>
          <span>{t('audit.filters.user')}</span>
          <input
            className="search-input"
            type="search"
            value={form.user}
            onChange={(event) => setForm((current) => ({ ...current, user: event.target.value }))}
          />
        </label>
        <label>
          <span>{t('audit.filters.action')}</span>
          <input
            className="search-input"
            type="search"
            value={form.action}
            onChange={(event) => setForm((current) => ({ ...current, action: event.target.value }))}
          />
        </label>
        <label>
          <span>{t('audit.filters.workload')}</span>
          <input
            className="search-input"
            type="search"
            value={form.workload_id}
            onChange={(event) =>
              setForm((current) => ({ ...current, workload_id: event.target.value }))
            }
          />
        </label>
        <label>
          <span>{t('audit.filters.resource')}</span>
          <input
            className="search-input"
            type="search"
            value={form.resource_id}
            onChange={(event) =>
              setForm((current) => ({ ...current, resource_id: event.target.value }))
            }
          />
        </label>
        <label>
          <span>{t('audit.filters.config_hash')}</span>
          <input
            className="search-input"
            type="search"
            value={form.config_hash}
            onChange={(event) =>
              setForm((current) => ({ ...current, config_hash: event.target.value }))
            }
          />
        </label>
        <label>
          <span>{t('audit.filters.from')}</span>
          <input
            className="search-input"
            type="text"
            value={form.from}
            placeholder="2026-07-01T00:00:00Z"
            onChange={(event) => setForm((current) => ({ ...current, from: event.target.value }))}
          />
        </label>
        <label>
          <span>{t('audit.filters.to')}</span>
          <input
            className="search-input"
            type="text"
            value={form.to}
            placeholder="2026-07-03T00:00:00Z"
            onChange={(event) => setForm((current) => ({ ...current, to: event.target.value }))}
          />
        </label>
        <button className="button button-primary audit-apply" type="submit">
          {t('audit.filters.apply')}
        </button>
      </form>
    </section>
  )
}

export default function Audit() {
  const { t } = useTranslation()
  const me = useStore((s) => s.me)
  const [searchParams, setSearchParams] = useSearchParams()
  const { enabled: auditViewerEnabled, isLoading: auditViewerLoading } = useCapability('audit.viewer')
  const canViewAudit = auditViewerEnabled && hasPerm(me?.groups, 'audit:view')
  const filters = useMemo(() => filtersFromSearch(searchParams), [searchParams])
  const auditFilters = useMemo(() => toAuditFilters(filters), [filters])
  const csvHref = useMemo(() => buildAuditEventsCSVUrl(auditFilters), [auditFilters])

  const query = useQuery({
    queryKey: ['audit-events', auditFilters],
    queryFn: () => auditAPI.list(auditFilters),
    enabled: canViewAudit,
    staleTime: 15_000,
  })

  if (!me) return null
  if (auditViewerLoading) {
    return (
      <section className="panel audit-access-panel">
        <p className="panel-hint">{t('common.loading')}</p>
      </section>
    )
  }
  if (!canViewAudit) return <AuditAccessDenied />

  const applyFilters = (form: AuditFormState) => {
    setSearchParams(buildSearchParams(form), { replace: true })
  }

  const handleExport = async (event: React.MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault()
    try {
      const blob = await auditAPI.exportCSV(auditFilters)
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = 'audit-events.csv'
      document.body.appendChild(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
    } catch (err) {
      window.alert(getAPIErrorDetails(err, t('audit.export_error')).message)
    }
  }

  return (
    <div className="audit-page">
      <header className="page-header audit-header">
        <div>
          <h1 className="page-title">{t('audit.title')}</h1>
          <p className="page-subtitle">{t('audit.subtitle')}</p>
        </div>
        <a className="button button-primary" href={csvHref} onClick={handleExport}>
          {t('audit.export_csv')}
        </a>
      </header>

      <AuditFilterPanel
        key={searchParams.toString()}
        initialFilters={filters}
        onApply={applyFilters}
      />

      <section className="panel audit-table-panel" aria-labelledby="audit-table-title">
        <div className="panel-head">
          <div>
            <h2 className="panel-title" id="audit-table-title">
              {t('audit.table.title')}
            </h2>
            <p className="panel-hint">
              {query.data
                ? t('audit.table.count', { count: query.data.total })
                : t('audit.table.hint')}
            </p>
          </div>
        </div>

        {query.isLoading ? (
          <p className="panel-hint">{t('common.loading')}</p>
        ) : query.isError ? (
          <div className="audit-state audit-state-error">
            <p>{getAPIErrorDetails(query.error, t('audit.error')).message}</p>
            <button
              className="button button-secondary"
              type="button"
              onClick={() => query.refetch()}
            >
              {t('common.retry')}
            </button>
          </div>
        ) : query.data && !query.data.available ? (
          <div className="audit-state">
            <strong>{t('audit.unavailable.title')}</strong>
            <p>{t('audit.unavailable.body')}</p>
          </div>
        ) : query.data && query.data.events.length === 0 ? (
          <div className="audit-state">
            <strong>{t('audit.empty.title')}</strong>
            <p>{t('audit.empty.body')}</p>
          </div>
        ) : (
          <div className="audit-table-wrap">
            <table className="audit-table">
              <thead>
                <tr>
                  <th>{t('audit.table.columns.timestamp')}</th>
                  <th>{t('audit.table.columns.user')}</th>
                  <th>{t('audit.table.columns.action')}</th>
                  <th>{t('audit.table.columns.resource')}</th>
                  <th>{t('audit.table.columns.config_hash')}</th>
                  <th>{t('audit.table.columns.chain')}</th>
                  <th>{t('audit.table.columns.detail')}</th>
                  <th>{t('audit.table.columns.links')}</th>
                </tr>
              </thead>
              <tbody>
                {(query.data?.events ?? []).map((event) => (
                  <tr key={event.id || `${event.occurred_at}-${event.action}-${event.event_hash}`}>
                    <td>{formatDate(event.occurred_at)}</td>
                    <td>
                      <strong>{event.email || event.user_id || '—'}</strong>
                      {event.email && event.user_id && (
                        <span className="audit-muted">{event.user_id}</span>
                      )}
                    </td>
                    <td className="audit-mono">{event.action}</td>
                    <td>
                      <strong>{event.resource || '—'}</strong>
                      <span className="audit-muted">
                        {event.workload_id || event.resource_id || '—'}
                      </span>
                    </td>
                    <td className="audit-mono" title={event.config_hash}>
                      {shortHash(event.config_hash)}
                    </td>
                    <td>
                      <span className="audit-muted">{t('audit.table.prev_hash')}</span>
                      <span className="audit-mono audit-chain-value">{event.prev_hash || '—'}</span>
                      <span className="audit-muted">{t('audit.table.event_hash')}</span>
                      <span className="audit-mono audit-chain-value">
                        {event.event_hash || '—'}
                      </span>
                      {event.immutable_ref && (
                        <span className="audit-muted">{event.immutable_ref}</span>
                      )}
                    </td>
                    <td>{event.detail || '—'}</td>
                    <td>
                      <AuditRowActions event={event} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
