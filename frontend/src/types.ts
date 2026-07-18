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

export type CanaryStatusValue =
  'running' | 'succeeded' | 'promoted' | 'aborted' | 'rollback_started' | 'stopped' | 'failed'

export type CanaryStopReason =
  | 'remote_config_failed'
  | 'collector_degraded'
  | 'no_heartbeat'
  | 'config_drift'
  | 'alert_triggered'
  | string

export interface CanarySelection {
  strategy: 'one' | 'count' | 'n' | 'percentage' | 'label_selector' | 'instances'
  instance_uid?: string
  instance_uids?: string[]
  count?: number
  percentage?: number
  labels?: Record<string, string>
}

export interface CanaryTarget {
  instance_uid: string
  pod_name?: string
  status: InstancePushStatus | PushStatus | string
  stop_reason?: CanaryStopReason
  updated_at?: string
}

export interface CanaryCounts {
  pending: number
  applying: number
  applied: number
  failed: number
}

export interface CanaryValidationResult {
  valid: boolean
  targets: CanaryTarget[]
  stop_reasons?: CanaryStopReason[]
  errors?: string[]
}

export interface CanaryStatus {
  id: string
  workload_id: string
  config_hash: string
  status: CanaryStatusValue
  selection: CanarySelection
  targets: CanaryTarget[]
  counts: CanaryCounts
  stop_reasons?: CanaryStopReason[]
  actor?: string
  created_at: string
  updated_at: string
  promoted_at?: string
  aborted_at?: string
  rolled_back_at?: string
}

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
  timed_out?: boolean
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
  accepts_remote_config?: boolean
  remote_config_capability_known?: boolean
  remote_config_status?: RemoteConfigStatus
}

export interface WorkloadTopologySummary {
  connected_count: number
  healthy_count: number
  unhealthy_count: number
  drifted_count: number
  heterogeneous: boolean
  version_diversity: string[]
  config_hash_diversity: string[]
  remote_config_status_counts: Record<string, number>
  heterogeneity: Record<string, boolean>
  heterogeneity_reasons: string[]
}

