package api

import (
	"net/http"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

var validThemes = map[string]bool{"light": true, "dark": true, "system": true}
var validLanguages = map[string]bool{"en": true, "fr": true}

type putPrefsRequest struct {
	Theme    string `json:"theme"`
	Language string `json:"language"`
}

func (a *API) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	info := ext.UserInfoFromContext(r.Context())
	if info == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req putPrefsRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if !validThemes[req.Theme] {
		respondError(w, http.StatusBadRequest, "invalid theme")
		return
	}
	if !validLanguages[req.Language] {
		respondError(w, http.StatusBadRequest, "invalid language")
		return
	}

	prefs := models.UserPreferences{
		UserID: info.UserID, Theme: req.Theme, Language: req.Language,
	}
	if err := a.db.UpsertUserPreferences(prefs); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save preferences")
		return
	}
	saved, err := a.db.GetUserPreferences(info.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to read back preferences")
		return
	}
	respondJSON(w, http.StatusOK, saved)
}
