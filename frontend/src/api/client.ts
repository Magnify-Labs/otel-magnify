import axios from 'axios'
import type {
  APIErrorDetails,
  APIErrorResponse,
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
  ConfigApplicationPlan,
  ConfigApplicationPlanExportFormat,
  ValidationResult,
  PushActivityPoint,
  PushGroup,
  PushPreview,
  PushPreviewRequest,
  MeResponse,
  UserPreferences,
  RollbackPrepareResponse,
  RollbackActionResponse,
  RollbackStatusReport,
  OTelConfigDiffRequest,
  OTelConfigDiffResponse,
  CanarySelection,
  CanaryStatus,
  CanaryValidationResult,
  ConfigDriftDashboard,
  FleetVersionIntelligence,
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

export function isAPIErrorResponse(value: unknown): value is APIErrorResponse {
  return (
    isRecord(value) &&
    (typeof value.error === 'string' ||
      typeof value.code === 'string' ||
      Array.isArray(value.validation_errors))
  )
}

export function getAPIErrorDetails(err: unknown, fallback = 'Request failed'): APIErrorDetails {
  if (axios.isAxiosError<APIErrorResponse>(err)) {
    const data = err.response?.data
    if (isAPIErrorResponse(data)) {
      return {
        status: err.response?.status,
        message: data.error ?? err.message ?? fallback,
        code: data.code,
        validation_errors: data.validation_errors,
      }
    }
    return { status: err.response?.status, message: err.message || fallback }
  }
  return { message: fallback }
}

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
  planConfig: (id: string, yaml: string) =>
    api
      .post<ConfigApplicationPlan>(`/workloads/${id}/config/plan`, yaml, {
        headers: { 'Content-Type': 'text/yaml' },
      })
      .then((r) => r.data),
  exportConfigPlanMarkdown: (id: string, yaml: string) =>
    api
      .post<Blob>(`/workloads/${id}/config/plan/export`, yaml, {
        headers: { 'Content-Type': 'text/yaml' },
        params: { format: 'markdown' satisfies ConfigApplicationPlanExportFormat },
        responseType: 'blob',
      })
      .then((r) => r.data),
  exportConfigPlanJson: (id: string, yaml: string) =>
    api
      .post<ConfigApplicationPlan>(`/workloads/${id}/config/plan/export`, yaml, {
        headers: { 'Content-Type': 'text/yaml' },
        params: { format: 'json' satisfies ConfigApplicationPlanExportFormat },
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
  validateCanary: (id: string, config: string, selection: CanarySelection) =>
    api
      .post<CanaryValidationResult>(`/workloads/${id}/config/canary/validate`, {
        config,
        selection,
      })
      .then((r) => r.data),
  startCanary: (id: string, config: string, selection: CanarySelection) =>
    api
      .post<CanaryStatus>(`/workloads/${id}/config/canary`, { config, selection })
      .then((r) => r.data),
  getCanary: (id: string, canaryId: string) =>
    api.get<CanaryStatus>(`/workloads/${id}/config/canary/${canaryId}`).then((r) => r.data),
  promoteCanary: (id: string, canaryId: string) =>
    api
      .post<CanaryStatus>(`/workloads/${id}/config/canary/${canaryId}/promote`)
      .then((r) => r.data),
  abortCanary: (id: string, canaryId: string) =>
    api.post<CanaryStatus>(`/workloads/${id}/config/canary/${canaryId}/abort`).then((r) => r.data),
  rollbackCanary: (id: string, canaryId: string) =>
    api
      .post<CanaryStatus>(`/workloads/${id}/config/canary/${canaryId}/rollback`)
      .then((r) => r.data),
  getRollbackStatus: (id: string, requestId: string) =>
    api
      .get<RollbackStatusReport>(`/workloads/${id}/rollback/status`, {
        params: { request_id: requestId },
      })
      .then((r) => r.data),
  versionIntelligence: (recommendedVersion?: string) =>
    api
      .get<FleetVersionIntelligence>('/workloads/version-intelligence', {
        params: recommendedVersion ? { recommended_version: recommendedVersion } : undefined,
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
  groups: () => api.get<PushGroup[]>('/push-groups').then((r) => r.data ?? []),
  preview: (request: PushPreviewRequest) =>
    api.post<PushPreview>('/pushes/preview', request).then((r) => r.data),
}

export const configSafetyAPI = {
  drift: () => api.get<ConfigDriftDashboard>('/config-safety/drift').then((r) => r.data),
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
