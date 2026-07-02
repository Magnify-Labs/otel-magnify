package perm

// matrix maps system-group names → permissions granted. In Spec A all
// groups are system groups where name == role. Spec B will resolve
// "group.name → group.role → matrix" for custom groups. Call sites of
// Has() do not need to change when that indirection lands.
var matrix = map[string]map[Permission]bool{
	"viewer": {
		// No write permissions. Read endpoints (GET) are not gated;
		// any authenticated user can consume them.
	},
	"editor": {
		PushConfig:      true,
		ValidateConfig:  true,
		CreateConfigTpl: true,
		ResolveAlert:    true,
		ExportReports:   true,
		ArchiveWorkload: true,
	},
	"administrator": {
		PushConfig:      true,
		ValidateConfig:  true,
		CreateConfigTpl: true,
		ResolveAlert:    true,
		ExportReports:   true,
		ArchiveWorkload: true,
		DeleteWorkload:  true,
		ViewAudit:       true,
		ManageUsers:     true,
		ManageSettings:  true,
	},
}
