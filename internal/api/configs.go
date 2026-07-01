package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/internal/oteldiff"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type createConfigRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type configDiffRequest struct {
	BaseYAML   string `json:"base_yaml"`
	TargetYAML string `json:"target_yaml"`
}

func (a *API) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	savedConfigs, err := a.db.ListConfigs()
	if err != nil {
		respondError(w, 500, "failed to list configs")
		return
	}

	kind := r.URL.Query().Get("kind")
	category := r.URL.Query().Get("category")
	stack := r.URL.Query().Get("stack")
	configs := make([]models.Config, 0, len(savedConfigs)+len(builtInConfigTemplates()))
	for _, cfg := range savedConfigs {
		if cfg.Kind == "" {
			cfg.Kind = models.ConfigKindSaved
		}
		if cfg.Status == "" {
			cfg.Status = models.ConfigStatusReady
		}
		if configMatchesLibraryFilters(cfg, kind, category, stack) {
			configs = append(configs, cfg)
		}
	}
	for _, cfg := range builtInConfigTemplates() {
		if configMatchesLibraryFilters(cfg, kind, category, stack) {
			configs = append(configs, cfg)
		}
	}
	respondJSON(w, 200, configs)
}

func (a *API) handleDiffConfigs(w http.ResponseWriter, r *http.Request) {
	var req configDiffRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.BaseYAML == "" || req.TargetYAML == "" {
		respondError(w, 400, "base_yaml and target_yaml are required")
		return
	}
	respondJSON(w, 200, oteldiff.Compare([]byte(req.BaseYAML), []byte(req.TargetYAML)))
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
		Kind:      models.ConfigKindSaved,
		Status:    models.ConfigStatusReady,
	}

	if err := a.db.CreateConfig(cfg); err != nil {
		respondError(w, 500, "failed to create config")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "config.create", "config", cfg.ID, ""); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, 201, cfg)
}

func (a *API) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if cfg, ok := findBuiltInConfigTemplate(id); ok {
		respondJSON(w, 200, cfg)
		return
	}
	cfg, err := a.db.GetConfig(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, 404, "config not found")
			return
		}
		respondError(w, 500, "failed to get config")
		return
	}
	if cfg.Kind == "" {
		cfg.Kind = models.ConfigKindSaved
	}
	if cfg.Status == "" {
		cfg.Status = models.ConfigStatusReady
	}
	respondJSON(w, 200, cfg)
}
