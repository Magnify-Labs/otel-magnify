// Package models defines the shared domain structs persisted by the store and serialized over the API.
package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// Labels is a map[string]string stored as JSON TEXT in the DB.
type Labels map[string]string

// Value JSON-encodes the labels for storage as TEXT.
func (l Labels) Value() (string, error) {
	b, err := json.Marshal(l)
	return string(b), err
}

// Scan decodes the JSON-encoded labels from a string, []byte, or NULL DB value.
func (l *Labels) Scan(src any) error {
	switch v := src.(type) {
	case string:
		return json.Unmarshal([]byte(v), l)
	case []byte:
		return json.Unmarshal(v, l)
	case nil:
		*l = make(Labels)
		return nil
	default:
		return json.Unmarshal([]byte("{}"), l)
	}
}

// AvailableComponents maps OTel Collector categories (receivers, processors,
// exporters, extensions, connectors) to the set of component types the agent
// reports as installed. Populated from OpAMP AgentToServer.available_components.
type AvailableComponents struct {
	// Components keyed by category, each value a sorted list of component type names.
	Components map[string][]string `json:"components"`
	// Hash reported by the agent (hex-encoded); used to detect changes.
	Hash string `json:"hash,omitempty"`
}

// Value JSON-encodes the available components for storage as TEXT.
func (a AvailableComponents) Value() (string, error) {
	b, err := json.Marshal(a)
	return string(b), err
}

// Scan decodes the JSON-encoded available components from a string, []byte, or NULL DB value.
func (a *AvailableComponents) Scan(src any) error {
	switch v := src.(type) {
	case string:
		if v == "" {
			return nil
		}
		return json.Unmarshal([]byte(v), a)
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return json.Unmarshal(v, a)
	case nil:
		return nil
	default:
		return fmt.Errorf("unsupported type for AvailableComponents: %T", src)
	}
}

// RemoteConfigStatus mirrors the agent-reported state of the last config push (applying/applied/failed plus hash).
type RemoteConfigStatus struct {
	Status       string          `json:"status"` // "applying" | "applied" | "failed"
	ConfigHash   string          `json:"config_hash"`
	ErrorMessage string          `json:"error_message,omitempty"`
	UpdatedAt    time.Time       `json:"updated_at"`
	PushStatus   *WorkloadConfig `json:"push_status,omitempty"`
}

// Value JSON-encodes the remote config status for storage as TEXT.
func (r RemoteConfigStatus) Value() (string, error) {
	b, err := json.Marshal(r.Sanitized())
	return string(b), err
}

// Scan decodes the JSON-encoded remote config status from a string, []byte, or NULL DB value.
func (r *RemoteConfigStatus) Scan(src any) error {
	switch v := src.(type) {
	case string:
		if v == "" {
			return nil
		}
		if err := json.Unmarshal([]byte(v), r); err != nil {
			return err
		}
		r.Sanitize()
		return nil
	case []byte:
		if len(v) == 0 {
			return nil
		}
		if err := json.Unmarshal(v, r); err != nil {
			return err
		}
		r.Sanitize()
		return nil
	case nil:
		return nil
	default:
		return fmt.Errorf("unsupported type for RemoteConfigStatus: %T", src)
	}
}

const (
	// ConfigKindSaved identifies a user-saved config library entry.
	ConfigKindSaved = "saved"
	// ConfigKindTemplate identifies a built-in or user-authored reusable template.
	ConfigKindTemplate = "template"
	// ConfigKindDraft identifies a config library entry that is not ready to apply.
	ConfigKindDraft = "draft"
	// ConfigKindKnownGood identifies a config promoted as a recovery baseline.
	ConfigKindKnownGood = "known_good"

	// ConfigStatusReady marks a config library entry as ready for display/use.
	ConfigStatusReady = "ready"
	// ConfigStatusDraft marks a config library entry as still being edited.
	ConfigStatusDraft = "draft"
)

