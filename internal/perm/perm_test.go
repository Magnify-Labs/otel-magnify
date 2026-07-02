package perm

import (
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

func TestHas_MatrixPerRole(t *testing.T) {
	tests := []struct {
		role string
		perm Permission
		want bool
	}{
		{"viewer", PushConfig, false},
		{"viewer", ViewAudit, false},
		{"viewer", DeleteWorkload, false},
		{"viewer", ManageUsers, false},
		{"editor", PushConfig, true},
		{"editor", ValidateConfig, true},
		{"editor", CreateConfigTpl, true},
		{"editor", ResolveAlert, true},
		{"editor", ArchiveWorkload, true},
		{"editor", DeleteWorkload, false},
		{"editor", ViewAudit, false},
		{"editor", ManageUsers, false},
		{"administrator", PushConfig, true},
		{"administrator", ViewAudit, true},
		{"administrator", DeleteWorkload, true},
		{"administrator", ManageUsers, true},
		{"administrator", ManageSettings, true},
	}
	for _, tc := range tests {
		u := ext.UserInfo{Groups: []string{tc.role}}
		if got := Has(u, tc.perm); got != tc.want {
			t.Errorf("Has(groups=[%s], %s) = %v, want %v", tc.role, tc.perm, got, tc.want)
		}
	}
}

func TestHas_MultiGroupUnion(t *testing.T) {
	u := ext.UserInfo{Groups: []string{"viewer", "editor"}}
	if !Has(u, PushConfig) {
		t.Error("viewer+editor should grant PushConfig")
	}
	if Has(u, DeleteWorkload) {
		t.Error("viewer+editor should NOT grant DeleteWorkload")
	}
}

func TestHas_NoGroupsReturnsFalse(t *testing.T) {
	u := ext.UserInfo{}
	if Has(u, PushConfig) {
		t.Error("empty groups should grant nothing")
	}
}

func TestHas_UnknownGroupReturnsFalse(t *testing.T) {
	u := ext.UserInfo{Groups: []string{"superhero"}}
	if Has(u, PushConfig) {
		t.Error("unknown group should grant nothing")
	}
}
