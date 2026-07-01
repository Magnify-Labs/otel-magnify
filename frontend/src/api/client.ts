import axios from 'axios'
import type {
  Workload,
  Instance,
  WorkloadEvent,
  EventsStats,
  Config,
  Alert,
  WorkloadConfig,
  WorkloadKnownGoodConfig,
  MarkKnownGoodResponse,
  DefaultRollbackResponse,
  ValidationResult,
  PushActivityPoint,
  MeResponse,
  UserPreferences,
  RollbackPrepareResponse,
  RollbackActionResponse,
  RollbackStatusReport,
  OTelConfigDiffRequest,
  OTelConfigDiffResponse,
} from '../types'

declare module 'axios' {
  export interface AxiosRequestConfig {
    // When true, the 401 response interceptor skips the logout + redirect to /login.
    // Use for endpoints where 401 is a business-logic signal (e.g. PUT /api/me/password
    // returning 401 when current_password is wrong) rather than a session-expired signal.
    skipAuthRedirect?: boolean
  }
  export interface InternalAxiosRequestConfig {
    skipAuthRedirect?: boolean
  }
}

export type AuthMethod = {
  id: string
  type: 'password' | 'sso'
  display_name: string
  login_url: string
}

const api = axios.create({ baseURL: '/api' })

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401 && !err.config?.skipAuthRedirect) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  },
)

export const workloadsAPI = {
  list: (includeArchived = false) =>
    api
      .get<Workload[]>('/workloads', { params: { include_archived: includeArchived } })
      .then((r) => r.data ?? []),
  get: (id: string) => api.get<Workload>(`/workloads/${id}`).then((r) => r.data),
  instances: (id: string) =>
    api.get<Instance[]>(`/workloads/${id}/instances`).then((r) => r.data ?? []),
  events: (id: string, params?: { limit?: number; since?: string }) =>
    api.get<WorkloadEvent[]>(`/workloads/${id}/events`, { params }).then((r) => r.data ?? []),
  eventsStats: (id: string, window = '24h') =>
    api
      .get<EventsStats>(`/workloads/${id}/events/stats`, { params: { window } })
      .then((r) => r.data),
  pushConfig: (id: string, yaml: string) =>
    api
      .post<WorkloadConfig>(`/workloads/${id}/config`, yaml, {
        headers: { 'Content-Type': 'text/yaml' },
      })
      .then((r) => r.data),
  validateConfig: (id: string, yaml: string) =>
    api
      .post<ValidationResult>(`/workloads/${id}/config/validate`, yaml, {
        headers: { 'Content-Type': 'text/yaml' },
      })
      .then((r) => r.data),
  getConfigHistory: (id: string) =>
    api.get<WorkloadConfig[]>(`/workloads/${id}/configs`).then((r) => r.data ?? []),
  getConfigByHash: (id: string, hash: string) =>
    api.get<WorkloadConfig>(`/workloads/${id}/configs/${hash}`).then((r) => r.data),
  getKnownGood: (id: string) =>
    api.get<WorkloadKnownGoodConfig>(`/workloads/${id}/known-good`).then((r) => r.data),
  markKnownGood: (
    id: string,
    hash: string,
    options: { replaceReason?: string; ifCurrentKnownGood?: string; force?: boolean } = {},
  ) =>
    api
      .post<MarkKnownGoodResponse>(`/workloads/${id}/configs/${hash}/known-good`, {
        replace_reason: options.replaceReason ?? '',
        if_current_known_good: options.ifCurrentKnownGood,
        force: options.force ?? false,
      })
      .then((r) => r.data),
  clearKnownGood: (id: string) => api.delete(`/workloads/${id}/known-good`),
  setConfigLabel: (id: string, hash: string, label: string) =>
    api
      .post<{ label: string }>(`/workloads/${id}/configs/${hash}/label`, { label })
      .then((r) => r.data),
  prepareRollback: (id: string, targetHash: string) =>
    api
      .get<RollbackPrepareResponse>(`/workloads/${id}/rollback/prepare`, {
        params: { target_hash: targetHash },
      })
      .then((r) => r.data),
  rollbackConfig: (id: string, hash: string) =>
    api
      .post<RollbackActionResponse>(`/workloads/${id}/configs/${hash}/rollback`)
      .then((r) => r.data),
  rollbackDefault: (id: string) =>
    api.post<DefaultRollbackResponse>(`/workloads/${id}/rollback`).then((r) => r.data),
  getRollbackStatus: (id: string, requestId: string) =>
    api
      .get<RollbackStatusReport>(`/workloads/${id}/rollback/status`, {
        params: { request_id: requestId },
      })
      .then((r) => r.data),
  delete: (id: string) => api.delete(`/workloads/${id}`),
}

export const configsAPI = {
  list: () => api.get<Config[]>('/configs').then((r) => r.data ?? []),
  get: (id: string) => api.get<Config>(`/configs/${id}`).then((r) => r.data),
  create: (name: string, content: string) =>
    api.post<Config>('/configs', { name, content }).then((r) => r.data),
  diff: (request: OTelConfigDiffRequest) =>
    api.post<OTelConfigDiffResponse>('/configs/diff', request).then((r) => r.data),
}

export const alertsAPI = {
  list: (includeResolved = false) =>
    api
      .get<Alert[]>('/alerts', { params: { include_resolved: includeResolved } })
      .then((r) => r.data ?? []),
  resolve: (id: string) => api.post(`/alerts/${id}/resolve`),
}

export const pushesAPI = {
  activity: (window: '7d' = '7d') =>
    api
      .get<PushActivityPoint[]>('/pushes/activity', { params: { window } })
      .then((r) => r.data ?? []),
}

export const authAPI = {
  login: (email: string, password: string) =>
    api.post<{ token: string }>('/auth/login', { email, password }).then((r) => r.data),
  getMethods: () => api.get<{ methods: AuthMethod[] }>('/auth/methods').then((r) => r.data.methods),
}

export const meAPI = {
  get: () => api.get<MeResponse>('/me').then((r) => r.data),
  changePassword: (current: string, next: string) =>
    api.put(
      '/me/password',
      { current_password: current, new_password: next },
      { skipAuthRedirect: true },
    ),
  updatePreferences: (prefs: Pick<UserPreferences, 'theme' | 'language'>) =>
    api.put<UserPreferences>('/me/preferences', prefs).then((r) => r.data),
}

export const workloadsArchiveAPI = {
  archive: (id: string) => api.post(`/workloads/${id}/archive`),
}

export default api