// ConfigVariable describes a frontend-editable placeholder exposed by a
// library config/template. Names are stable API contract values.
type ConfigVariable struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// Config is a named, versionable YAML template that operators push to one or more workloads.
type Config struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Content     string           `json:"content"`
	CreatedAt   time.Time        `json:"created_at"`
	CreatedBy   string           `json:"created_by"`
	Kind        string           `json:"kind"`
	Status      string           `json:"status"`
	Category    string           `json:"category,omitempty"`
	Stack       string           `json:"stack,omitempty"`
	Description string           `json:"description,omitempty"`
	Variables   []ConfigVariable `json:"variables,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
	BuiltIn     bool             `json:"built_in"`
}

// WorkloadConfig records a single push of a Config to a Workload, including its current apply status.
type WorkloadConfig struct {
	WorkloadID   string    `json:"workload_id"`
	ConfigID     string    `json:"config_id"`
	ConfigHash   string    `json:"config_hash,omitempty"`
	AppliedAt    time.Time `json:"applied_at"`
	Status       string    `json:"status"` // canonical: submitted | sent | applying | applied | failed | rollback_*
	ErrorMessage string    `json:"error_message,omitempty"`
	PushedBy     string    `json:"pushed_by,omitempty"`
	Content      string    `json:"content,omitempty"` // filled by JOIN in history queries
	// Label is a free-form, user-supplied marker for this revision (e.g.
	// "stable-2026-05", "before audit"). Nil pointer = unset; "" = explicitly
	// cleared. Operators set it from the push history table.
	Label *string `json:"label,omitempty"`

	PushID                        string                         `json:"push_id,omitempty"`
	SubmittedAt                   time.Time                      `json:"submitted_at,omitempty"`
	SentAt                        *time.Time                     `json:"sent_at,omitempty"`
	UpdatedAt                     time.Time                      `json:"updated_at,omitempty"`
	OpAMPStatusTimeoutAt          *time.Time                     `json:"opamp_status_timeout_at,omitempty"`
	TimedOutWaitingForOpAMPStatus bool                           `json:"timed_out_waiting_for_opamp_status"`
	TimeoutMessage                string                         `json:"timeout_message,omitempty"`
	RollbackOfPushID              string                         `json:"rollback_of_push_id,omitempty"`
	Timeline                      []WorkloadConfigTimelineEntry  `json:"timeline,omitempty"`
	InstanceStatuses              []WorkloadConfigInstanceStatus `json:"instance_statuses,omitempty"`
	TargetCount                   int                            `json:"target_count"`
	AppliedCount                  int                            `json:"applied_count"`
	FailedCount                   int                            `json:"failed_count"`
	PendingCount                  int                            `json:"pending_count"`
	TimedOutCount                 int                            `json:"timed_out_count"`
	NoStatusCount                 int                            `json:"no_status_count"`
	ErrorGroups                   []WorkloadConfigErrorGroup     `json:"error_groups,omitempty"`

	// Derived state labels for config history UX and rollback semantics.
	IsCurrent         bool `json:"is_current"`
	IsPrevious        bool `json:"is_previous"`
	IsLastKnownGood   bool `json:"is_last_known_good"`
	IsFailedCandidate bool `json:"is_failed_candidate"`
	ContentAvailable  bool `json:"content_available"`
}

// WorkloadKnownGoodConfig is the active workload-scoped pointer to the
// revision operators have explicitly selected as the recovery baseline.
type WorkloadKnownGoodConfig struct {
	WorkloadID       string     `json:"workload_id"`
	ConfigID         string     `json:"config_id"`
	MarkedAt         time.Time  `json:"marked_at"`
	MarkedBy         string     `json:"marked_by,omitempty"`
	SourceAppliedAt  *time.Time `json:"source_applied_at,omitempty"`
	ReplacedConfigID string     `json:"replaced_config_id,omitempty"`
	ReplaceReason    string     `json:"replace_reason,omitempty"`
	ContentAvailable bool       `json:"content_available"`
}

// SetKnownGoodResult describes whether a mark-known-good call changed the
// active pointer and which config, if any, it replaced.
type SetKnownGoodResult struct {
	Changed            bool   `json:"changed"`
	ReplacedConfigID   string `json:"replaced_config_id,omitempty"`
	PreconditionFailed bool   `json:"precondition_failed,omitempty"`
	CurrentConfigID    string `json:"current_config_id,omitempty"`
}

// RollbackTarget is the resolved config selected by default rollback:
// Last known-good first, then Previous.
type RollbackTarget struct {
	Kind   string         `json:"target_kind"`
	Config WorkloadConfig `json:"config"`
}

// PushActivityPoint is one bucket in the dashboard push-activity chart.
// Day is the UTC calendar day in YYYY-MM-DD form; Count is the number of
// workload-config rows whose applied_at falls on that day.
type PushActivityPoint struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
}

// ConfigDriftAction describes whether a dashboard action is currently safe to
// expose. Disabled actions carry an explicit operator-facing reason instead of
// pretending the backend can execute an unsafe or unspecified operation.
type ConfigDriftAction struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"`
	URL     string `json:"url,omitempty"`
}

