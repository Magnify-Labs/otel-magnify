// Package perm is the single source of truth for the authorization matrix.
// Handlers never branch on user groups directly; they call api.RequirePerm(p)
// which delegates to Has(). Changing the permission model only touches this
// package.
package perm

import "github.com/magnify-labs/otel-magnify/pkg/ext"

// Permission identifies an authorization-checked action; values are the canonical strings stored in the role matrix.
type Permission string

// Permissions enumerated below are the granular actions that handlers gate via api.RequirePerm.
const (
	PushConfig      Permission = "workload:push_config"
	ValidateConfig  Permission = "workload:validate_config"
	CreateConfigTpl Permission = "config:create"
	ResolveAlert    Permission = "alert:resolve"
	ExportReports   Permission = "reports:export"
	ArchiveWorkload Permission = "workload:archive"
	DeleteWorkload  Permission = "workload:delete"
	ManageUsers     Permission = "users:manage"    // réservé Spec B
	ManageSettings  Permission = "settings:manage" // réservé Spec C
)

// Has returns true when any of the user's groups grants p.
func Has(u ext.UserInfo, p Permission) bool {
	for _, g := range u.Groups {
		if matrix[g][p] {
			return true
		}
	}
	return false
}
