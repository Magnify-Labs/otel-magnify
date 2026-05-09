package api

import "net/http"

// sideEffectStatus describes whether the request's mutation persisted before
// the audit log write failed. Surfaced in the 503 body so callers can
// reconcile without inspecting the API state separately.
type sideEffectStatus string

const (
	sideEffectApplied sideEffectStatus = "applied"
	sideEffectNone    sideEffectStatus = "none"
)

// respondAuditUnavailable writes the standardised 503 returned by every
// handler whose audit emission failed. Body shape:
//
//	{"error": "audit unavailable", "side_effect_status": "applied" | "none"}
//
// "applied" means the business mutation already persisted (config row written,
// password changed, OpAMP push sent, etc.) but the audit DB rejected the
// event. "none" means nothing was written outside the audit subsystem.
func respondAuditUnavailable(w http.ResponseWriter, status sideEffectStatus) {
	respondJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error":              "audit unavailable",
		"side_effect_status": string(status),
	})
}
