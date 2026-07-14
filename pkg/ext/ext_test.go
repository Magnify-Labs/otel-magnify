package ext_test

import (
	"context"
	"testing"

	"github.com/magnify-labs/otel-magnify/internal/alerts"
	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// Compile-time interface satisfaction checks.
var (
	_ ext.Store         = (*store.DB)(nil)
	_ ext.AuthProvider  = (*auth.Auth)(nil)
	_ ext.AuditLogger   = ext.NopAuditLogger{}
	_ ext.AlertNotifier = (*alerts.WebhookNotifier)(nil)
)

func TestUserInfoContextRoundTrip(t *testing.T) {
	info := &ext.UserInfo{
		UserID: "u-123",
		Email:  "test@example.com",
		Groups: []string{"administrator"},
	}

	ctx := ext.ContextWithUserInfo(context.Background(), info)
	got := ext.UserInfoFromContext(ctx)

	if got == nil {
		t.Fatal("expected UserInfo, got nil")
		return
	}
	if got.UserID != info.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, info.UserID)
	}
	if got.Email != info.Email {
		t.Errorf("Email = %q, want %q", got.Email, info.Email)
	}
	if len(got.Groups) != 1 || got.Groups[0] != "administrator" {
		t.Errorf("Groups = %v, want [administrator]", got.Groups)
	}
}

func TestUserInfoFromEmptyContext(t *testing.T) {
	got := ext.UserInfoFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil from empty context, got %+v", got)
	}
}
