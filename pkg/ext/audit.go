package ext

import "context"

// AuditEvent describes a single security-relevant action recorded by an AuditLogger.
type AuditEvent struct {
	Action     string
	UserID     string
	Email      string
	Resource   string
	ResourceID string
	Detail     string
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

// NopAuditLogger is a no-op AuditLogger used as the default when no audit sink is wired.
type NopAuditLogger struct{}

// Log discards the event and reports success.
func (NopAuditLogger) Log(context.Context, AuditEvent) error { return nil }
