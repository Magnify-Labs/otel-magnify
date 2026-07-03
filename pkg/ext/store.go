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
	RecordGitOpsValidationStatus(status models.GitOpsValidationStatus) error
	GetLatestGitOpsValidationStatus(provider, sourcePath, sourceRef, commitSHA string) (*models.GitOpsValidationStatus, error)

	RecordWorkloadConfig(wc models.WorkloadConfig) error
	UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error
	MarkWorkloadConfigSent(workloadID, configID string, sentAt time.Time) error
	UpdateWorkloadConfigInstanceStatus(workloadID, configID, instanceUID, status, errorMessage string, updatedAt time.Time) error
	GetLatestWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetLatestPendingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetLatestPendingOrApplyingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetWorkloadConfigHistory(workloadID string) ([]models.WorkloadConfig, error)
	GetLastAppliedWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetLatestKnownGoodWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetWorkloadKnownGood(workloadID string) (*models.WorkloadKnownGoodConfig, error)
	SetWorkloadKnownGood(workloadID, configID, markedBy, replaceReason string) (*models.WorkloadKnownGoodConfig, models.SetKnownGoodResult, error)
	SetWorkloadKnownGoodWithPrecondition(workloadID, configID, markedBy, replaceReason string, ifCurrent *string, force bool) (*models.WorkloadKnownGoodConfig, models.SetKnownGoodResult, error)
	ClearWorkloadKnownGood(workloadID string) (*models.WorkloadKnownGoodConfig, error)
	GetPreviousAppliedWorkloadConfig(workloadID, excludeHash string) (*models.WorkloadConfig, error)
	GetRollbackTarget(workloadID, excludeHash string) (*models.RollbackTarget, error)
	IsConfigKnownGoodProtected(configID string) (bool, error)

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

	CreateOrUpdateConfigApprovalRequest(req models.ConfigApprovalRequest) (models.ConfigApprovalRequest, error)
	GetConfigApprovalRequest(id string) (models.ConfigApprovalRequest, error)
	ListConfigApprovalRequests(workloadID string) ([]models.ConfigApprovalRequest, error)
	ApproveConfigApprovalRequest(id, approvedBy, comment string, approvedAt time.Time) (models.ConfigApprovalRequest, error)
	MarkConfigApprovalRequestPushed(id, configHash, pushComment string, prodDoubleConfirmed, breakGlass bool, breakGlassReason string, pushedAt time.Time) (models.ConfigApprovalRequest, error)

	CreateCanaryStatus(status models.CanaryStatus) error
	UpdateCanaryStatus(status models.CanaryStatus) error
	GetCanaryStatus(workloadID, canaryID string) (*models.CanaryStatus, error)

	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	ListAlerts(includeResolved bool) ([]models.Alert, error)
	GetUnresolvedAlertByWorkloadAndRule(workloadID, rule string) (*models.Alert, error)

	Close() error
	Migrate() error
}
