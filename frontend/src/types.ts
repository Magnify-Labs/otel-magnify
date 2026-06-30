export type PushStatus = 'pending' | 'applying' | 'applied' | 'failed'

export type FingerprintSource = 'k8s' | 'host' | 'uid'

export interface RemoteConfigStatus {
  status: PushStatus
  config_hash: string
  error_message?: string
  updated_at: string
}

export interface AvailableComponents {
  components: Record<string, string[]>
  hash?: string
}

export interface Workload {
  id: string
  fingerprint_source: FingerprintSource
  fingerprint_keys: Record<string, string>
  display_name: string
  type: 'collector' | 'sdk'
  version: string
  status: 'connected' | 'disconnected' | 'degraded'
  last_seen_at: string
  labels: Record<string, string>
  active_config_id?: string
  active_config_hash?: string
  remote_config_status?: RemoteConfigStatus
  available_components?: AvailableComponents
  accepts_remote_config?: boolean
  retention_until?: string
  archived_at?: string
}

export interface Instance {
  instance_uid: string
  pod_name?: string
  version?: string
  connected_at: string
  last_message_at: string
  effective_config_hash?: string
  healthy: boolean
}

export interface WorkloadEvent {
  id: number
  workload_id: string
  instance_uid: string
  pod_name?: string
  event_type: 'connected' | 'disconnected' | 'version_changed'
  version?: string
  prev_version?: string
  occurred_at: string
}

export interface EventsStats {
  connected: number
  disconnected: number
  version_changed: number
  churn_rate_per_hour: number
}

export interface Config {
  id: string
  name: string
  content: string
  created_at: string
  created_by: string
}

export interface Alert {
  id: string
  workload_id: string
  rule: 'workload_down' | 'config_drift' | 'version_outdated'
  severity: 'warning' | 'critical'
  message: string
  fired_at: string
  resolved_at?: string
}

export interface WorkloadConfig {
  workload_id: string
  config_id: string
  applied_at: string
  status: PushStatus
  error_message?: string
  pushed_by?: string
  content?: string
  label?: string
}

export interface AutoRollbackEvent {
  workload_id: string
  from_hash: string
  to_hash: string
  reason: string
}

export interface ValidationError {
  code: string
  message: string
  path?: string
  check_id?: string
}

export interface ValidationMessage {
  code: string
  severity: 'info' | 'warning' | 'error'
  message: string
  path?: string
  check_id?: string
}

export interface ValidationCheck {
  id: string
  label: string
  source: string
  status: 'passed' | 'warning' | 'failed' | 'skipped'
  required: boolean
  messages?: ValidationMessage[]
  metadata?: Record<string, unknown>
}

export interface ValidationResult {
  valid: boolean
  overall_status?: 'passed' | 'warning' | 'failed' | 'unknown'
  summary?: string
  target_collector_version?: string
  validated_at?: string
  errors?: ValidationError[]
  warnings?: ValidationMessage[]
  checks?: ValidationCheck[]
}

export interface PushActivityPoint {
  day: string
  count: number
}

export interface Group {
  id: string
  name: string
  role: 'viewer' | 'editor' | 'administrator'
  is_system: boolean
  created_at: string
}

export interface UserPreferences {
  user_id: string
  theme: 'light' | 'dark' | 'system'
  language: 'en' | 'fr'
  updated_at: string
}

export interface MeResponse {
  id: string
  email: string
  groups: Group[]
  preferences: UserPreferences
}
