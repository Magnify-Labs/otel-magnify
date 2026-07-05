package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/internal/configpolicy"
	"github.com/magnify-labs/otel-magnify/internal/oteldiff"
	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type createConfigRequest struct {
	Name       string   `json:"name"`
	Content    string   `json:"content"`
	Kind       string   `json:"kind,omitempty"`
	Status     string   `json:"status,omitempty"`
	Category   string   `json:"category,omitempty"`
	Stack      string   `json:"stack,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	SourceType string   `json:"source_type,omitempty"`
}

type configDiffRequest struct {
	BaseYAML   string                   `json:"base_yaml"`
	TargetYAML string                   `json:"target_yaml"`
	Context    configDiffContextRequest `json:"context,omitempty"`
}

type configDiffContextRequest struct {
	WorkloadID      string                      `json:"workload_id,omitempty"`
	DisplayName     string                      `json:"display_name,omitempty"`
	WorkloadType    string                      `json:"workload_type,omitempty"`
	Type            string                      `json:"type,omitempty"`
	Status          string                      `json:"status,omitempty"`
	Labels          map[string]string           `json:"labels,omitempty"`
	FingerprintKeys map[string]string           `json:"fingerprint_keys,omitempty"`
	FleetPeers      []configDiffWorkloadRequest `json:"fleet_peers,omitempty"`
	BaseLabel       string                      `json:"base_label,omitempty"`
	TargetLabel     string                      `json:"target_label,omitempty"`
	IncludeRawPaths bool                        `json:"include_raw_paths,omitempty"`
}

type configDiffWorkloadRequest struct {
	ID              string            `json:"id,omitempty"`
	DisplayName     string            `json:"display_name,omitempty"`
	WorkloadType    string            `json:"workload_type,omitempty"`
	Type            string            `json:"type,omitempty"`
	Status          string            `json:"status,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	FingerprintKeys map[string]string `json:"fingerprint_keys,omitempty"`
}

type configPolicyPreviewRequest struct {
	CandidateYAML string                      `json:"candidate_yaml"`
	CurrentYAML   string                      `json:"current_yaml,omitempty"`
	TargetYAML    string                      `json:"target_yaml,omitempty"`
	BaseYAML      string                      `json:"base_yaml,omitempty"`
	Target        models.ConfigPolicyTarget   `json:"target"`
	Settings      models.ConfigPolicySettings `json:"settings,omitempty"`
	Context       configPolicyContextRequest  `json:"context,omitempty"`
}

type configPolicyContextRequest struct {
	Environment                string   `json:"environment,omitempty"`
	EndpointAllowlist          []string `json:"endpoint_allowlist,omitempty"`
	CriticalExporters          []string `json:"critical_exporters,omitempty"`
	RequiredResourceAttributes []string `json:"required_resource_attributes,omitempty"`
	MaxSamplingPercentage      float64  `json:"max_sampling_percentage,omitempty"`
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
	redactConfigContent(configs)
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
	respondJSON(w, 200, oteldiff.CompareWithContext([]byte(req.BaseYAML), []byte(req.TargetYAML), a.configDiffBlastRadiusContext(req.Context)))
}

func (a *API) configDiffBlastRadiusContext(req configDiffContextRequest) oteldiff.BlastRadiusContext {
	ctx := oteldiff.BlastRadiusContext{Workload: blastRadiusWorkloadFromDiffContext(req)}
	for _, peer := range req.FleetPeers {
		ctx.FleetPeers = append(ctx.FleetPeers, blastRadiusWorkloadFromDiffPeer(peer))
	}
	if req.WorkloadID == "" || a.db == nil {
		return ctx
	}
	if wl, err := a.db.GetWorkload(req.WorkloadID); err == nil {
		ctx.Workload = blastRadiusWorkloadFromModel(wl)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ctx
	}
	peers, err := a.db.ListWorkloads(false)
	if err != nil {
		return ctx
	}
	ctx.FleetPeers = ctx.FleetPeers[:0]
	for _, peer := range peers {
		if peer.ID == req.WorkloadID {
			continue
		}
		ctx.FleetPeers = append(ctx.FleetPeers, blastRadiusWorkloadFromModel(peer))
	}
	return ctx
}

func blastRadiusWorkloadFromDiffContext(req configDiffContextRequest) oteldiff.BlastRadiusWorkload {
	return oteldiff.BlastRadiusWorkload{ID: req.WorkloadID, DisplayName: req.DisplayName, Type: firstNonEmpty(req.WorkloadType, req.Type), Status: req.Status, Labels: req.Labels, FingerprintKeys: req.FingerprintKeys}
}

func blastRadiusWorkloadFromDiffPeer(req configDiffWorkloadRequest) oteldiff.BlastRadiusWorkload {
	return oteldiff.BlastRadiusWorkload{ID: req.ID, DisplayName: req.DisplayName, Type: firstNonEmpty(req.WorkloadType, req.Type), Status: req.Status, Labels: req.Labels, FingerprintKeys: req.FingerprintKeys}
}

