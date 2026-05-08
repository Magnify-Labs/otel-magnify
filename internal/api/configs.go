package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type createConfigRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (a *API) handleListConfigs(w http.ResponseWriter, _ *http.Request) {
	configs, err := a.db.ListConfigs()
	if err != nil {
		respondError(w, 500, "failed to list configs")
		return
	}
	respondJSON(w, 200, configs)
}

func (a *API) handleCreateConfig(w http.ResponseWriter, r *http.Request) {
	var req createConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.Name == "" || req.Content == "" {
		respondError(w, 400, "name and content are required")
		return
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(req.Content)))
	info := ext.UserInfoFromContext(r.Context())
	createdBy := ""
	if info != nil {
		createdBy = info.Email
	}

	cfg := models.Config{
		ID:        hash,
		Name:      req.Name,
		Content:   req.Content,
		CreatedAt: time.Now().UTC(),
		CreatedBy: createdBy,
	}

	if err := a.db.CreateConfig(cfg); err != nil {
		respondError(w, 500, "failed to create config")
		return
	}
	respondJSON(w, 201, cfg)
}

func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cfg, err := a.db.GetConfig(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "config not found")
			return
		}
		respondError(w, 500, "failed to get config")
		return
	}
	respondJSON(w, 200, cfg)
}

// validateConfigRequest carries the candidate YAML to validate. Wrapped in
// JSON rather than text/yaml so the endpoint can grow extra options
// (workload_id, agent set hint) without breaking clients.
type validateConfigRequest struct {
	Content string `json:"content"`
}

// handleValidateConfig runs the deeper otelcol-binary validation on a
// candidate configuration without requiring a target workload. The light
// validator already runs at /api/workloads/{id}/config/validate (and as a
// safety net inside push); this endpoint complements it by catching
// per-component schema errors that the light parser cannot detect.
//
// Returns 200 with a validator.Result body. The Valid flag distinguishes a
// passing run from a failing one; non-200 statuses are reserved for
// transport-level failures (bad JSON, validator unavailable on the server).
//
// Audit: a "config.validate" event is emitted only when validation succeeds.
// Failed validations are not audited — operators iterate on draft YAML many
// times before pushing, so logging every failed attempt would drown signal
// in noise. The push handler still audits the actual push, which is the
// security-relevant action.
func (a *API) handleValidateConfig(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, 400, "failed to read body")
		return
	}
	//nolint:errcheck // deferred cleanup of fully-read request body; net/http server also closes it
	defer r.Body.Close()

	var req validateConfigRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, 400, "invalid JSON body")
		return
	}
	if req.Content == "" {
		respondError(w, 400, "content is required")
		return
	}

	if a.configValidator == nil {
		// Server-side configuration error: the binary path was not wired.
		// Surface as 503 so operators can distinguish from a user-side
		// validation failure.
		respondJSON(w, http.StatusServiceUnavailable, validator.Result{
			Errors: []validator.Error{{
				Code:    "validator_unavailable",
				Message: "server-side validator is not configured",
			}},
		})
		return
	}

	result := a.configValidator.Validate(r.Context(), []byte(req.Content))

	if result.Valid {
		a.audit.Log(r.Context(), auditEventFromRequest(r, "config.validate", "config", "", ""))
	}
	respondJSON(w, 200, result)
}
