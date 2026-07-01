package models

import (
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// PushStatusSubmitted means the server accepted and queued the push.
	PushStatusSubmitted = "submitted"
	// PushStatusSent means the config was sent through OpAMP.
	PushStatusSent = "sent"
	// PushStatusApplying means at least one target reported applying.
	PushStatusApplying = "applying"
	// PushStatusApplied means the push reached an applied terminal state.
	PushStatusApplied = "applied"
	// PushStatusFailed means the push failed on at least one required target.
	PushStatusFailed = "failed"
	// PushStatusRollbackStarted means rollback was triggered after the push.
	PushStatusRollbackStarted = "rollback_started"
	// PushStatusRollbackApplied means rollback completed successfully.
	PushStatusRollbackApplied = "rollback_applied"
	// PushStatusRollbackFailed means rollback was triggered but failed.
	PushStatusRollbackFailed = "rollback_failed"
	// InstanceStatusSent means an instance was targeted but has not reported yet.
	InstanceStatusSent = "sent"
	// InstanceStatusNoStatus means no OpAMP status arrived before timeout.
	InstanceStatusNoStatus = "no_status"
)

// OpAMPStatusTimeoutMessage is the user-facing timeout message for missing OpAMP status.
const OpAMPStatusTimeoutMessage = "No OpAMP status after 30s"

// WorkloadConfigTimelineEntry is one observable milestone in a config push.
type WorkloadConfigTimelineEntry struct {
	State    string    `json:"state"`
	At       time.Time `json:"at"`
	Message  string    `json:"message,omitempty"`
	Terminal bool      `json:"terminal"`
	TimedOut bool      `json:"timed_out,omitempty"`
}

// WorkloadConfigInstanceStatus is the per-target snapshot/status for a push.
type WorkloadConfigInstanceStatus struct {
	InstanceUID  string    `json:"instance_uid"`
	PodName      string    `json:"pod_name,omitempty"`
	Node         string    `json:"node,omitempty"`
	Required     bool      `json:"required"`
	Status       string    `json:"status"`
	ConfigHash   string    `json:"config_hash,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
	ErrorCause   string    `json:"error_cause,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	TimedOut     bool      `json:"timed_out,omitempty"`
}

// WorkloadConfigErrorGroup summarizes repeated remote config errors by cause.
type WorkloadConfigErrorGroup struct {
	Cause             string    `json:"cause"`
	Title             string    `json:"title"`
	Severity          string    `json:"severity"`
	Count             int       `json:"count"`
	AffectedInstances []string  `json:"affected_instances,omitempty"`
	FirstSeenAt       time.Time `json:"first_seen_at,omitempty"`
	LastSeenAt        time.Time `json:"last_seen_at,omitempty"`
	SampleMessage     string    `json:"sample_message,omitempty"`
	SamplePath        string    `json:"sample_path,omitempty"`
	ConfigHash        string    `json:"config_hash,omitempty"`
	Retryable         bool      `json:"retryable"`
}

