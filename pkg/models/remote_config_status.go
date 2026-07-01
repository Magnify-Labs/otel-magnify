package models

// Sanitized returns a copy whose operator-facing error text is safe for
// persistence, API responses, and broadcast payloads.
func (r RemoteConfigStatus) Sanitized() RemoteConfigStatus {
	r.ErrorMessage = SanitizeRemoteConfigErrorMessage(r.ErrorMessage)
	return r
}

// Sanitize mutates the status in place for callers that scan legacy JSON.
func (r *RemoteConfigStatus) Sanitize() {
	if r == nil {
		return
	}
	r.ErrorMessage = SanitizeRemoteConfigErrorMessage(r.ErrorMessage)
}
