package perm

// matrix maps system-group names → permissions granted. In Spec A all
// groups are system groups where name == role. Spec B will resolve
// "group.name → group.role → matrix" for custom groups. Call sites of
// Has() do not need to change when that indirection lands.
var matrix = map[string]map[Permission]bool{
	"viewer": {
		// No privileged permissions. Authenticated viewers may still consume
		// metadata-oriented GET endpoints, but full config content is gated
		// separately because it can contain exporter secrets.
	},
	"editor": {
		ReadConfigContent: true,
		PushConfig:        true,
		ValidateConfig:    true,
		CreateConfigTpl:   true,
		ResolveAlert:      true,
		ExportReports:     true,
		ArchiveWorkload:   true,
	},
	"administrator": {
		ReadConfigContent: true,
		PushConfig:        true,
		ValidateConfig:    true,
		CreateConfigTpl:   true,
		ResolveAlert:      true,
		ExportReports:     true,
		ArchiveWorkload:   true,
		DeleteWorkload:    true,
		ViewAudit:         true,
		ManageUsers:       true,
		ManageSettings:    true,
	},
}
