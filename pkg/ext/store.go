package ext

import (
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// Store is the persistence-layer contract consumed by the API and OpAMP layers; satisfied by the community SQLite/Postgres DB and overridable from EE.
type Store interface {
	CreateUser(u models.User) error
	GetUserByEmail(email string) (models.User, error)
	UpdateUser(u models.User) error

	// Groups (RBAC fondation, Spec A).
	ListSystemGroups() ([]models.Group, error)
	GetGroupByName(name string) (models.Group, error)
	AttachUserToGroupByName(userID, groupName string) error
	DetachUserFromGroup(userID, groupName string) error
	ReplaceUserGroups(userID string, groupNames []string) error
	GetUserGroups(userID string) ([]models.Group, error)

	// User preferences (theme + language).
	GetUserPreferences(userID string) (models.UserPreferences, error)
	UpsertUserPreferences(p models.UserPreferences) error

	UpsertWorkload(w models.Workload) error
	GetWorkload(id string) (models.Workload, error)
	ListWorkloads(includeArchived bool) ([]models.Workload, error)
	MarkWorkloadDisconnected(id string, retentionUntil time.Time) error
	ClearWorkloadRetention(id string) error
	ArchiveExpiredWorkloads(now time.Time) (int64, error)
	DeleteWorkload(id string) error

	InsertWorkloadEvent(e models.WorkloadEvent) (int64, error)
	ListWorkloadEvents(workloadID string, limit int, since time.Time) ([]models.WorkloadEvent, error)
	PurgeOldWorkloadEvents(cutoff time.Time) (int64, error)

	CreateConfig(c models.Config) error
	GetConfig(id string) (models.Config, error)
	ListConfigs() ([]models.Config, error)

	RecordWorkloadConfig(wc models.WorkloadConfig) error
	UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error
	GetLatestPendingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetWorkloadConfigHistory(workloadID string) ([]models.WorkloadConfig, error)
	GetLastAppliedWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)

	// SetWorkloadConfigLabel attaches (or clears, when label == "") an
	// operator-facing label to every workload_configs row matching
	// (workloadID, configID == hash). Multiple rows may match when the same
	// content has been pushed several times; they all carry the same label
	// because the label describes the *revision content*, not a single push.
	SetWorkloadConfigLabel(workloadID, hash, label string) error
	// GetWorkloadConfigByHash returns the most recent push of the given
	// hash to the workload, joined with the config content. Returns
	// (nil, nil) when no row matches.
	GetWorkloadConfigByHash(workloadID, hash string) (*models.WorkloadConfig, error)
	GetPushActivity(days int) ([]models.PushActivityPoint, error)

	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	ListAlerts(includeResolved bool) ([]models.Alert, error)
	GetUnresolvedAlertByWorkloadAndRule(workloadID, rule string) (*models.Alert, error)

	Close() error
	Migrate() error
}
