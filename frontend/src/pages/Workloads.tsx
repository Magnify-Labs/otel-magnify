import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { workloadsAPI } from '../api/client'
import WorkloadCard from '../components/workloads/WorkloadCard'
import { isSupervised, isReadOnlyCollector } from '../lib/workloadCapabilities'
import '../styles/inventory.css'

type ControlFilter = '' | 'supervised' | 'readonly'

export default function Inventory() {
  const { t } = useTranslation()
  const [includeArchived, setIncludeArchived] = useState(false)
  const { data: workloads, isLoading } = useQuery({
    queryKey: ['workloads', { includeArchived }],
    queryFn: () => workloadsAPI.list(includeArchived),
  })
  const [searchParams, setSearchParams] = useSearchParams()

  const [search, setSearch] = useState('')
  const [filterStatus, setFilterStatus] = useState<string>('')

  // Filters that are deep-linkable live in the URL. Derive them on every
  // render so back/forward navigation and external links stay in sync without
  // a state-syncing effect.
  const filterType = searchParams.get('type') ?? ''
  const filterControl = (searchParams.get('control') as ControlFilter | null) ?? ''

  function updateUrlFilter(key: 'type' | 'control', value: string) {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value) next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true },
    )
  }

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    return (workloads ?? []).filter((w) => {
      if (filterType && w.type !== filterType) return false
      if (filterStatus && w.status !== filterStatus) return false
      if (filterControl) {
        if (w.type !== 'collector') return false
        if (filterControl === 'supervised' && !isSupervised(w)) return false
        if (filterControl === 'readonly' && !isReadOnlyCollector(w)) return false
      }
      if (needle) {
        const haystack = `${w.display_name ?? ''} ${w.id}`.toLowerCase()
        if (!haystack.includes(needle)) return false
      }
      return true
    })
  }, [workloads, search, filterType, filterStatus, filterControl])

  const archivedCount = (workloads ?? []).filter((w) => w.archived_at).length

  return (
    <div>
      <header className="page-header">
        <h1 className="page-title">{t('inventory.title')}</h1>
        <span className="page-header-count">
          {filtered.length} / {workloads?.length ?? 0}
        </span>
      </header>

      <div className="filter-bar">
        <input
          className="search-input"
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('inventory.filter.search_placeholder')}
        />
        <select
          className="filter-select"
          value={filterType}
          onChange={(e) => updateUrlFilter('type', e.target.value)}
        >
          <option value="">{t('inventory.filter.type.all')}</option>
          <option value="collector">{t('inventory.filter.type.collector')}</option>
          <option value="sdk">{t('inventory.filter.type.sdk')}</option>
        </select>
        <select
          className="filter-select"
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
        >
          <option value="">{t('inventory.filter.status.all')}</option>
          <option value="connected">{t('inventory.filter.status.connected')}</option>
          <option value="disconnected">{t('inventory.filter.status.disconnected')}</option>
          <option value="degraded">{t('inventory.filter.status.degraded')}</option>
        </select>
        <select
          className="filter-select"
          value={filterControl}
          onChange={(e) => updateUrlFilter('control', e.target.value)}
        >
          <option value="">{t('inventory.filter.control.all')}</option>
          <option value="supervised">{t('inventory.filter.control.supervised')}</option>
          <option value="readonly">{t('inventory.filter.control.readonly')}</option>
        </select>
        <label className="filter-check">
          <input
            type="checkbox"
            checked={includeArchived}
            onChange={(e) => setIncludeArchived(e.target.checked)}
          />
          <span>{t('inventory.filter.include_archived')}</span>
          {includeArchived && archivedCount > 0 && (
            <span className="filter-check-count">{archivedCount}</span>
          )}
        </label>
      </div>

      {isLoading ? (
        <div className="loading">{t('common.loading')}</div>
      ) : filtered.length === 0 ? (
        <div className="empty-state">{t('inventory.empty')}</div>
      ) : (
        filtered.map((w) => <WorkloadCard key={w.id} workload={w} />)
      )}
    </div>
  )
}