export interface WorkloadTopology {
  schema_version: 'workload-topology.v1'
  workload_id: string
  summary: WorkloadTopologySummary
  instances: Instance[]
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

export interface AuditRecord {
  id: string
  occurred_at: string
  action: string
  user_id?: string
  email?: string
  resource?: string
  resource_id?: string
  workload_id?: string
  config_hash?: string
  detail?: string
  prev_hash?: string
  event_hash?: string
  immutable_ref?: string
}

export interface AuditEventFilters {
  user?: string
  user_id?: string
  email?: string
  action?: string
  resource_id?: string
  workload_id?: string
  config_hash?: string
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export interface AuditEventPage {
  available: boolean
  events: AuditRecord[]
  total: number
  limit: number
  offset: number
}

export interface EventsStats {
  connected: number
  disconnected: number
  version_changed: number
  churn_rate_per_hour: number
}

export type ConfigKind = 'saved' | 'template' | 'draft' | 'known_good'

export interface ConfigVariable {
  name: string
  label: string
  type: string
  required: boolean
  description?: string
  placeholder?: string
}

export interface Config {
  id: string
  name: string
  content?: string
  created_at: string
  created_by: string
  kind?: ConfigKind | string
  status?: 'ready' | 'draft' | string
  category?: string
  stack?: string
  description?: string
  variables?: ConfigVariable[]
  tags?: string[]
  built_in?: boolean
  source_type?: 'manual' | 'git' | 'migration_assistant' | string
  git_url?: string
  git_provider?: 'github' | 'gitlab' | 'generic' | string
  git_ref?: string
  git_path?: string
  commit_sha?: string
  imported_at?: string
}

export interface CreateConfigRequest {
  kind?: ConfigKind
  status?: 'ready' | 'draft' | string
  category?: string
  stack?: string
  tags?: string[]
  source_type?: 'manual' | 'git' | 'migration_assistant' | string
}

export type ConfigMigrationVendor =
  'datadog_agent' | 'fluent_bit' | 'splunk_forwarder' | 'new_relic_infra'

export interface ConfigMigrationContext {
  target_signal?: string
  target_exporter?: string
  otlp_endpoint?: string
  collector_distribution?: string
  notes?: string
}

export interface ConfigMigrationPreviewRequest {
  schema_version?: 'config_migration_preview_request.v1'
  vendor: ConfigMigrationVendor
  source: string
  source_format?: string
  labels?: Record<string, string>
  context?: ConfigMigrationContext
}

export interface ConfigMigrationWarning {
  code: string
  severity: string
  message: string
  path?: string
}

export interface ConfigMigrationUnsupportedKey {
  path: string
  reason: string
  suggestion?: string
}

export interface ConfigMigrationEvidence {
  source_path: string
  target_path: string
  rule_id: string
  explanation: string
}

export interface ConfigMigrationRedaction {
  path: string
  placeholder: string
  reason: string
}

export interface ConfigMigrationValidation {
  valid: boolean
  overall_status: string
  summary: string
  validated_at: string
}

export interface ConfigMigrationSaveHint {
  kind: ConfigKind | string
  source_type: 'migration_assistant' | string
  tags: string[]
  category: string
  stack: string
}

export interface ConfigMigrationPreviewResponse {
  schema_version: 'config_migration_preview.v1' | string
  vendor: ConfigMigrationVendor | string
  source_format: string
  draft_yaml: string
  draft_name: string
  confidence: 'low' | 'medium' | 'high' | string
  summary: string
  warnings: ConfigMigrationWarning[]
  unsupported_keys: ConfigMigrationUnsupportedKey[]
  evidence: ConfigMigrationEvidence[]
  redactions: ConfigMigrationRedaction[]
  validation?: ConfigMigrationValidation | null
  save_hint: ConfigMigrationSaveHint
}

export interface GitImportConfigRequest {
  name: string
  git_url: string
  git_ref: string
  git_path: string
}

export interface GitImportConfigResponse {
  config: Config
  validation: ValidationResult
}

export interface GitOpsExportRequest {
  provider: 'github' | 'gitlab'
  repository: string
  path: string
  base_branch: string
  branch: string
  title: string
  body: string
}

export interface GitOpsExportResult {
  provider: string
  url: string
  number: number
  branch: string
  commit_sha: string
}

export interface GitOpsCommentResult {
  provider: string
  url: string
  comment_id: string
}

export interface GitOpsExportResponse {
  result: GitOpsExportResult
  comment?: GitOpsCommentResult
  validation?: ValidationResult
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

export type ConfigApprovalStatus = 'pending' | 'approved' | 'pushed' | string

export interface ConfigApprovalRequest {
  id: string
  workload_id: string
  draft_yaml: string
  target_group: string
  target_env?: string
  requester?: string
  requested_by?: string
  request_comment: string
  approver?: string
  approval_comment?: string
  status: ConfigApprovalStatus
  approved_by?: string
  approved_at?: string
  push_comment?: string
  prod_target: boolean
  prod_confirmation: boolean
  prod_double_confirmed: boolean
  break_glass: boolean
  break_glass_reason?: string
  config_hash?: string
  created_at: string
  updated_at: string
  pushed_at?: string
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

export type ConfigApplicationPlanSchemaVersion = 'config_application_plan.v1'
export type ConfigApplicationPlanExportFormat = 'json' | 'markdown'
export type ConfigApplicationPlanPersistedRolloutStatus = 'not_persisted'

export interface ConfigApplicationPlanSummary {
  target_count: number
  collector_target_count: number
  remote_config_capable_count: number
  read_only_count: number
  validation_ok_count: number
  validation_failed_count: number
  components_missing_count: number
  high_risk_change_count: number
  excluded_count: number
}

export interface ConfigApplicationPlanTarget {
  workload_id: string
  display_name: string
  type: string
  accepts_remote_config: boolean
  read_only: boolean
  validation_status: 'ok' | 'failed' | string
  validation_errors?: string[]
  components_missing_count: number
  high_risk_change_count: number
  excluded: boolean
  exclusion_reasons: string[]
  hard_failures: string[]
  active_config_hash?: string
  active_config_unavailable: boolean
}

export interface ConfigApplicationPlanExport {
  supported: boolean
  formats: ConfigApplicationPlanExportFormat[]
  json_endpoint: string
  markdown_endpoint: string
  persisted_rollout: ConfigApplicationPlanPersistedRolloutStatus
}

export interface ConfigApplicationPlan {
  schema_version: ConfigApplicationPlanSchemaVersion
  workload_id: string
  config_hash: string
  summary: ConfigApplicationPlanSummary
  targets: ConfigApplicationPlanTarget[]
  risk_score?: ConfigRiskScore
  policy?: ConfigPolicyEvaluation | null
  hard_failures: string[]
  can_push: boolean
  apply_allowed: boolean
  export: ConfigApplicationPlanExport
}

export type ConfigRiskScoreSeverity = 'none' | 'low' | 'medium' | 'high' | string

export interface ConfigRiskScore {
  severity: ConfigRiskScoreSeverity
  reasons: string[]
  applies_to_count: number
}

export interface ConfigPolicyTarget {
  environment?: string
  scope?: string
  workload_id?: string
  tenant_id?: string
  team_id?: string
}

export interface ConfigPolicySamplingSettings {
  min_percentage?: number
  max_percentage?: number
}

export interface ConfigPolicySettings {
  allowed_otlp_endpoints?: string[]
  critical_exporters?: string[]
  required_resource_attributes?: string[]
  sampling?: ConfigPolicySamplingSettings
}

export interface ConfigPolicyFinding {
  policy_id: string
  policy_name: string
  rule_id: string
  rule_code: string
  severity: 'info' | 'warning' | 'critical' | string
  decision: 'pass' | 'warn' | 'block' | string
  target_scope?: string
  environment?: string
  path: string
  paths?: string[]
  message: string
  remediation: string
  packaging: 'community' | 'enterprise' | string
  tier: 'core' | 'configurable' | 'tenant_hook' | string
}

export interface ConfigPolicySummary {
  pass_count: number
  warn_count: number
  block_count: number
}

export interface ConfigPolicyAuditMeta {
  persisted: boolean
  event?: string
  reason?: string
}

export interface ConfigPolicyEvaluation {
  schema_version: 'config-policy.v1' | string
  valid: boolean
  allowed: boolean
  decision: 'pass' | 'warn' | 'block' | string
  severity: 'info' | 'warning' | 'critical' | string
  target: ConfigPolicyTarget
  settings?: ConfigPolicySettings
  findings: ConfigPolicyFinding[]
  summary: ConfigPolicySummary
  audit: ConfigPolicyAuditMeta
}

export interface ConfigPolicyPreviewRequest {
  candidate_yaml?: string
  current_yaml?: string
  target_yaml?: string
  base_yaml?: string
  target?: ConfigPolicyTarget
  settings?: ConfigPolicySettings
  context?: {
    environment?: string
    endpoint_allowlist?: string[]
    critical_exporters?: string[]
    required_resource_attributes?: string[]
    max_sampling_percentage?: number
  }
}

export type ReportExportRequestSchemaVersion = 'report_export_request.v1'
export type EvidencePackSchemaVersion = 'evidence_pack.v1'
export type ReportExportFormat = 'markdown' | 'csv' | 'pdf'
export type EvidenceReportType = 'evidence_pack'
export type ReportRedactionMode = 'strict' | 'none'
export type ReportSignatureScheme = 'none' | 'sha256-hmac' | 'ed25519'

export interface ReportScope {
  workload_ids?: string[]
  group_id?: string
  selector?: Record<string, string>
  since?: string
  until?: string
}

export interface ReportIncludeOptions {
  workload_summary: boolean
  config_history: boolean
  current_config: boolean
  config_plan: boolean
  drift_findings: boolean
  version_intelligence: boolean
  alerts: boolean
  workload_events: boolean
  rollback_readiness: boolean
  audit_verification: boolean
  signed_audit_metadata?: boolean
}

export interface ReportExportRequest {
  schema_version: ReportExportRequestSchemaVersion
  report_type: EvidenceReportType
  scope: ReportScope
  include: ReportIncludeOptions
  redaction: ReportRedactionMode
}

export interface ReportScopeResolved {
  workload_ids?: string[]
  workload_count?: number
  group_id?: string
  selector?: Record<string, string>
  since?: string
  until?: string
  requested_scope?: ReportScope
}

export interface EvidenceItem {
  id: string
  resource: string
  resource_id: string
  observed_at?: string
  severity?: string
  summary: string
  facts: Record<string, unknown>
  content_hash?: string
  redacted: boolean
}

export interface EvidenceTable {
  columns: string[]
  rows: string[][]
}

export interface EvidenceSection {
  id: string
  title: string
  order: number
  items: EvidenceItem[]
  csv_table?: EvidenceTable
}

export interface ReportSignature {
  scheme: ReportSignatureScheme
  key_id?: string
  signed_at: string
  payload_hash: string
  signature_b64?: string
  verifier?: string
}

export interface SignedAuditReportMetadata {
  status: string
  verifier: string
  verified_from?: string
  verified_until?: string
  head_hash?: string
  checked_at: string
  first_bad_sequence?: number
}

export interface ReportWarning {
  code: string
  message: string
}

export interface EvidencePack {
  schema_version: EvidencePackSchemaVersion
  generated_at: string
  inputs_hash: string
  report_hash: string
  scope: ReportScopeResolved
  sections: EvidenceSection[]
  signatures?: ReportSignature[]
  signed_audit?: SignedAuditReportMetadata
  warnings?: ReportWarning[]
}

export interface ReportExportErrorResponse extends APIErrorResponse {
  fallback_format?: ReportExportFormat
}

export const REPORT_EXPORT_CSV_COLUMNS = [
  'section_id',
  'item_id',
  'resource',
  'resource_id',
  'observed_at',
  'severity',
  'summary',
  'key',
  'value',
  'content_hash',
  'redacted',
] as const

export interface APIErrorResponse {
  error?: string
  code?: string
  validation_errors?: ValidationError[]
}

export interface APIErrorDetails {
  status?: number
  message: string
  code?: string
  validation_errors?: ValidationError[]
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
  target_status?: PushStatus
  target_applied_at?: string
  target_submitted_at?: string
  target_pushed_by?: string
  request_status: 'accepted' | 'request_failed'
  apply_status: RollbackApplyStatus
  terminal: boolean
  terminal_status?: RollbackTerminalStatus
  started_at: string
  updated_at?: string
  elapsed_ms: number
  timeout_seconds: number
  timed_out: boolean
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

export type FleetVersionStatus =
  'below_recommended' | 'at_recommended' | 'above_recommended' | 'unknown' | 'not_applicable'

export type FleetVersionRecommendationAction =
  'upgrade_collector' | 'choose_older_config' | 'remove_component'

export interface FleetVersionMatrixEntry {
  group: string
  type: Workload['type']
  status: Workload['status']
  version: string
  version_status?: FleetVersionStatus
  count: number
  workload_ids: string[]
}

export interface FleetCollectorVersionFinding {
  workload_id: string
  display_name: string
  group: string
  version: string
  recommended_version: string
}

export interface FleetInvalidVersionFinding {
  workload_id: string
  display_name: string
  version: string
  reason: string
}

export interface FleetUnsupportedComponentFinding {
  workload_id: string
  display_name: string
  config_hash: string
  category: string
  component_type: string
  path: string
  available_hash?: string
  available_types?: string[]
}

export interface FleetVersionRecommendation {
  action: FleetVersionRecommendationAction
  workload_id?: string
  config_hash?: string
  reason: string
  components?: string[]
}

export interface FleetCompatibilityReason {
  code: string
  message: string
}

export interface FleetCompatibilityCollectorSummary {
  workload_id: string
  display_name: string
  blocking_reasons: FleetCompatibilityReason[]
}

export interface FleetCompatibilitySummary {
  total_collectors: number
  runnable_count: number
  not_runnable_count: number
  not_runnable_collectors: FleetCompatibilityCollectorSummary[]
}

export interface FleetCompatibilityVersion {
  reported: string
  status: FleetVersionStatus | string
  comparable: boolean
  reason?: string
}

export interface FleetCompatibilityAvailable {
  hash?: string
  categories: string[]
  component_types: Record<string, string[]>
}

export interface FleetCompatibilityComponent {
  category: string
  component_type: string
  path: string
}

export interface FleetCompatibilityConfig {
  hash?: string
  source: string
}

export interface FleetCompatibilityKnownIssue {
  code: string
  severity: string
  affected_version: string
  message: string
}

export interface FleetCompatibilityOpAMP {
  accepts_remote_config: boolean
  remote_config_status?: string
  config_hash?: string
}

export interface FleetCompatibilityMatrixEntry {
  workload_id: string
  display_name: string
  group: string
  status: Workload['status'] | string
  version: FleetCompatibilityVersion
  available_components: FleetCompatibilityAvailable
  required_components: FleetCompatibilityComponent[]
  config: FleetCompatibilityConfig
  known_issues: FleetCompatibilityKnownIssue[]
  opamp: FleetCompatibilityOpAMP
  runnable: boolean
  blocking_reasons: FleetCompatibilityReason[]
}

export interface FleetVersionIntelligence {
  schema_version: 'fleet-version-intelligence.v1'
  recommended_version: string
  version_matrix: FleetVersionMatrixEntry[]
  compatibility_summary?: FleetCompatibilitySummary
  compatibility_matrix?: FleetCompatibilityMatrixEntry[]
  collectors_below_recommended: FleetCollectorVersionFinding[]
  unsupported_config_components: FleetUnsupportedComponentFinding[]
  invalid_versions: FleetInvalidVersionFinding[]
  recommendations: FleetVersionRecommendation[]
}

export interface PushGroupSelector {
  match_labels?: Record<string, string>
  types?: string[]
  versions?: string[]
  capabilities?: string[]
}

export interface PushGroup {
  id: string
  name: string
  description?: string
  selector: PushGroupSelector
}

export interface PushPreviewRequest {
  group_id?: string
  selector?: PushGroupSelector
  config_content?: string
}

export interface PushPreviewBreakdown {
  remote_config_capable: number
  read_only: number
  incompatible: number
  offline: number
}

export type PushPreviewBucket = 'remote_config_capable' | 'read_only' | 'incompatible' | 'offline'

export interface PushPreviewTarget {
  workload_id: string
  display_name: string
  type: string
  version?: string
  status: string
  bucket: PushPreviewBucket
  reason?: string
  accepts_remote_config: boolean
  last_seen_unix?: number
}

export interface PushPreview {
  group_id?: string
  selector: PushGroupSelector
  targeted_count: number
  breakdown: PushPreviewBreakdown
  targets: PushPreviewTarget[]
}

export interface ConfigDriftAction {
  enabled: boolean
  reason?: string
  url?: string
}

export interface ConfigDriftSummary {
  total_collectors: number
  drifted_collectors: number
  pending_too_long: number
  missing_effective_config: number
  remote_config_unsupported: number
  outdated_versions: number
  unknown_incomplete_components: number
  heterogeneous_groups: number
}

export type ConfigDriftStatus =
  | 'in_sync'
  | 'drifted'
  | 'pending_too_long'
  | 'missing_effective_config'
  | 'remote_config_unsupported'
  | 'heterogeneous_effective_config'

export interface ConfigDriftItem {
  workload_id: string
  collector: string
  env: string
  version: string
  expected_config_hash?: string
  effective_config_hash?: string
  effective_config_hashes?: string[]
  drift_status: ConfigDriftStatus | string
  drift_reasons?: string[]
  last_push?: WorkloadConfig
  last_push_age_seconds?: number
  pending_too_long: boolean
  accepts_remote_config: boolean
  missing_effective_config: boolean
  unknown_incomplete_components: boolean
  group_heterogeneous_config: boolean
  has_config_drift_alert: boolean
  has_version_outdated_alert: boolean
  actions: Record<string, ConfigDriftAction>
}

export interface ConfigDriftDashboard {
  generated_at: string
  summary: ConfigDriftSummary
  items: ConfigDriftItem[]
}

export interface EvidenceReportSummary {
  config_changes: number
  validation_failures: number
  rollbacks: number
  drifted_collectors: number
  outdated_collectors: number
  audit_events: number
}

export interface EvidenceConfigChange {
  workload_id: string
  display_name?: string
  config_hash: string
  previous_hash?: string
  status: PushStatus | string
  pushed_by?: string
  applied_at: string
  content_available: boolean
  diff_summary?: string
}

export interface EvidenceValidationFailure {
  workload_id: string
  display_name?: string
  config_hash: string
  status: PushStatus | string
  error: string
  occurred_at: string
}

export interface EvidenceRollback {
  workload_id: string
  display_name?: string
  config_hash: string
  rollback_of_push_id?: string
  status: PushStatus | string
  occurred_at: string
}

export interface EvidenceAuditTrailEntry {
  action: string
  resource: string
  resource_id?: string
  detail?: string
  at: string
}

export interface EvidenceReportSignature {
  algorithm: string
  payload_digest_sha256: string
  key_id?: string
  signature?: string
  verification_hint: string
}

export interface EvidenceReport {
  schema_version: 'config_safety_evidence_report.v1'
  report_id: string
  generated_at: string
  recommended_version?: string
  summary: EvidenceReportSummary
  config_changes: EvidenceConfigChange[]
  validation_failures: EvidenceValidationFailure[]
  rollbacks: EvidenceRollback[]
  drift: ConfigDriftDashboard
  outdated_collectors: FleetCollectorVersionFinding[]
  audit_trail: EvidenceAuditTrailEntry[]
  signature?: EvidenceReportSignature
}

export type EvidenceReportExportFormat = 'json' | 'markdown' | 'csv' | 'pdf'
export type EvidenceReportDownloadFormat = Exclude<EvidenceReportExportFormat, 'json'>

export interface EvidenceReportDownload {
  blob: Blob
  format: EvidenceReportDownloadFormat
  filename: string
  contentType?: string
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
  'receivers' | 'processors' | 'exporters' | 'connectors' | 'extensions'
export type OTelSignal = 'traces' | 'metrics' | 'logs' | 'profiles' | 'unknown'

export interface OTelConfigDiffRequest {
  base_yaml: string
  target_yaml: string
  context?: OTelConfigDiffContext
}

export interface OTelConfigDiffContext {
  workload_id?: string
  display_name?: string
  workload_type?: string
  type?: string
  status?: string
  labels?: Record<string, string>
  fingerprint_keys?: Record<string, string>
  fleet_peers?: OTelConfigDiffWorkloadContext[]
  base_label?: string
  target_label?: string
  include_raw_paths?: boolean
}

export interface OTelConfigDiffWorkloadContext {
  id?: string
  display_name?: string
  workload_type?: string
  type?: string
  status?: string
  labels?: Record<string, string>
  fingerprint_keys?: Record<string, string>
}

export interface OTelConfigDiffResponse {
  schema_version: 'otel-config-diff.v1'
  valid: boolean
  summary: OTelDiffSummary
  risk_score?: ConfigRiskScore
  human_summary?: OTelHumanSummaryItem[]
  blast_radius: OTelBlastRadius
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

export interface OTelBlastRadius {
  schema_version: 'otel-config-blast-radius.v1'
  affected_signals: string[]
  touched_exporters: string[]
  impacted_services: OTelBlastRadiusService[]
  impacted_clusters: string[]
  critical_collectors: OTelBlastRadiusCollector[]
}

export interface OTelBlastRadiusService {
  service_name: string
  workload_id?: string
  display_name?: string
  type?: string
  status?: string
}

export interface OTelBlastRadiusCollector {
  workload_id: string
  display_name?: string
  status?: string
  reasons: string[]
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

export interface OTelHumanSummaryItem {
  category: 'component' | 'pipeline' | 'field' | 'unchanged'
  kind: OTelDiffChangeKind | 'unchanged'
  risk: OTelDiffRisk
  text: string
  component_id?: string
  pipeline_key?: string
  signal?: OTelSignal
  path?: string
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
