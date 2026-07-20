import api from './client'

export type SSOProviderType = 'saml'

export type SystemGroupName = 'viewer' | 'editor' | 'administrator'

export type SSOProvider = {
  id: string
  type: SSOProviderType
  display_name: string
  idp_metadata_url: string
  idp_metadata_xml: string
  sp_entity_id: string
  allow_idp_initiated: boolean
  default_groups: string[]
  active: boolean
  created_at: string
  updated_at: string
}

export type SSOProviderInput = Omit<SSOProvider, 'created_at' | 'updated_at'>

export type SSOMapping = {
  provider_id: string
  idp_group: string
  system_group: SystemGroupName
  created_at: string
}

export const adminSSOAPI = {
  listProviders: () => api.get<SSOProvider[]>('/admin/sso/providers').then((r) => r.data ?? []),
  getProvider: (id: string) =>
    api.get<SSOProvider>(`/admin/sso/providers/${id}`).then((r) => r.data),
  createProvider: (p: SSOProviderInput) =>
    api.post<SSOProvider>('/admin/sso/providers', p).then((r) => r.data),
  updateProvider: (id: string, p: Omit<SSOProviderInput, 'id'>) =>
    api.put<SSOProvider>(`/admin/sso/providers/${id}`, p).then((r) => r.data),
  deleteProvider: (id: string) => api.delete(`/admin/sso/providers/${id}`),
  setActive: (id: string, active: boolean) =>
    api.patch(`/admin/sso/providers/${id}/active`, { active }),

  listMappings: (id: string) =>
    api.get<SSOMapping[]>(`/admin/sso/providers/${id}/mappings`).then((r) => r.data ?? []),
  createMapping: (id: string, m: Pick<SSOMapping, 'idp_group' | 'system_group'>) =>
    api.post<SSOMapping>(`/admin/sso/providers/${id}/mappings`, m).then((r) => r.data),
  deleteMapping: (id: string, m: Pick<SSOMapping, 'idp_group' | 'system_group'>) =>
    api.delete(`/admin/sso/providers/${id}/mappings`, { data: m }),
}
