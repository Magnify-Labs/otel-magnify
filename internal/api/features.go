package api

import (
	"net/http"
)

func (a *API) handleListCapabilities(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, a.capabilities.Document())
}

// handleListFeatures returns the configured feature flag map as JSON.
//
// The route is public (no auth middleware) because feature flags are
// not secrets: the features themselves remain gated by their own auth
// + permission middleware. Public access avoids a round-trip auth
// before the SPA can render its menu.
func (a *API) handleListFeatures(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, http.StatusOK, struct {
		Features map[string]bool `json:"features"`
	}{Features: a.capabilities.LegacyFeatures()})
}
