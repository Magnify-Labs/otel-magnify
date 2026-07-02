package ext

import (
	"context"
	"time"
)

// AuditEvent describes a single security-relevant action recorded by an AuditLogger.
type AuditEvent struct {
	Action     string
	UserID     string
	Email      string
	Resource   string
	ResourceID string
	Detail     string
}

// AuditRecord is the stable read model returned by audit query backends.
// WorkloadID/ResourceID and ConfigHash are carried explicitly so the UI can
// link records to config diff/rollback flows without parsing Detail.
type AuditRecord struct {
	ID           string    `json:"id"`
	OccurredAt   time.Time `json:"occurred_at"`
	Action       string    `json:"action"`
	UserID       string    `json:"user_id,omitempty"`
	Email        string    `json:"email,omitempty"`
	Resource     string    `json:"resource,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	WorkloadID   string    `json:"workload_id,omitempty"`
	ConfigHash   string    `json:"config_hash,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	PrevHash     string    `json:"prev_hash,omitempty"`
	EventHash    string    `json:"event_hash,omitempty"`
	ImmutableRef string    `json:"immutable_ref,omitempty"`
}

// AuditEventFilter captures the supported query contract for audit viewers.
type AuditEventFilter struct {
	User       string
	UserID     string
	Email      string
	Action     string
	ResourceID string
	WorkloadID string
	ConfigHash string
	From       time.Time
	To         time.Time
	Limit      int
	Offset     int
}

// AuditEventPage is a single paginated audit query result.
type AuditEventPage struct {
	Events []AuditRecord
	Total  int
}

// AuditLogger sinks AuditEvents to the configured backend (file, syslog, SIEM, etc.).
//
// Log returns an error so persistent backends can report failure to the
// caller, which propagates as a 503 in the community handlers (fail-loud
// contract — see EE audit sink design doc). The default community
// NopAuditLogger always returns nil.
type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent) error
}

// AuditEventQuerier is implemented by audit sinks that support strategic reads.
// Community's NopAuditLogger intentionally does not implement this, so the API
// can return a deterministic unavailable page when no readable sink is wired.
type AuditEventQuerier interface {
	ListAuditEvents(ctx context.Context, filter AuditEventFilter) (AuditEventPage, error)
}

// NopAuditLogger is a no-op AuditLogger used as the default when no audit sink is wired.
type NopAuditLogger struct{}

// Log discards the event and reports success.
func (NopAuditLogger) Log(context.Context, AuditEvent) error { return nil }
