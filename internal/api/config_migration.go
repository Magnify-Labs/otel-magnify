package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/magnify-labs/otel-magnify/internal/migrationassistant"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (a *API) handlePreviewConfigMigration(w http.ResponseWriter, r *http.Request) {
	var req models.ConfigMigrationPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	resp, err := migrationassistant.NewAssistant().Preview(req)
	if err != nil {
		if errors.Is(err, migrationassistant.ErrSourceTooLarge) {
			respondError(w, http.StatusRequestEntityTooLarge, "source exceeds 1 MiB limit")
			return
		}
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, resp)
}