// HydratePushStatus normalizes legacy status aliases, rebuilds timeline/counts,
// overlays the 30s no-OpAMP-status warning, and groups per-instance errors.
func (wc *WorkloadConfig) HydratePushStatus(now time.Time) {
	if wc.ConfigHash == "" {
		wc.ConfigHash = wc.ConfigID
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	wc.Status = canonicalPushStatus(wc.Status)
	if wc.SubmittedAt.IsZero() {
		wc.SubmittedAt = wc.AppliedAt
	}
	if wc.PushID == "" && !wc.SubmittedAt.IsZero() {
		wc.PushID = wc.WorkloadID + ":" + wc.ConfigID + ":" + wc.SubmittedAt.UTC().Format(time.RFC3339Nano)
	}
	if wc.OpAMPStatusTimeoutAt == nil && !wc.SubmittedAt.IsZero() {
		t := wc.SubmittedAt.Add(30 * time.Second)
		wc.OpAMPStatusTimeoutAt = &t
	}

	wc.TargetCount = len(wc.InstanceStatuses)
	wc.AppliedCount, wc.FailedCount, wc.PendingCount = 0, 0, 0
	wc.TimedOutCount, wc.NoStatusCount = 0, 0
	hasRemoteStatus := false
	hasApplyingOrApplied := false
	allApplied := wc.TargetCount > 0
	wc.ErrorMessage = SanitizeRemoteConfigErrorMessage(wc.ErrorMessage)
	for i := range wc.InstanceStatuses {
		st := canonicalInstanceStatus(wc.InstanceStatuses[i].Status)
		wc.InstanceStatuses[i].Status = st
		wc.InstanceStatuses[i].TimedOut = false
		if wc.InstanceStatuses[i].ConfigHash == "" {
			wc.InstanceStatuses[i].ConfigHash = wc.ConfigID
		}
		if wc.InstanceStatuses[i].ErrorMessage != "" {
			if wc.InstanceStatuses[i].ErrorCause == "" {
				wc.InstanceStatuses[i].ErrorCause = normalizeRemoteConfigErrorCause("", wc.InstanceStatuses[i].ErrorMessage)
			}
			wc.InstanceStatuses[i].ErrorMessage = SanitizeRemoteConfigErrorMessage(wc.InstanceStatuses[i].ErrorMessage)
		}
		if wc.OpAMPStatusTimeoutAt != nil && !now.Before(*wc.OpAMPStatusTimeoutAt) && wc.InstanceStatuses[i].Required && (st == PushStatusSent || st == InstanceStatusNoStatus) {
			st = InstanceStatusNoStatus
			wc.InstanceStatuses[i].Status = st
			wc.InstanceStatuses[i].TimedOut = true
			wc.InstanceStatuses[i].ErrorCause = "apply_timeout"
			wc.InstanceStatuses[i].ErrorMessage = OpAMPStatusTimeoutMessage
			wc.TimedOutCount++
		}
		switch st {
		case PushStatusApplied:
			wc.AppliedCount++
			hasRemoteStatus = true
			hasApplyingOrApplied = true
		case PushStatusApplying:
			wc.PendingCount++
			allApplied = false
			hasRemoteStatus = true
			hasApplyingOrApplied = true
		case PushStatusFailed:
			wc.FailedCount++
			allApplied = false
			hasRemoteStatus = true
		default:
			if st == InstanceStatusNoStatus {
				wc.NoStatusCount++
			}
			wc.PendingCount++
			allApplied = false
		}
	}

	if wc.TargetCount > 0 {
		switch {
		case wc.FailedCount > 0:
			wc.Status = PushStatusFailed
		case allApplied:
			wc.Status = PushStatusApplied
		case hasApplyingOrApplied:
			wc.Status = PushStatusApplying
		case wc.SentAt != nil:
			wc.Status = PushStatusSent
		}
	}

	wc.TimedOutWaitingForOpAMPStatus = false
	wc.TimeoutMessage = ""
	if wc.TimedOutCount > 0 || (wc.OpAMPStatusTimeoutAt != nil && !now.Before(*wc.OpAMPStatusTimeoutAt) && !hasRemoteStatus && !isTerminalPushStatus(wc.Status)) {
		wc.TimedOutWaitingForOpAMPStatus = true
		wc.TimeoutMessage = OpAMPStatusTimeoutMessage
	}

	wc.Timeline = wc.buildTimeline()
	wc.ErrorGroups = buildWorkloadConfigErrorGroups(*wc)
}

func (wc WorkloadConfig) buildTimeline() []WorkloadConfigTimelineEntry {
	var out []WorkloadConfigTimelineEntry
	add := func(state string, at time.Time, msg string, timedOut bool) {
		if at.IsZero() {
			return
		}
		out = append(out, WorkloadConfigTimelineEntry{State: state, At: at, Message: msg, Terminal: isTerminalPushStatus(state), TimedOut: timedOut})
	}
	add(PushStatusSubmitted, wc.SubmittedAt, "", false)
	if wc.SentAt != nil {
		add(PushStatusSent, *wc.SentAt, "", false)
	}
	if wc.TimedOutWaitingForOpAMPStatus && wc.OpAMPStatusTimeoutAt != nil {
		state := wc.Status
		if wc.TimedOutCount > 0 {
			state = InstanceStatusNoStatus
		}
		add(state, *wc.OpAMPStatusTimeoutAt, OpAMPStatusTimeoutMessage, true)
	}
	for _, inst := range wc.InstanceStatuses {
		if inst.UpdatedAt.IsZero() || inst.Status == PushStatusSent || inst.Status == InstanceStatusNoStatus {
			continue
		}
		add(inst.Status, inst.UpdatedAt, inst.ErrorMessage, false)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func canonicalPushStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending":
		return PushStatusSubmitted
	case PushStatusSubmitted, PushStatusSent, PushStatusApplying, PushStatusApplied, PushStatusFailed, PushStatusRollbackStarted, PushStatusRollbackApplied, PushStatusRollbackFailed:
		return strings.ToLower(status)
	default:
		return strings.ToLower(status)
	}
}

func canonicalInstanceStatus(status string) string {
	switch canonicalPushStatus(status) {
	case PushStatusSubmitted, PushStatusSent:
		return PushStatusSent
	case PushStatusApplying, PushStatusApplied, PushStatusFailed:
		return canonicalPushStatus(status)
	default:
		return InstanceStatusNoStatus
	}
}

func isTerminalPushStatus(status string) bool {
	switch status {
	case PushStatusApplied, PushStatusFailed, PushStatusRollbackApplied, PushStatusRollbackFailed:
		return true
	default:
		return false
	}
}

func buildWorkloadConfigErrorGroups(wc WorkloadConfig) []WorkloadConfigErrorGroup {
	groups := map[string]*WorkloadConfigErrorGroup{}
	add := func(cause, instance, msg string, at time.Time) {
		cause = normalizeRemoteConfigErrorCause(cause, msg)
		g := groups[cause]
		if g == nil {
			g = &WorkloadConfigErrorGroup{Cause: cause, Title: remoteConfigErrorTitle(cause), Severity: remoteConfigErrorSeverity(cause), ConfigHash: wc.ConfigID, Retryable: remoteConfigErrorRetryable(cause)}
			groups[cause] = g
		}
		g.Count++
		if instance != "" {
			g.AffectedInstances = append(g.AffectedInstances, instance)
		}
		if g.SampleMessage == "" {
			g.SampleMessage = SanitizeRemoteConfigErrorMessage(msg)
		}
		if !at.IsZero() && (g.FirstSeenAt.IsZero() || at.Before(g.FirstSeenAt)) {
			g.FirstSeenAt = at
		}
		if !at.IsZero() && (g.LastSeenAt.IsZero() || at.After(g.LastSeenAt)) {
			g.LastSeenAt = at
		}
	}
	for _, inst := range wc.InstanceStatuses {
		if inst.Status == PushStatusFailed || inst.ErrorCause != "" || inst.ErrorMessage != "" {
			add(inst.ErrorCause, inst.InstanceUID, inst.ErrorMessage, inst.UpdatedAt)
		}
	}
	if wc.ErrorMessage != "" && len(groups) == 0 {
		add("", "", wc.ErrorMessage, wc.AppliedAt)
	}
	out := make([]WorkloadConfigErrorGroup, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.AffectedInstances)
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) < severityRank(out[j].Severity)
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Cause < out[j].Cause
	})
	return out
}

