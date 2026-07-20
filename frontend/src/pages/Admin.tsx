import { Navigate, Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useStore } from '../store'
import { hasPerm } from '../lib/perm'
import { useCapability } from '../hooks/useCapability'

export default function Admin() {
  const { t } = useTranslation()
  const me = useStore((s) => s.me)
  const { enabled: ssoAdminEnabled } = useCapability('sso.admin')

  if (!me) return null
  if (!hasPerm(me.groups, 'users:manage')) return <Navigate to="/" replace />

  return (
    <div className="page-profile">
      <h2>{t('admin.title')}</h2>
      <section className="profile-section">
        <h3>{t('admin.sections.title')}</h3>
        <ul className="admin-index">
          {ssoAdminEnabled && hasPerm(me.groups, 'settings:manage') && (
            <li>
              <Link to="/admin/sso/providers">
                <strong>{t('nav.admin.sso')}</strong>
                <p className="muted">{t('admin.sections.sso.description')}</p>
              </Link>
            </li>
          )}
          {!ssoAdminEnabled && hasPerm(me.groups, 'settings:manage') && (
            <li className="muted">
              <em>{t('admin.placeholder')}</em>
            </li>
          )}
        </ul>
      </section>
    </div>
  )
}
