import { useState } from 'react'
import { Navigate, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useStore } from '../../../store'
import { hasPerm } from '../../../lib/perm'
import { useCapability } from '../../../hooks/useCapability'
import { adminSSOAPI, type SSOProviderInput, type SystemGroupName } from '../../../api/admin'
import { adminSSOKeys } from '../../../api/queryKeys'
import MetadataInput from '../../../components/admin/MetadataInput'
import '../../../styles/admin-sso.css'

const SYSTEM_GROUPS: SystemGroupName[] = ['viewer', 'editor', 'administrator']

const emptyForm: SSOProviderInput = {
  id: '',
  type: 'saml',
  display_name: '',
  idp_metadata_url: '',
  idp_metadata_xml: '',
  sp_entity_id: '',
  allow_idp_initiated: false,
  default_groups: [],
  active: true,
}

export default function ProviderEdit() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const params = useParams<{ id?: string }>()
  const isEdit = Boolean(params.id) && params.id !== 'new'
  const id = isEdit ? (params.id as string) : ''

  const me = useStore((s) => s.me)
  const { enabled: ssoEnabled, isLoading: featuresLoading } = useCapability('sso.admin')

  const [form, setForm] = useState<SSOProviderInput>(emptyForm)
  const [error, setError] = useState<string | null>(null)

  // Hooks must be called unconditionally. Gate via `enabled:` instead of
  // placing them after early-return guards — avoids "Rendered more hooks"
  // errors when `me` transitions from null to a non-admin user.
  const detail = useQuery({
    queryKey: adminSSOKeys.provider(id),
    queryFn: () => adminSSOAPI.getProvider(id),
    enabled: isEdit && Boolean(me) && hasPerm(me?.groups, 'settings:manage') && ssoEnabled,
  })

  // Hydrate the form from the fetched provider once per loaded payload.
  // Tracking the synced reference in state (instead of a syncing effect) lets
  // React reconcile the new local state during the same render that produced
  // it, which is the recommended pattern for adjusting state from props.
  // See https://react.dev/reference/react/useState#storing-information-from-previous-renders
  const [syncedFromData, setSyncedFromData] = useState<typeof detail.data | null>(null)
  if (detail.data && detail.data !== syncedFromData) {
    setSyncedFromData(detail.data)
    setForm({
      id: detail.data.id,
      type: detail.data.type,
      display_name: detail.data.display_name,
      idp_metadata_url: detail.data.idp_metadata_url,
      idp_metadata_xml: detail.data.idp_metadata_xml,
      sp_entity_id: detail.data.sp_entity_id,
      allow_idp_initiated: detail.data.allow_idp_initiated,
      default_groups: detail.data.default_groups ?? [],
      active: detail.data.active,
    })
  }

  const create = useMutation({
    mutationFn: (p: SSOProviderInput) => adminSSOAPI.createProvider(p),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: adminSSOKeys.providers() })
      navigate('/admin/sso/providers')
    },
    onError: (err: { response?: { status?: number; data?: { message?: string } } }) => {
      setError(err.response?.data?.message ?? t('admin.sso.error.generic'))
    },
  })

  const update = useMutation({
    mutationFn: (p: SSOProviderInput) => {
      // Strip `id` from the payload — the ID is already in the URL path parameter.
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { id: _id, ...rest } = p
      return adminSSOAPI.updateProvider(id, rest)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: adminSSOKeys.providers() })
      navigate('/admin/sso/providers')
    },
    onError: (err: { response?: { status?: number; data?: { message?: string } } }) => {
      setError(err.response?.data?.message ?? t('admin.sso.error.generic'))
    },
  })

  if (!me) return null
  if (!hasPerm(me.groups, 'settings:manage')) return <Navigate to="/admin" replace />
  if (featuresLoading) return null
  if (!ssoEnabled) return <Navigate to="/admin" replace />

  if (isEdit && detail.isError) {
    return (
      <div className="page-admin-sso">
        <div className="banner banner-error">{t('admin.sso.error.not_found')}</div>
        <button onClick={() => navigate('/admin/sso/providers')}>{t('common.back')}</button>
      </div>
    )
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setError(null)
    if (isEdit) update.mutate(form)
    else create.mutate(form)
  }

  const toggleDefaultGroup = (g: SystemGroupName) => {
    setForm((f) =>
      f.default_groups.includes(g)
        ? { ...f, default_groups: f.default_groups.filter((x) => x !== g) }
        : { ...f, default_groups: [...f.default_groups, g] },
    )
  }

  const autofillSPEntityID = () => {
    const slug = form.id || '<id>'
    setForm((f) => ({
      ...f,
      sp_entity_id: `${window.location.origin}/api/auth/sso/${slug}/metadata`,
    }))
  }

  return (
    <div className="page-admin-sso">
      <h2>{isEdit ? t('admin.sso.edit.title.edit') : t('admin.sso.edit.title.new')}</h2>

      {error && (
        <div className="banner banner-error" role="alert">
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit} className="profile-form" data-testid="provider-form">
        <div className="field">
          <label className="field-label" htmlFor="sso-id">
            {t('admin.sso.field.id')}
          </label>
          <input
            id="sso-id"
            className="field-input"
            type="text"
            value={form.id}
            onChange={(e) => setForm((f) => ({ ...f, id: e.target.value }))}
            disabled={isEdit}
            placeholder="okta-main"
            pattern="^[a-z0-9-]{1,64}$"
            required
          />
          <small className="muted">{t('admin.sso.field.id.help')}</small>
        </div>

        <div className="field">
          <label className="field-label" htmlFor="sso-display">
            {t('admin.sso.field.display_name')}
          </label>
          <input
            id="sso-display"
            className="field-input"
            type="text"
            value={form.display_name}
            onChange={(e) => setForm((f) => ({ ...f, display_name: e.target.value }))}
            required
          />
        </div>

        <MetadataInput
          metadataURL={form.idp_metadata_url}
          metadataXML={form.idp_metadata_xml}
          onChange={({ metadataURL, metadataXML }) =>
            setForm((f) => ({ ...f, idp_metadata_url: metadataURL, idp_metadata_xml: metadataXML }))
          }
        />

        <div className="field">
          <label className="field-label" htmlFor="sso-spid">
            {t('admin.sso.field.sp_entity_id')}
          </label>
          <input
            id="sso-spid"
            className="field-input"
            type="url"
            value={form.sp_entity_id}
            onChange={(e) => setForm((f) => ({ ...f, sp_entity_id: e.target.value }))}
            required
          />
          <button type="button" onClick={autofillSPEntityID}>
            {t('admin.sso.field.sp_entity_id.fill')}
          </button>
        </div>

        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={form.allow_idp_initiated}
              onChange={(e) => setForm((f) => ({ ...f, allow_idp_initiated: e.target.checked }))}
            />
            {t('admin.sso.field.allow_idp_initiated')}
          </label>
        </div>

        <fieldset className="field">
          <legend className="field-label">{t('admin.sso.field.default_groups')}</legend>
          {SYSTEM_GROUPS.map((g) => (
            <label key={g}>
              <input
                type="checkbox"
                checked={form.default_groups.includes(g)}
                onChange={() => toggleDefaultGroup(g)}
              />
              {g}
            </label>
          ))}
        </fieldset>

        <div className="field">
          <label>
            <input
              type="checkbox"
              checked={form.active}
              onChange={(e) => setForm((f) => ({ ...f, active: e.target.checked }))}
            />
            {t('admin.sso.field.active')}
          </label>
        </div>

        <div className="form-actions">
          <button type="button" className="btn" onClick={() => navigate('/admin/sso/providers')}>
            {t('common.cancel')}
          </button>
          <button
            type="submit"
            className="btn btn-primary"
            disabled={create.isPending || update.isPending}
          >
            {t('common.save')}
          </button>
        </div>
      </form>

      {isEdit && <MappingsSection providerID={id} />}
    </div>
  )
}