// SanitizeRemoteConfigErrorMessage converts arbitrary collector/agent-provided
// text into a stable, length-capped summary safe for storage and APIs. Raw
// RemoteConfigStatus.error_message may include YAML snippets, endpoints,
// headers, tokens, tenant names, or policy details; callers must not persist or
// expose it directly.
func SanitizeRemoteConfigErrorMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	if msg == "Remote config error details redacted" {
		return msg
	}
	for _, cause := range []string{"collector_validation", "opamp_send_failed", "apply_timeout", "capability_mismatch", "permission_or_policy", "rollback_unavailable", "unknown"} {
		if msg == remoteConfigErrorTitle(cause) {
			return msg
		}
	}
	cause := normalizeRemoteConfigErrorCause("", msg)
	summary := remoteConfigErrorTitle(cause)
	if cause == "unknown" {
		summary = "Remote config error details redacted"
	}
	if utf8.RuneCountInString(summary) > 96 {
		r := []rune(summary)
		summary = string(r[:96])
	}
	return summary
}

func normalizeRemoteConfigErrorCause(cause, msg string) string {
	cause = strings.TrimSpace(strings.ToLower(cause))
	if cause != "" {
		return cause
	}
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "unknown receiver"), strings.Contains(m, "unknown exporter"), strings.Contains(m, "unknown processor"), strings.Contains(m, "invalid pipeline"), strings.Contains(m, "invalid component"):
		return "collector_validation"
	case strings.Contains(m, "not connected"), strings.Contains(m, "connection lost"), strings.Contains(m, "send"):
		return "opamp_send_failed"
	case strings.Contains(m, "timeout"), strings.Contains(m, "timed out"):
		return "apply_timeout"
	case strings.Contains(m, "capability"):
		return "capability_mismatch"
	case strings.Contains(m, "permission"), strings.Contains(m, "policy"), strings.Contains(m, "refus"):
		return "permission_or_policy"
	case strings.Contains(m, "rollback") && strings.Contains(m, "unavailable"):
		return "rollback_unavailable"
	default:
		return "unknown"
	}
}

func remoteConfigErrorTitle(cause string) string {
	switch cause {
	case "collector_validation":
		return "Collector rejected the config"
	case "opamp_send_failed":
		return "OpAMP send failed"
	case "apply_timeout":
		return "Config apply timed out"
	case "capability_mismatch":
		return "Remote config capability mismatch"
	case "permission_or_policy":
		return "Agent policy rejected the config"
	case "rollback_unavailable":
		return "Rollback unavailable"
	default:
		return "Remote config error"
	}
}

func remoteConfigErrorSeverity(cause string) string {
	if cause == "rollback_unavailable" || cause == "unknown" {
		return "medium"
	}
	return "high"
}

func remoteConfigErrorRetryable(cause string) bool {
	return cause != "permission_or_policy" && cause != "capability_mismatch" && cause != "rollback_unavailable"
}

func severityRank(sev string) int {
	if sev == "high" {
		return 0
	}
	return 1
}
