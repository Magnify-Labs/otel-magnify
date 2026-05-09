// Package audit centralises audit-event emission for community handlers.
//
// Handlers call Emit with the request context and the AuditLogger held on
// api.API. The community binary defaults to ext.NopAuditLogger so events go
// nowhere; the enterprise binary swaps in a persistent backend via
// pkg/server.WithAuditLogger.
package audit

import (
	"context"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// Emit records a single audit event. It pulls UserID and Email from ctx
// (set by the auth middleware via ext.ContextWithUserInfo). When ctx has no
// UserInfo — e.g. a failed login attempt before authentication — UserID and
// Email stay empty, and the caller is responsible for putting the attempted
// identifier in detail.
//
// A nil logger is treated as a no-op so call sites don't have to guard
// against the community NopAuditLogger default. Returns the underlying
// logger's error so handlers can fail-loud.
func Emit(ctx context.Context, logger ext.AuditLogger, action, resource, resourceID, detail string) error {
	if logger == nil {
		return nil
	}
	ev := ext.AuditEvent{
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Detail:     detail,
	}
	if info := ext.UserInfoFromContext(ctx); info != nil {
		ev.UserID = info.UserID
		ev.Email = info.Email
	}
	return logger.Log(ctx, ev)
}
