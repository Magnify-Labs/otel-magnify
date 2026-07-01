export type PushStatus =
  | 'validated'
  | 'pending'
  | 'submitted'
  | 'sent'
  | 'applying'
  | 'applied'
  | 'failed'
  | 'rollback_started'
  | 'rollback_applied'
  | 'rollback_failed'

export type InstancePushStatus = 'sent' | 'applying' | 'applied' | 'failed' | 'no_status'

export type FingerprintSource = 'k8s' | 'host' | 'uid'

export interface WorkloadConfigTimelineEntry {
  state: PushStatus
  at?: string
  message?: string
  terminal: boolean
  timed_out?: boolean
}

export interface WorkloadConfigInstanceStatus {
  instance_uid: string
  pod_name?: string
  node?: string
  required: boolean
  status: InstancePushStatus
  config_hash?: string
  updated_at?: string
  error_cause?: string
  error_message?: string
}

export interface WorkloadConfigErrorGroup {
  cause: string
  title: string
  severity: 'high' | 'medium' | 'low' | string
  count: number
  affected_instances?: string[]
  first_seen_at?: string
  last_seen_at?: string
  sample_message?: string
  sample_path?: string
  config_hash?: string
  retryable: boolean
}

export interface RemoteConfigStatus {
  status: PushStatus
  config_hash: string
  error_message?: string
  updated_at: string
  push_status?: WorkloadConfig
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
  current_config_push?: WorkloadConfig
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
  config_hash?: string
  applied_at: string
  status: PushStatus
  error_message?: string
  pushed_by?: string
  content?: string
  label?: string
  is_current?: boolean
  is_previous?: boolean
  is_last_known_good?: boolean
  is_failed_candidate?: boolean
  content_available?: boolean
  push_id?: string
  submitted_at?: string
  sent_at?: string
  updated_at?: string
  opamp_status_timeout_at?: string
  timed_out_waiting_for_opamp_status?: boolean
  timeout_message?: string
  rollback_of_push_id?: string
  timeline?: WorkloadConfigTimelineEntry[]
  instance_statuses?: WorkloadConfigInstanceStatus[]
  target_count?: number
  applied_count?: number
  failed_count?: number
  pending_count?: number
  error_groups?: WorkloadConfigErrorGroup[]
}

export interface WorkloadKnownGoodConfig {
  workload_id: string
  config_id: string
  marked_at: string
  marked_by?: string
  source_applied_at?: string
  replaced_config_id?: string
  replace_reason?: string
  content_available: boolean
}

export interface MarkKnownGoodResponse {
  changed: boolean
  replaced_config_id?: string
  known_good: WorkloadKnownGoodConfig
}

export interface DefaultRollbackResponse {
  status: string
  config_hash: string
  target_kind: 'last_known_good' | 'previous' | string
}

export type RollbackValidationStatus = 'valid' | 'valid_with_warnings' | 'invalid' | 'unavailable'
export type ValidationSeverity = 'error' | 'warning' | 'info'
export type RollbackApplyStatus = 'accepted' | 'applying' | 'applied' | 'failed' | 'unknown'
export type RollbackTerminalStatus = 'applied' | 'failed' | 'unknown' | 'request_failed'

export interface RollbackConfigMetadata {
  label?: string
  known_good: boolean
  applied_at?: string
  pushed_by?: string
  previous_status?: PushStatus
  error_message?: string
  active_config_id?: string
  active_config_hash?: string
}

export interface RollbackConfigSnapshot {
  hash: string
  content_available: boolean
  content?: string
  content_sha256?: string
  source: 'active_config' | 'history'
  metadata: RollbackConfigMetadata
}

export interface RollbackValidationFinding {
  code: string
  severity: ValidationSeverity
  message: string
  path?: string
  blocking: boolean
  source: 'yaml' | 'capabilities' | 'remote_config' | 'target' | 'concurrency' | 'server'
}

