import { NavLink, Outlet, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import type React from 'react'
import { useStore } from '../../store'
import { alertsAPI } from '../../api/client'
import { endClientSession } from '../../api/session'
import type { Capability } from '../../api/capabilitiesContract'
import { hasPerm, type Permission } from '../../lib/perm'
import { useCapabilities } from '../../hooks/useCapability'
import '../../styles/sidebar.css'

function IconDashboard() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <rect x="1.5" y="1.5" width="5" height="5" rx="1" />
      <rect x="9.5" y="1.5" width="5" height="5" rx="1" />
      <rect x="1.5" y="9.5" width="5" height="5" rx="1" />
      <rect x="9.5" y="9.5" width="5" height="5" rx="1" />
    </svg>
  )
}
function IconInventory() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="8" cy="5" r="2.5" />
      <path d="M2.5 15c0-3 2.5-5 5.5-5s5.5 2 5.5 5" />
    </svg>
  )
}
function IconConfigs() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
    >
      <path d="M3 4h10M3 8h7M3 12h5" />
    </svg>
  )
}
function IconDrift() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M2 12h3l2-8 2 8h5" />
      <path d="M11.5 4.5 14 2m0 0v2.5M14 2h-2.5" />
    </svg>
  )
}
function IconAlerts() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M8 2L2 13h12L8 2z" />
      <path d="M8 6.5v3" />
      <circle cx="8" cy="11" r="0.5" fill="currentColor" stroke="none" />
    </svg>
  )
}
function IconAudit() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M8 1.5 3 3.5v4.2c0 2.9 2 5.4 5 6.8 3-1.4 5-3.9 5-6.8V3.5L8 1.5z" />
      <path d="M5.5 7.5h5M5.5 10h3" />
    </svg>
  )
}
function IconProfile() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
    >
      <circle cx="8" cy="6" r="2.5" />
      <path d="M3 14c0-2.5 2-4 5-4s5 1.5 5 4" />
    </svg>
  )
}
function IconAdmin() {
  return (
    <svg
      className="nav-icon"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M8 1.5L2.5 4v4.5c0 3 2.5 5.5 5.5 6 3-0.5 5.5-3 5.5-6V4L8 1.5z" />
    </svg>
  )
}

type FleetNavItem = {
  path: string
  key: string
  Icon: () => React.ReactNode
  end: boolean
  perm?: Permission
  capability?: string
}

const emptyCapabilities: ReadonlyMap<string, Capability> = new Map()

const fleetNav: FleetNavItem[] = [
  { path: '/', key: 'dashboard', Icon: IconDashboard, end: true },
  { path: '/inventory', key: 'inventory', Icon: IconInventory, end: false },
  {
    path: '/config-safety/drift',
    key: 'config_drift',
    Icon: IconDrift,
    end: false,
    capability: 'config_safety.drift_dashboard',
  },
  { path: '/configs', key: 'configs', Icon: IconConfigs, end: false },
  { path: '/alerts', key: 'alerts', Icon: IconAlerts, end: false },
  {
    path: '/audit',
    key: 'audit',
    Icon: IconAudit,
    end: false,
    perm: 'audit:view',
    capability: 'audit.viewer',
  },
]

function initials(email: string): string {
  const left = email.split('@')[0] ?? ''
  return left.slice(0, 2).toUpperCase()
}

function IdentityCard() {
  const navigate = useNavigate()
  const me = useStore((s) => s.me)
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  if (!me) return null

  const handleLogout = () => {
    endClientSession()
    navigate('/login')
  }

  return (
    <div className="identity-card">
      <button
        className="identity-trigger"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <div className="identity-avatar" aria-hidden>
          {initials(me.email)}
        </div>
        <div className="identity-body">
          <div className="identity-email">{me.email}</div>
          <div className="identity-groups">
            {me.groups.map((g) => g.name).join(' · ') || t('account.no_group')}
          </div>
        </div>
        <span className="identity-chevron" aria-hidden>
          ▸
        </span>
      </button>
      {open && (
        <div role="menu" className="identity-popover">
          <button className="identity-menu-item" onClick={handleLogout}>
            {t('account.logout')}
          </button>
        </div>
      )}
    </div>
  )
}

export default function Layout() {
  const { t } = useTranslation()
  const { data: alerts } = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })
  const alertCount = alerts?.length ?? 0
  const me = useStore((s) => s.me)

  const canAdmin = hasPerm(me?.groups, 'users:manage')
  const { data: capabilities = emptyCapabilities, isError: capabilitiesError } = useCapabilities()
  const visibleFleetNav = fleetNav.filter(
    (item) =>
      (!item.perm || hasPerm(me?.groups, item.perm)) &&
      (!item.capability || capabilities.get(item.capability)?.state === 'enabled'),
  )

  return (
    <div className="app-layout">
      <aside className="sidebar">
        <div className="sidebar-logo">
          <div className="sidebar-logo-name">
            otel<span>-magnify</span>
          </div>
          <div className="sidebar-logo-sub">{t('sidebar.subtitle')}</div>
          <span className="sidebar-signal-dot" aria-hidden />
          <span className="sidebar-signal-bar" aria-hidden />
        </div>

        <nav>
          <div className="sidebar-section-label">{t('sidebar.section.fleet')}</div>
          <ul className="sidebar-nav">
            {visibleFleetNav.map(({ path, key, Icon, end }) => (
              <li key={path} className="sidebar-nav-item">
                <NavLink to={path} end={end}>
                  <Icon />
                  <span>{t(`sidebar.nav.${key}`)}</span>
                  {key === 'alerts' && alertCount > 0 && (
                    <span className="sidebar-badge">{alertCount}</span>
                  )}
                </NavLink>
              </li>
            ))}
          </ul>

          <div className="sidebar-section-label">{t('sidebar.section.account')}</div>
          <ul className="sidebar-nav">
            <li className="sidebar-nav-item">
              <NavLink to="/profile">
                <IconProfile />
                <span>{t('sidebar.nav.profile')}</span>
              </NavLink>
            </li>
            {canAdmin && (
              <li className="sidebar-nav-item">
                <NavLink to="/admin">
                  <IconAdmin />
                  <span>{t('sidebar.nav.administration')}</span>
                </NavLink>
              </li>
            )}
          </ul>
        </nav>

        <IdentityCard />

        <div className="sidebar-footer">
          <span className="sidebar-footer-dot" aria-hidden />
          {t('sidebar.footer.live')} · v{__APP_VERSION__}
        </div>
      </aside>

      <main className="main-content">
        {capabilitiesError && (
          <div className="banner banner-error" role="alert">
            {t('sidebar.capabilities_error')}
          </div>
        )}
        <Outlet />
      </main>
    </div>
  )
}