function MappingsSection({ providerID }: { providerID: string }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const list = useQuery({
    queryKey: adminSSOKeys.mappings(providerID),
    queryFn: () => adminSSOAPI.listMappings(providerID),
  })

  const create = useMutation({
    mutationFn: (m: { idp_group: string; system_group: SystemGroupName }) =>
      adminSSOAPI.createMapping(providerID, m),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminSSOKeys.mappings(providerID) }),
  })

  const remove = useMutation({
    mutationFn: (m: { idp_group: string; system_group: SystemGroupName }) =>
      adminSSOAPI.deleteMapping(providerID, m),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: adminSSOKeys.mappings(providerID) }),
  })

  const [idpGroup, setIdpGroup] = useState('')
  const [sysGroup, setSysGroup] = useState<SystemGroupName>('viewer')

  const handleAdd = (e: React.FormEvent) => {
    e.preventDefault()
    if (!idpGroup) return
    create.mutate({ idp_group: idpGroup, system_group: sysGroup })
    setIdpGroup('')
  }

  return (
    <section className="profile-section" data-testid="mappings-section">
      <h3>{t('admin.sso.mappings.title')}</h3>

      {list.data && list.data.length === 0 && (
        <p className="empty-state">{t('admin.sso.mappings.empty')}</p>
      )}

      {list.data && list.data.length > 0 && (
        <table className="admin-table" data-testid="mappings-table">
          <thead>
            <tr>
              <th>{t('admin.sso.mappings.idp_group')}</th>
              <th>{t('admin.sso.mappings.system_group')}</th>
              <th>{t('admin.sso.col.actions')}</th>
            </tr>
          </thead>
          <tbody>
            {list.data.map((m) => (
              <tr key={`${m.idp_group}-${m.system_group}`}>
                <td>
                  <code>{m.idp_group}</code>
                </td>
                <td>{m.system_group}</td>
                <td>
                  <button
                    onClick={() =>
                      remove.mutate({ idp_group: m.idp_group, system_group: m.system_group })
                    }
                  >
                    {t('common.delete')}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <form onSubmit={handleAdd} className="profile-form">
        <div className="field">
          <label className="field-label" htmlFor="map-idp">
            {t('admin.sso.mappings.idp_group')}
          </label>
          <input
            id="map-idp"
            className="field-input"
            type="text"
            value={idpGroup}
            onChange={(e) => setIdpGroup(e.target.value)}
            required
          />
        </div>
        <div className="field">
          <label className="field-label" htmlFor="map-sys">
            {t('admin.sso.mappings.system_group')}
          </label>
          <select
            id="map-sys"
            className="field-input"
            value={sysGroup}
            onChange={(e) => setSysGroup(e.target.value as SystemGroupName)}
          >
            {SYSTEM_GROUPS.map((g) => (
              <option key={g} value={g}>
                {g}
              </option>
            ))}
          </select>
        </div>
        <button type="submit" className="btn btn-primary" disabled={create.isPending}>
          {t('admin.sso.mappings.add')}
        </button>
      </form>
    </section>
  )
}
