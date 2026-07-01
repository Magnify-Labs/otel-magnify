package models

import "encoding/json"

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

// MarshalJSON serializes a sanitized remote config status so model-level JSON
// responses cannot expose raw agent-provided error details.
func (r RemoteConfigStatus) MarshalJSON() ([]byte, error) {
	type remoteConfigStatusAlias RemoteConfigStatus
	sanitized := remoteConfigStatusAlias(r.Sanitized())
	return json.Marshal(sanitized)
}

// UnmarshalJSON decodes and sanitizes raw remote config status payloads so
// callers never observe pre-existing sensitive error text after decoding.
func (r *RemoteConfigStatus) UnmarshalJSON(data []byte) error {
	type remoteConfigStatusAlias RemoteConfigStatus
	var decoded remoteConfigStatusAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*r = RemoteConfigStatus(decoded)
	r.Sanitize()
	return nil
}