// ConfigDriftSummary aggregates the fleet-level risk counters shown at the top
// of the dedicated config safety drift dashboard.
type ConfigDriftSummary struct {
	TotalCollectors             int `json:"total_collectors"`
	DriftedCollectors           int `json:"drifted_collectors"`
	PendingTooLong              int `json:"pending_too_long"`
	MissingEffectiveConfig      int `json:"missing_effective_config"`
	RemoteConfigUnsupported     int `json:"remote_config_unsupported"`
	OutdatedVersions            int `json:"outdated_versions"`
	UnknownIncompleteComponents int `json:"unknown_incomplete_components"`
	HeterogeneousGroups         int `json:"heterogeneous_groups"`
}

// ConfigDriftItem is one collector row in the config safety drift dashboard.
type ConfigDriftItem struct {
	WorkloadID                  string                       `json:"workload_id"`
	Collector                   string                       `json:"collector"`
	Env                         string                       `json:"env"`
	Version                     string                       `json:"version"`
	ExpectedConfigHash          string                       `json:"expected_config_hash,omitempty"`
	EffectiveConfigHash         string                       `json:"effective_config_hash,omitempty"`
	EffectiveConfigHashes       []string                     `json:"effective_config_hashes,omitempty"`
	DriftStatus                 string                       `json:"drift_status"`
	DriftReasons                []string                     `json:"drift_reasons,omitempty"`
	LastPush                    *WorkloadConfig              `json:"last_push,omitempty"`
	LastPushAgeSeconds          int64                        `json:"last_push_age_seconds,omitempty"`
	PendingTooLong              bool                         `json:"pending_too_long"`
	AcceptsRemoteConfig         bool                         `json:"accepts_remote_config"`
	MissingEffectiveConfig      bool                         `json:"missing_effective_config"`
	UnknownIncompleteComponents bool                         `json:"unknown_incomplete_components"`
	GroupHeterogeneousConfig    bool                         `json:"group_heterogeneous_config"`
	HasConfigDriftAlert         bool                         `json:"has_config_drift_alert"`
	HasVersionOutdatedAlert     bool                         `json:"has_version_outdated_alert"`
	Actions                     map[string]ConfigDriftAction `json:"actions"`
}

// ConfigDriftDashboard is the read model returned by /api/config-safety/drift.
type ConfigDriftDashboard struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Summary     ConfigDriftSummary `json:"summary"`
	Items       []ConfigDriftItem  `json:"items"`
}

// Alert is one open or resolved alert raised by the alert engine.
type Alert struct {
	ID         string     `json:"id"`
	WorkloadID string     `json:"workload_id"`
	Rule       string     `json:"rule"`     // "workload_down" | "config_drift" | "version_outdated"
	Severity   string     `json:"severity"` // "warning" | "critical"
	Message    string     `json:"message"`
	FiredAt    time.Time  `json:"fired_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// User is an authenticated principal of the platform; PasswordHash is bcrypt-encoded and never serialized.
type User struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	PasswordHash string  `json:"-"`
	TenantID     *string `json:"tenant_id,omitempty"`
}

// Group represents an RBAC group. In Spec A only the three seeded system
// groups exist (viewer, editor, administrator); custom groups arrive in
// Spec B. The Role column is the authoritative permission input — custom
// groups inherit their permission set from their Role.
type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"` // viewer | editor | administrator
	IsSystem  bool      `json:"is_system"`
	CreatedAt time.Time `json:"created_at"`
}

// UserGroup is the many-to-many pivot row between users and groups.
type UserGroup struct {
	UserID  string `json:"user_id"`
	GroupID string `json:"group_id"`
}

// UserPreferences holds the user-scoped UI preferences (theme + language).
// A missing row means "defaults": theme=system, language=en.
type UserPreferences struct {
	UserID    string    `json:"user_id"`
	Theme     string    `json:"theme"`    // light | dark | system
	Language  string    `json:"language"` // en | fr
	UpdatedAt time.Time `json:"updated_at"`
}

// FingerprintKeys is a small JSON map persisted alongside a Workload to
// record which resource attributes contributed to its identity.
type FingerprintKeys map[string]string

// Value JSON-encodes the fingerprint keys for storage as TEXT.
func (f FingerprintKeys) Value() (string, error) {
	b, err := json.Marshal(f)
	return string(b), err
}

// Scan decodes the JSON-encoded fingerprint keys from a string, []byte, or NULL DB value.
func (f *FingerprintKeys) Scan(src any) error {
	switch v := src.(type) {
	case string:
		if v == "" {
			*f = FingerprintKeys{}
			return nil
		}
		return json.Unmarshal([]byte(v), f)
	case []byte:
		if len(v) == 0 {
			*f = FingerprintKeys{}
			return nil
		}
		return json.Unmarshal(v, f)
	case nil:
		*f = FingerprintKeys{}
		return nil
	default:
		return fmt.Errorf("unsupported type for FingerprintKeys: %T", src)
	}
}

