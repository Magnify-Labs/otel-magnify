import { Navigate, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useStore } from '../../../store'
import { hasPerm } from '../../../lib/perm'
import { useCapability } from '../../../hooks/useCapability'
import { adminSSOAPI, type SSOProvider } from '../../../api/admin'
import { adminSSOKeys } from '../../../api/queryKeys'
import '../../../styles/admin-sso.css'

export default function Providers() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const me = useStore((s) => s.me)

  const { enabled: ssoEnabled, isLoading: featuresLoading } = useCapability('sso.admin')

  // Hooks must be called unconditionally. Gate via `enabled:` instead of
  // placing them after early-return guards — avoids "Rendered more hooks"
  // errors when `me` transitions from null to a non-admin user.
  const list = useQuery({
    queryKey: adminSSOKeys.providers(),
    queryFn: adminSSOAPI.listProviders,
    enabled: Boolean(me) && hasPerm(me?.groups, 'settings:manage') && ssoEnabled,
  })

  const setActive = useMutation({
    mutationFn: ({ id, active }: { id: string; active: boolean }) =>
      adminSSOAPI.setActive(id, active),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminSSOKeys.providers() }),
  })

  const remove = useMutation({
    mutationFn: (id: string) => adminSSOAPI.deleteProvider(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminSSOKeys.providers() }),
  })

  if (!me) return null
  if (!hasPerm(me.groups, 'settings:manage')) return <Navigate to="/admin" replace />
  if (featuresLoading) return null
  if (!ssoEnabled) return <Navigate to="/admin" replace />

  const handleDelete = (p: SSOProvider) => {
    if (window.confirm(t('admin.sso.delete.confirm', { id: p.id }))) {
      remove.mutate(p.id)
    }
  }

  return (
    <div className="page-admin-sso">
      <header className="page-header">
        <h2>{t('admin.sso.title')}</h2>
        <button className="btn btn-primary" onClick={() => navigate('/admin/sso/providers/new')}>
          {t('admin.sso.new')}
        </button>
      </header>

      {list.isLoading && <p>{t('common.loading')}</p>}
      {list.isError && (
        <div className="banner banner-error" role="alert">
          {t('admin.sso.error.generic')}
          <button onClick={() => list.refetch()}>{t('common.retry')}</button>
        </div>
      )}
      {list.data && list.data.length === 0 && <p className="empty-state">{t('admin.sso.empty')}</p>}
      {list.data && list.data.length > 0 && (
        <table className="admin-table" data-testid="providers-table">
          <thead>
            <tr>
              <th>{t('admin.sso.col.display_name')}</th>
              <th>{t('admin.sso.col.type')}</th>
              <th>{t('admin.sso.col.active')}</th>
              <th>{t('admin.sso.col.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {list.data.map((p) => (
              <tr key={p.id} data-testid={`provider-row-${p.id}`}>
                <td>
                  <strong>{p.display_name}</strong>
                  <div className="muted">{p.id}</div>
                </td>
                <td>{p.type.toUpperCase()}</td>
                <td>
                  <input
                    type="checkbox"
                    checked={p.active}
                    onChange={(e) => setActive.mutate({ id: p.id, active: e.target.checked })}
                    aria-label={t('admin.sso.col.active')}
                    disabled={setActive.isPending}
                  />
                </td>
                <td>
                  <button onClick={() => navigate(`/admin/sso/providers/${p.id}`)}>
                    {t('common.edit')}
                  </button>
                  <button onClick={() => handleDelete(p)} className="btn btn-danger">
                    {t('common.delete')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