export interface UnavailableComponentWarning {
  category: 'receivers' | 'processors' | 'exporters' | 'extensions' | 'connectors'
  component_id: string
  component_type: string
  path?: string
  available?: string[]
  blocking: true
}

export interface RollbackValidationResult {
  status: RollbackValidationStatus
  valid: boolean
  can_confirm: boolean
  checked_at: string
  validator_version: string
  inputs: {
    workload_id: string
    workload_type: 'collector' | 'sdk'
    accepts_remote_config: boolean
    available_components_hash?: string
    available_components?: AvailableComponents
    target_hash: string
    target_content_sha256?: string
  }
  findings: RollbackValidationFinding[]
  unavailable_components: UnavailableComponentWarning[]
}

export interface RollbackDiffPayload {
  status: 'available' | 'empty' | 'unavailable' | 'error'
  direction: 'current_to_target'
  computation: 'backend_raw' | 'backend_semantic' | 'frontend_raw_inputs_only'
  base_hash?: string
  target_hash: string
  raw_diff?: {
    format: 'unified'
    language: 'yaml'
    base_label: string
    target_label: string
    text: string
    truncated: boolean
  }
  inputs?: {
    current_content_available: boolean
    target_content_available: boolean
    current_yaml?: string
    target_yaml?: string
  }
  message?: string
}

export interface RollbackPrepareResponse {
  schema_version: 'guided-rollback-prepare.v1'
  workload: Pick<
    Workload,
    | 'id'
    | 'display_name'
    | 'type'
    | 'status'
    | 'accepts_remote_config'
    | 'active_config_id'
    | 'active_config_hash'
    | 'remote_config_status'
    | 'available_components'
  >
  target_ref: {
    selector: 'hash' | 'known_good'
    source: 'push_history_row' | 'latest_known_good'
    workload_id: string
    target_hash: string
    known_good: boolean
    known_good_source: 'first_class_marker' | 'label_convention' | 'none'
  }
  current_config: RollbackConfigSnapshot
  target_config: RollbackConfigSnapshot
  diff: RollbackDiffPayload
  validation: RollbackValidationResult
  action: {
    can_submit: boolean
    submit_url: string
    method: 'POST'
    requires_confirmation: true
    confirmation_label: 'Confirm rollback' | 'Confirm rollback with warnings'
    blocking_reasons: RollbackValidationFinding[]
    warnings: RollbackValidationFinding[]
    concurrent_change?: {
      in_progress: boolean
      config_hash?: string
      status?: PushStatus
      message?: string
    }
  }
  status_context: {
    initial_remote_config_status?: RemoteConfigStatus
    timeout_seconds: number
  }
}

export interface RollbackActionResponse {
  schema_version?: 'guided-rollback-action.v1'
  request_id?: string
  status: string
  message?: string
  workload_id?: string
  target_hash?: string
  config_hash: string
  history_row?: WorkloadConfig
  status_url?: string
  timeout_seconds?: number
  audit?: { event: string; emitted: boolean }
}