// Workload is a logical unit of management: a K8s Deployment / DaemonSet /
// StatefulSet, a single host process, or a cardinality-1 fallback keyed on
// the OpAMP InstanceUid.
type Workload struct {
	ID                  string               `json:"id"`
	FingerprintSource   string               `json:"fingerprint_source"` // "k8s" | "host" | "uid"
	FingerprintKeys     FingerprintKeys      `json:"fingerprint_keys"`
	DisplayName         string               `json:"display_name"`
	Type                string               `json:"type"` // "collector" | "sdk"
	Version             string               `json:"version"`
	Status              string               `json:"status"` // "connected" | "disconnected" | "degraded"
	LastSeenAt          time.Time            `json:"last_seen_at"`
	Labels              Labels               `json:"labels"`
	ActiveConfigID      *string              `json:"active_config_id,omitempty"`
	ActiveConfigHash    string               `json:"active_config_hash,omitempty"`
	RemoteConfigStatus  *RemoteConfigStatus  `json:"remote_config_status,omitempty"`
	CurrentConfigPush   *WorkloadConfig      `json:"current_config_push,omitempty"`
	AvailableComponents *AvailableComponents `json:"available_components,omitempty"`
	AcceptsRemoteConfig bool                 `json:"accepts_remote_config"`
	RetentionUntil      *time.Time           `json:"retention_until,omitempty"`
	ArchivedAt          *time.Time           `json:"archived_at,omitempty"`
}

// FleetVersionIntelligence is the stable API response for collector fleet
// version posture and config/component compatibility. It intentionally returns
// metadata and component names only, never raw config content.
type FleetVersionIntelligence struct {
	SchemaVersion               string                             `json:"schema_version"`
	RecommendedVersion          string                             `json:"recommended_version"`
	VersionMatrix               []FleetVersionMatrixEntry          `json:"version_matrix"`
	CollectorsBelowRecommended  []FleetCollectorVersionFinding     `json:"collectors_below_recommended"`
	UnsupportedConfigComponents []FleetUnsupportedComponentFinding `json:"unsupported_config_components"`
	InvalidVersions             []FleetInvalidVersionFinding       `json:"invalid_versions"`
	Recommendations             []FleetVersionRecommendation       `json:"recommendations"`
}

// FleetVersionMatrixEntry groups workloads by group, type, status, and
// reported version for fleet-level posture summaries.
type FleetVersionMatrixEntry struct {
	Group         string   `json:"group"`
	Type          string   `json:"type"`
	Status        string   `json:"status"`
	Version       string   `json:"version"`
	VersionStatus string   `json:"version_status"`
	Count         int      `json:"count"`
	WorkloadIDs   []string `json:"workload_ids"`
}

// FleetCollectorVersionFinding identifies one collector running below the
// recommended version supplied to the fleet intelligence API.
type FleetCollectorVersionFinding struct {
	WorkloadID         string `json:"workload_id"`
	DisplayName        string `json:"display_name"`
	Group              string `json:"group"`
	Version            string `json:"version"`
	RecommendedVersion string `json:"recommended_version"`
}

// FleetInvalidVersionFinding identifies one collector whose reported version
// cannot be compared safely against the recommended version.
type FleetInvalidVersionFinding struct {
	WorkloadID  string `json:"workload_id"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
	Reason      string `json:"reason"`
}

// FleetUnsupportedComponentFinding identifies a config component referenced by
// a workload config that is not present in the collector's reported components.
type FleetUnsupportedComponentFinding struct {
	WorkloadID     string   `json:"workload_id"`
	DisplayName    string   `json:"display_name"`
	ConfigHash     string   `json:"config_hash"`
	Category       string   `json:"category"`
	ComponentType  string   `json:"component_type"`
	Path           string   `json:"path"`
	AvailableHash  string   `json:"available_hash,omitempty"`
	AvailableTypes []string `json:"available_types,omitempty"`
}

// FleetVersionRecommendation is an operator action suggested by the fleet
// version intelligence API.
type FleetVersionRecommendation struct {
	Action     string   `json:"action"`
	WorkloadID string   `json:"workload_id,omitempty"`
	ConfigHash string   `json:"config_hash,omitempty"`
	Reason     string   `json:"reason"`
	Components []string `json:"components,omitempty"`
}

// WorkloadEvent is an append-only record of a pod transition on a workload.
type WorkloadEvent struct {
	ID          int64     `json:"id"`
	WorkloadID  string    `json:"workload_id"`
	InstanceUID string    `json:"instance_uid"`
	PodName     string    `json:"pod_name,omitempty"`
	EventType   string    `json:"event_type"` // "connected" | "disconnected" | "version_changed"
	Version     string    `json:"version,omitempty"`
	PrevVersion string    `json:"prev_version,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}
