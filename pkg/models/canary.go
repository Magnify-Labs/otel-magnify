package models

import "time"

const (
	// CanaryStatusRunning means the selected targets are still being observed.
	CanaryStatusRunning = "running"
	// CanaryStatusSucceeded means every selected target reported applied.
	CanaryStatusSucceeded = "succeeded"
	// CanaryStatusPromoted means the candidate was pushed to remaining targets.
	CanaryStatusPromoted = "promoted"
	// CanaryStatusAborted means an operator stopped the workflow without broad push.
	CanaryStatusAborted = "aborted"
	// CanaryStatusRollback means a safe rollback config was pushed to canary targets.
	CanaryStatusRollback = "rollback_started"
	// CanaryStatusStopped means a machine-detectable stop condition halted the canary.
	CanaryStatusStopped = "stopped"
	// CanaryStatusFailed means the workflow failed before completing.
	CanaryStatusFailed = "failed"

	// CanaryStopRemoteConfigFailed means OpAMP reported a remote-config failure.
	CanaryStopRemoteConfigFailed = "remote_config_failed"
	// CanaryStopCollectorDegraded means a selected collector is unhealthy.
	CanaryStopCollectorDegraded = "collector_degraded"
	// CanaryStopNoHeartbeat means a selected collector heartbeat is stale or absent.
	CanaryStopNoHeartbeat = "no_heartbeat"
	// CanaryStopConfigDrift means a selected collector drifted from expected config.
	CanaryStopConfigDrift = "config_drift"
	// CanaryStopAlertTriggered means an alert gate requested canary stop.
	CanaryStopAlertTriggered = "alert_triggered"
)

// CanarySelection describes how a manual canary chooses collector instances.
type CanarySelection struct {
	Strategy     string            `json:"strategy"`
	InstanceUID  string            `json:"instance_uid,omitempty"`
	InstanceUIDs []string          `json:"instance_uids,omitempty"`
	Count        int               `json:"count,omitempty"`
	Percentage   int               `json:"percentage,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// CanaryTarget is the sanitized per-instance status exposed for canary polling.
type CanaryTarget struct {
	InstanceUID string    `json:"instance_uid"`
	PodName     string    `json:"pod_name,omitempty"`
	Status      string    `json:"status"`
	StopReason  string    `json:"stop_reason,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// CanaryCounts summarizes target state buckets for frontend polling.
type CanaryCounts struct {
	Pending  int `json:"pending"`
	Applying int `json:"applying"`
	Applied  int `json:"applied"`
	Failed   int `json:"failed"`
}

// CanaryStatus is the persisted manual canary workflow state.
type CanaryStatus struct {
	ID           string          `json:"id"`
	WorkloadID   string          `json:"workload_id"`
	ConfigHash   string          `json:"config_hash"`
	Status       string          `json:"status"`
	Selection    CanarySelection `json:"selection"`
	Targets      []CanaryTarget  `json:"targets"`
	Counts       CanaryCounts    `json:"counts"`
	StopReasons  []string        `json:"stop_reasons,omitempty"`
	Actor        string          `json:"actor,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	PromotedAt   *time.Time      `json:"promoted_at,omitempty"`
	AbortedAt    *time.Time      `json:"aborted_at,omitempty"`
	RolledBackAt *time.Time      `json:"rolled_back_at,omitempty"`
}

// CanaryValidationResult is returned by the canary validation endpoint before any push.
type CanaryValidationResult struct {
	Valid       bool           `json:"valid"`
	Targets     []CanaryTarget `json:"targets"`
	StopReasons []string       `json:"stop_reasons,omitempty"`
	ErrorCodes  []string       `json:"error_codes,omitempty"`
	Errors      []string       `json:"errors,omitempty"`
}

// Recount recalculates aggregate counts and stop reasons from target state.
func (c *CanaryStatus) Recount() {
	c.Counts = CanaryCounts{}
	reasons := map[string]bool{}
	for _, target := range c.Targets {
		switch target.Status {
		case PushStatusApplied:
			c.Counts.Applied++
		case PushStatusApplying:
			c.Counts.Applying++
		case PushStatusFailed:
			c.Counts.Failed++
		default:
			c.Counts.Pending++
		}
		if target.StopReason != "" {
			reasons[target.StopReason] = true
		}
	}
	c.StopReasons = c.StopReasons[:0]
	for reason := range reasons {
		c.StopReasons = append(c.StopReasons, reason)
	}
}