export interface RollbackStatusReport {
  schema_version: 'guided-rollback-status.v1'
  request_id: string
  workload_id: string
  target_hash: string
  target_label?: string
  request_status: 'accepted' | 'request_failed'
  apply_status: RollbackApplyStatus
  terminal: boolean
  terminal_status?: RollbackTerminalStatus
  started_at: string
  updated_at?: string
  elapsed_ms: number
  timeout_seconds: number
  timed_out: boolean
  history_row?: WorkloadConfig
  remote_config_status?: RemoteConfigStatus
  last_known_status?: PushStatus
  error_message?: string
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

export type OTelDiffRisk = 'none' | 'low' | 'medium' | 'high'
export type OTelDiffChangeKind = 'added' | 'removed' | 'modified' | 'unchanged'
export type OTelComponentCategory =
  | 'receivers'
  | 'processors'
  | 'exporters'
  | 'connectors'
  | 'extensions'
export type OTelSignal = 'traces' | 'metrics' | 'logs' | 'profiles' | 'unknown'

export interface OTelConfigDiffRequest {
  base_yaml: string
  target_yaml: string
  context?: {
    workload_id?: string
    base_label?: string
    target_label?: string
    include_raw_paths?: boolean
  }
}

export interface OTelConfigDiffResponse {
  schema_version: 'otel-config-diff.v1'
  valid: boolean
  summary: OTelDiffSummary
  components: OTelComponentDiff[]
  pipelines: OTelPipelineDiff[]
  endpoints: OTelEndpointDiff[]
  security: OTelSecurityDiff[]
  risk_items: OTelRiskItem[]
  diagnostics: OTelDiffDiagnostic[]
  normalized: {
    base_hash: string
    target_hash: string
    base_component_count: number
    target_component_count: number
    base_pipeline_count: number
    target_pipeline_count: number
  }
}

export interface OTelDiffSummary {
  overall_risk: OTelDiffRisk
  headline: string
  counts: {
    components_added: number
    components_removed: number
    components_modified: number
    pipelines_added: number
    pipelines_removed: number
    pipelines_modified: number
    endpoints_added: number
    endpoints_removed: number
    endpoints_modified: number
    high_risk: number
    medium_risk: number
    low_risk: number
  }
}

export interface OTelComponentRef {
  category: OTelComponentCategory
  id: string
  type: string
  name?: string
  path: string
}

export interface OTelComponentDiff {
  id: string
  kind: OTelDiffChangeKind
  component: OTelComponentRef
  risk: OTelDiffRisk
  title: string
  before?: unknown
  after?: unknown
  changed_fields: OTelFieldChange[]
  impacted_pipelines: string[]
  rules: string[]
}

export interface OTelPipelineDiff {
  id: string
  kind: OTelDiffChangeKind
  pipeline_key: string
  signal: OTelSignal
  risk: OTelDiffRisk
  before?: OTelPipelineShape
  after?: OTelPipelineShape
  component_ref_changes: OTelPipelineRefChange[]
  rules: string[]
}

export interface OTelPipelineShape {
  receivers: string[]
  processors: string[]
  exporters: string[]
}

export interface OTelPipelineRefChange {
  section: 'receivers' | 'processors' | 'exporters'
  component_id: string
  kind: 'added' | 'removed' | 'moved'
  from_index?: number
  to_index?: number
  risk: OTelDiffRisk
  reason?: string
}

export interface OTelEndpointDiff {
  id: string
  kind: OTelDiffChangeKind
  component: OTelComponentRef
  endpoint_kind: 'otlp_grpc' | 'otlp_http' | 'prometheus' | 'jaeger' | 'zipkin' | 'generic'
  field_path: string
  before?: OTelEndpointValue
  after?: OTelEndpointValue
  risk: OTelDiffRisk
  rules: string[]
}

export interface OTelEndpointValue {
  raw: string
  scheme?: string
  host?: string
  port?: number
  path?: string
  normalized: string
  insecure?: boolean
  tls_enabled?: boolean
}

export interface OTelSecurityDiff {
  id: string
  kind: OTelDiffChangeKind | 'weakened' | 'strengthened'
  component?: OTelComponentRef
  path: string
  field: 'tls' | 'insecure' | 'headers' | 'auth' | 'placeholder' | 'secret_like'
  before?: unknown
  after?: unknown
  risk: OTelDiffRisk
  rules: string[]
  message: string
}

export interface OTelRiskItem {
  id: string
  risk: OTelDiffRisk
  category: 'availability' | 'data_loss' | 'security' | 'routing' | 'cost' | 'operability'
  rule: string
  title: string
  description: string
  affected_paths: string[]
  affected_pipelines: string[]
}

export interface OTelFieldChange {
  path: string
  before?: unknown
  after?: unknown
  risk: OTelDiffRisk
}

export interface OTelDiffDiagnostic {
  side: 'base' | 'target' | 'both'
  code: string
  message: string
  path?: string
  severity: 'info' | 'warning' | 'error'
}