func blastRadiusWorkloadFromModel(wl models.Workload) oteldiff.BlastRadiusWorkload {
	return oteldiff.BlastRadiusWorkload{ID: wl.ID, DisplayName: wl.DisplayName, Type: wl.Type, Status: wl.Status, Labels: map[string]string(wl.Labels), FingerprintKeys: map[string]string(wl.FingerprintKeys)}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *API) handlePreviewConfigPolicy(w http.ResponseWriter, r *http.Request) {
	var req configPolicyPreviewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.CandidateYAML) == "" {
		req.CandidateYAML = req.TargetYAML
	}
	if strings.TrimSpace(req.CurrentYAML) == "" {
		req.CurrentYAML = req.BaseYAML
	}
	if req.Target.Environment == "" {
		req.Target.Environment = req.Context.Environment
	}
	if req.Settings.AllowedOTLPEndpoints == nil {
		req.Settings.AllowedOTLPEndpoints = req.Context.EndpointAllowlist
	}
	if len(req.Settings.CriticalExporters) == 0 {
		req.Settings.CriticalExporters = req.Context.CriticalExporters
	}
	if len(req.Settings.RequiredResourceAttributes) == 0 {
		req.Settings.RequiredResourceAttributes = req.Context.RequiredResourceAttributes
	}
	if req.Context.MaxSamplingPercentage > 0 {
		req.Settings.Sampling.MaxPercentage = req.Context.MaxSamplingPercentage
	}
	if strings.TrimSpace(req.CandidateYAML) == "" {
		respondError(w, 400, "target_yaml is required")
		return
	}
	result := configpolicy.NewDefaultEngine().Evaluate(configpolicy.EvaluationRequest{
		CurrentYAML:   req.CurrentYAML,
		CandidateYAML: req.CandidateYAML,
		Target:        req.Target,
		Settings:      req.Settings,
	})
	respondJSON(w, 200, result)
}

func redactConfigContent(configs []models.Config) {
	for i := range configs {
		configs[i].Content = ""
	}
}

func (a *API) handleCreateConfig(w http.ResponseWriter, r *http.Request) {
	var req createConfigRequest
	if !decodeJSONBody(w, r, &req) {
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
		ID:         hash,
		Name:       req.Name,
		Content:    req.Content,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  createdBy,
		Kind:       normalizeCreateConfigKind(req.Kind),
		Status:     normalizeCreateConfigStatus(req.Kind, req.Status),
		Category:   strings.TrimSpace(req.Category),
		Stack:      strings.TrimSpace(req.Stack),
		Tags:       sanitizedCreateConfigTags(req.Tags),
		SourceType: normalizeCreateConfigSourceType(req.SourceType),
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

func normalizeCreateConfigKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case models.ConfigKindDraft:
		return models.ConfigKindDraft
	default:
		return models.ConfigKindSaved
	}
}

func normalizeCreateConfigStatus(kind string, _ string) string {
	if normalizeCreateConfigKind(kind) == models.ConfigKindDraft {
		return models.ConfigStatusDraft
	}
	return models.ConfigStatusReady
}

func normalizeCreateConfigSourceType(sourceType string) string {
	switch strings.TrimSpace(sourceType) {
	case models.ConfigSourceMigrationAssistant:
		return models.ConfigSourceMigrationAssistant
	default:
		return models.ConfigSourceManual
	}
}

func sanitizedCreateConfigTags(tags []string) []string {
	result := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}

func (a *API) handleImportConfigFromGit(w http.ResponseWriter, r *http.Request) {
	var req gitImportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if err := validateGitImportRequest(req); err != nil {
		respondError(w, 400, err.Error())
		return
	}

	result, err := gitImportConfig(r.Context(), req)
	if err != nil {
		respondError(w, 400, redactGitOpsText(err.Error()))
		return
	}
	validation := validator.Validate([]byte(result.Content), nil)
	if !validation.Valid {
		respondJSON(w, 400, map[string]any{
			"error":      "configuration failed validation",
			"validation": validation,
		})
		return
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(result.Content)))
	createdBy := ""
	if info := ext.UserInfoFromContext(r.Context()); info != nil {
		createdBy = info.Email
	}
	importedAt := result.ImportedAt.UTC()
	if result.GitURL == "" {
		result.GitURL = sanitizeGitURL(req.GitURL)
	}
	if result.GitProvider == "" {
		result.GitProvider = gitProviderFromURL(result.GitURL)
	}
	cfg := models.Config{
		ID:          hash,
		Name:        req.Name,
		Content:     result.Content,
		CreatedAt:   time.Now().UTC(),
		CreatedBy:   createdBy,
		Kind:        models.ConfigKindSaved,
		Status:      models.ConfigStatusReady,
		SourceType:  models.ConfigSourceGit,
		GitURL:      result.GitURL,
		GitProvider: result.GitProvider,
		GitRef:      req.GitRef,
		GitPath:     req.GitPath,
		CommitSHA:   result.CommitSHA,
		ImportedAt:  &importedAt,
	}

	if err := a.db.CreateConfig(cfg); err != nil {
		respondError(w, 500, "failed to create config")
		return
	}
	if err := audit.Emit(r.Context(), a.audit, "config.import_git", "config", cfg.ID, cfg.CommitSHA); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	respondJSON(w, 201, map[string]any{
		"config":     cfg,
		"validation": validation,
	})
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
