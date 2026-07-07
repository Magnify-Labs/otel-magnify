package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/magnify-labs/otel-magnify/internal/validator"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type gitOpsProvider interface {
	ExportConfig(r *http.Request, req gitOpsExportRequest) (models.GitOpsExportResult, error)
	UpsertValidationComment(r *http.Request, req gitOpsValidationCommentRequest) (models.GitOpsCommentResult, error)
	HandleWebhook(r *http.Request, req gitOpsWebhookRequest) (models.GitOpsWebhookResult, error)
}

type gitOpsExportRequest struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	Path       string `json:"path"`
	BaseBranch string `json:"base_branch"`
	Branch     string `json:"branch"`
	Title      string `json:"title"`
	Body       string `json:"body"`

	ConfigID  string `json:"-"`
	Content   string `json:"-"`
	CommitSHA string `json:"-"`
}

type gitOpsValidationCommentRequest struct {
	Provider   string `json:"provider"`
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	Body       string `json:"body"`
}

type gitOpsWebhookRequest struct {
	Provider string
	Event    string
	Payload  []byte
}

type gitOpsValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type gitOpsValidationCommentResult struct {
	Valid    bool                    `json:"valid"`
	Errors   []gitOpsValidationIssue `json:"errors,omitempty"`
	Warnings []string                `json:"warnings,omitempty"`
}

type gitOpsValidationGateResult struct {
	Valid      bool                           `json:"valid"`
	Reason     string                         `json:"reason,omitempty"`
	Provider   string                         `json:"provider"`
	SourcePath string                         `json:"source_path"`
	SourceRef  string                         `json:"source_ref,omitempty"`
	CommitSHA  string                         `json:"commit_sha"`
	Latest     *models.GitOpsValidationStatus `json:"latest,omitempty"`
}

var (
	gitOpsProviderOverride       gitOpsProvider
	gitOpsWebhookSecretOverrides = map[string]string{}
	gitOpsHTTPClient             = &http.Client{Timeout: 10 * time.Second}
	sensitiveCommentValueRegexp  = regexp.MustCompile(`(?i)(secret[_-]?token|access[_-]?token|private[_-]?token|authorization|bearer|password|api[_-]?key|client[_-]?secret)([=: ]+)([^\s,;&]+)`)
	credentialURLUserinfoRegexp  = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	gitOpsSourcePathMarkerRegexp = regexp.MustCompile(`(?i)otel-magnify:\s*path=([^\s<]+)`)
)

const gitOpsValidationCommentMarker = "<!-- otel-magnify: validation-comment -->"

func (a *API) handleExportConfigToGit(w http.ResponseWriter, r *http.Request) {
	configID := chi.URLParam(r, "id")
	var req gitOpsExportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := validateGitOpsExportRequest(&req); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	cfg, err := a.db.GetConfig(configID)
	if err != nil {
		respondError(w, http.StatusNotFound, "config not found")
		return
	}
	validation := validator.Validate([]byte(cfg.Content), nil)
	if !validation.Valid {
		respondJSON(w, http.StatusBadRequest, map[string]any{"error": "configuration failed validation", "validation": validation})
		return
	}
	provider, err := gitOpsProviderFor(req.Provider)
	if err != nil {
		respondError(w, http.StatusNotImplemented, err.Error())
		return
	}
	gitOpsValidation, err := a.gitOpsValidationGate(req, cfg)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load gitops validation status")
		return
	}
	if !gitOpsValidation.Valid {
		respondJSON(w, http.StatusBadRequest, map[string]any{"error": "gitops validation has not passed", "validation": validation, "gitops_validation": gitOpsValidation})
		return
	}
	req.ConfigID = cfg.ID
	req.Content = cfg.Content
	req.CommitSHA = cfg.CommitSHA
	result, err := provider.ExportConfig(r, req)
	if err != nil {
		respondError(w, http.StatusBadGateway, redactGitOpsText(err.Error()))
		return
	}
	commentBody := buildGitOpsValidationComment(cfg, validationCommentFromValidator(validation))
	comment, err := provider.UpsertValidationComment(r, gitOpsValidationCommentRequest{Provider: req.Provider, Repository: req.Repository, Number: result.Number, Body: commentBody})
	if err != nil {
		respondError(w, http.StatusBadGateway, redactGitOpsText(err.Error()))
		return
	}
	if !a.emitAudit(w, r, sideEffectApplied, "config.export_git", "config", cfg.ID, gitOpsExportAuditDetail(req, result, comment)) {
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"result": result, "comment": comment, "validation": validation})
}

func (a *API) handleGitOpsWebhook(w http.ResponseWriter, r *http.Request) {
	providerName := strings.ToLower(chi.URLParam(r, "provider"))
	if providerName != "github" && providerName != "gitlab" {
		respondError(w, http.StatusBadRequest, "unsupported gitops provider")
		return
	}
	secret := gitOpsWebhookSecret(providerName)
	if secret == "" {
		respondError(w, http.StatusNotImplemented, providerName+" gitops webhook is disabled: webhook secret is not configured")
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid webhook payload")
		return
	}
	if !verifyGitOpsWebhookSignature(providerName, secret, payload, r.Header) {
		respondError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	provider, err := gitOpsProviderFor(providerName)
	if err != nil {
		respondError(w, http.StatusNotImplemented, err.Error())
		return
	}
	result, err := provider.HandleWebhook(r, gitOpsWebhookRequest{Provider: providerName, Event: gitOpsWebhookEvent(providerName, r.Header), Payload: payload})
	if err != nil {
		respondError(w, http.StatusBadGateway, redactGitOpsText(err.Error()))
		return
	}
	if result.SourcePath != "" && result.CommitSHA != "" {
		if err := a.db.RecordGitOpsValidationStatus(models.GitOpsValidationStatus{
			Provider:   result.Provider,
			Event:      result.Event,
			Action:     result.Action,
			Status:     result.ValidationStatus,
			SourcePath: result.SourcePath,
			SourceRef:  result.SourceRef,
			CommitSHA:  result.CommitSHA,
			ObservedAt: time.Now().UTC(),
		}); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to store gitops validation status")
			return
		}
	}
	respondJSON(w, http.StatusOK, result)
}

func validateGitOpsExportRequest(req *gitOpsExportRequest) error {
	req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
	if req.Provider != "github" && req.Provider != "gitlab" {
		return errors.New("provider must be github or gitlab")
	}
	if err := validateGitOpsRepository(req.Provider, req.Repository); err != nil {
		return err
	}
	for name, value := range map[string]string{"repository": req.Repository, "path": req.Path, "base_branch": req.BaseBranch, "branch": req.Branch, "title": req.Title} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if err := validateGitPath(req.Path); err != nil {
		return fmt.Errorf("invalid git path: %w", err)
	}
	if err := validateGitRef(req.BaseBranch); err != nil {
		return fmt.Errorf("invalid git ref: base_branch: %w", err)
	}
	if err := validateGitRef(req.Branch); err != nil {
		return fmt.Errorf("invalid git ref: branch: %w", err)
	}
	return nil
}

func validateGitOpsRepository(provider, repository string) error {
	raw := repository
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return errors.New("repository is required")
	}
	if repository != raw || strings.ContainsAny(repository, "\x00\r\n	 ") || strings.Contains(repository, `\`) || strings.ContainsAny(repository, "?#") || strings.Contains(repository, "://") || strings.HasPrefix(repository, "/") || strings.HasSuffix(repository, "/") {
		return errors.New("invalid repository")
	}
	parts := strings.Split(repository, "/")
	if provider == "github" && len(parts) != 2 {
		return errors.New("invalid repository")
	}
	if provider == "gitlab" && len(parts) < 2 {
		return errors.New("invalid repository")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid repository")
		}
	}
	return nil
}

func (a *API) gitOpsValidationGate(req gitOpsExportRequest, cfg models.Config) (gitOpsValidationGateResult, error) {
	gate := gitOpsValidationGateResult{
		Provider:   req.Provider,
		SourcePath: req.Path,
		SourceRef:  cfg.GitRef,
		CommitSHA:  cfg.CommitSHA,
	}
	if gate.SourceRef == "" {
		gate.Reason = "missing source_ref"
		return gate, nil
	}
	if gate.CommitSHA == "" {
		gate.Reason = "missing commit_sha"
		return gate, nil
	}
	status, err := a.db.GetLatestGitOpsValidationStatus(req.Provider, req.Path, cfg.GitRef, cfg.CommitSHA)
	if err != nil {
		return gate, err
	}
	gate.Latest = status
	if status == nil {
		gate.Reason = "missing validation pass"
		return gate, nil
	}
	if !gitOpsValidationStatusPassed(status.Status) {
		gate.Reason = "latest validation status is " + status.Status
		return gate, nil
	}
	gate.Valid = true
	return gate, nil
}

func gitOpsValidationStatusPassed(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "pass", "passed", "success", "succeeded", "ok":
		return true
	default:
		return false
	}
}

func gitOpsProviderFor(name string) (gitOpsProvider, error) {
	if gitOpsProviderOverride != nil {
		return gitOpsProviderOverride, nil
	}
	switch strings.ToLower(name) {
	case "github":
		token := os.Getenv("GITOPS_GITHUB_TOKEN")
		if token == "" {
			return nil, errors.New("github gitops provider is disabled: GITOPS_GITHUB_TOKEN is not configured")
		}
		return &githubGitOpsProvider{token: token, client: gitOpsHTTPClient}, nil
	case "gitlab":
		token := os.Getenv("GITOPS_GITLAB_TOKEN")
		if token == "" {
			return nil, errors.New("gitlab gitops provider is disabled: GITOPS_GITLAB_TOKEN is not configured")
		}
		return &gitlabGitOpsProvider{token: token, baseURL: envDefault("GITOPS_GITLAB_BASE_URL", "https://gitlab.com"), client: gitOpsHTTPClient}, nil
	default:
		return nil, errors.New("unsupported gitops provider")
	}
}

func gitOpsWebhookSecret(provider string) string {
	if secret, ok := gitOpsWebhookSecretOverrides[provider]; ok {
		return secret
	}
	if provider == "github" {
		return os.Getenv("GITOPS_GITHUB_WEBHOOK_SECRET")
	}
	return os.Getenv("GITOPS_GITLAB_WEBHOOK_SECRET")
}

func verifyGitOpsWebhookSignature(provider, secret string, payload []byte, header http.Header) bool {
	if provider == "github" {
		got := header.Get("X-Hub-Signature-256")
		if !strings.HasPrefix(got, "sha256=") {
			return false
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payload)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(got), []byte(want))
	}
	return hmac.Equal([]byte(header.Get("X-Gitlab-Token")), []byte(secret))
}

func gitOpsWebhookEvent(provider string, header http.Header) string {
	if provider == "github" {
		return header.Get("X-GitHub-Event")
	}
	return header.Get("X-Gitlab-Event")
}

func validationCommentFromValidator(result validator.Result) gitOpsValidationCommentResult {
	out := gitOpsValidationCommentResult{Valid: result.Valid}
	for _, err := range result.Errors {
		out.Errors = append(out.Errors, gitOpsValidationIssue{Code: err.Code, Message: err.Message, Path: err.Path})
	}
	for _, warning := range result.Warnings {
		if warning.Message != "" {
			out.Warnings = append(out.Warnings, warning.Message)
		}
	}
	return out
}

func buildGitOpsValidationComment(cfg models.Config, result gitOpsValidationCommentResult) string {
	status := "pass"
	if !result.Valid {
		status = "fail"
	}
	var b strings.Builder
	b.WriteString("## otel-magnify validation: ")
	b.WriteString(status)
	b.WriteString("\n\n")
	b.WriteString("Source:\n")
	b.WriteString("- Path: ")
	b.WriteString(redactGitOpsText(cfg.GitPath))
	b.WriteString("\n- Ref: ")
	b.WriteString(redactGitOpsText(cfg.GitRef))
	b.WriteString("\n- Commit SHA: ")
	b.WriteString(redactGitOpsText(cfg.CommitSHA))
	if cfg.GitURL != "" {
		b.WriteString("\n- Repository: ")
		b.WriteString(redactGitOpsText(sanitizeGitURL(cfg.GitURL)))
	}
	b.WriteString("\n\n")
	if result.Valid {
		b.WriteString("Validation passed. No blocking errors were found.\n")
	} else {
		b.WriteString("Validation failed with ")
		b.WriteString(strconv.Itoa(len(result.Errors)))
		b.WriteString(" error(s).\n")
	}
	if len(result.Errors) > 0 {
		b.WriteString("\nErrors:\n")
		for _, issue := range result.Errors {
			b.WriteString("- ")
			if issue.Code != "" {
				b.WriteString("[")
				b.WriteString(redactGitOpsText(issue.Code))
				b.WriteString("] ")
			}
			if issue.Path != "" {
				b.WriteString(redactGitOpsText(issue.Path))
				b.WriteString(": ")
			}
			b.WriteString(redactGitOpsText(issue.Message))
			b.WriteString("\n")
		}
	}
	if len(result.Warnings) > 0 {
		b.WriteString("\nWarnings:\n")
		for _, warning := range result.Warnings {
			b.WriteString("- ")
			b.WriteString(redactGitOpsText(warning))
			b.WriteString("\n")
		}
	}
	return gitOpsValidationCommentBodyWithMarker(b.String())
}

func gitOpsValidationCommentBodyWithMarker(body string) string {
	if strings.Contains(body, gitOpsValidationCommentMarker) {
		return body
	}
	return strings.TrimRight(body, "\n") + "\n\n" + gitOpsValidationCommentMarker + "\n"
}

func redactGitOpsText(s string) string {
	s = credentialURLUserinfoRegexp.ReplaceAllString(s, "${1}")
	return sensitiveCommentValueRegexp.ReplaceAllString(s, "[redacted]")
}

func gitOpsExportAuditDetail(req gitOpsExportRequest, result models.GitOpsExportResult, comment models.GitOpsCommentResult) string {
	return redactGitOpsText(fmt.Sprintf("provider=%s repository=%s branch=%s pr_number=%d commit=%s comment_id=%s", req.Provider, req.Repository, result.Branch, result.Number, result.CommitSHA, comment.CommentID))
}

func resetGitOpsProviderForTest(t interface{ Cleanup(func()) }, provider gitOpsProvider) {
	old := gitOpsProviderOverride
	gitOpsProviderOverride = provider
	t.Cleanup(func() { gitOpsProviderOverride = old })
}

func setGitOpsWebhookSecretForTest(t interface{ Cleanup(func()) }, provider, secret string) {
	old, hadOld := gitOpsWebhookSecretOverrides[provider]
	gitOpsWebhookSecretOverrides[provider] = secret
	t.Cleanup(func() {
		if hadOld {
			gitOpsWebhookSecretOverrides[provider] = old
		} else {
			delete(gitOpsWebhookSecretOverrides, provider)
		}
	})
}

type githubGitOpsProvider struct {
	token  string
	client *http.Client
}

func (p *githubGitOpsProvider) ExportConfig(r *http.Request, req gitOpsExportRequest) (models.GitOpsExportResult, error) {
	baseSHA, err := p.githubBaseRefSHA(r, req.Repository, req.BaseBranch)
	if err != nil {
		return models.GitOpsExportResult{}, err
	}
	if err := p.githubJSON(r, http.MethodPost, "/repos/"+req.Repository+"/git/refs", map[string]string{"ref": "refs/heads/" + req.Branch, "sha": baseSHA}, nil); err != nil {
		return models.GitOpsExportResult{}, err
	}
	var contentResp struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := p.githubJSON(r, http.MethodPut, "/repos/"+req.Repository+"/contents/"+req.Path, map[string]string{"message": req.Title, "content": base64.StdEncoding.EncodeToString([]byte(req.Content)), "branch": req.Branch}, &contentResp); err != nil {
		return models.GitOpsExportResult{}, err
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}
	if err := p.githubJSON(r, http.MethodPost, "/repos/"+req.Repository+"/pulls", map[string]string{"title": req.Title, "head": req.Branch, "base": req.BaseBranch, "body": gitOpsPRBodyWithSourceMarker(req.Body, req.Path)}, &pr); err != nil {
		return models.GitOpsExportResult{}, err
	}
	return models.GitOpsExportResult{Provider: "github", URL: pr.HTMLURL, Number: pr.Number, Branch: req.Branch, CommitSHA: contentResp.Commit.SHA}, nil
}

func (p *githubGitOpsProvider) UpsertValidationComment(r *http.Request, req gitOpsValidationCommentRequest) (models.GitOpsCommentResult, error) {
	body := gitOpsValidationCommentBodyWithMarker(req.Body)
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", req.Repository, req.Number)
	var comments []struct {
		HTMLURL string `json:"html_url"`
		ID      int64  `json:"id"`
		Body    string `json:"body"`
	}
	if err := p.githubJSON(r, http.MethodGet, path, nil, &comments); err != nil {
		return models.GitOpsCommentResult{}, err
	}
	var comment struct {
		HTMLURL string `json:"html_url"`
		ID      int64  `json:"id"`
	}
	for _, existing := range comments {
		if strings.Contains(existing.Body, gitOpsValidationCommentMarker) {
			updatePath := fmt.Sprintf("/repos/%s/issues/comments/%d", req.Repository, existing.ID)
			if err := p.githubJSON(r, http.MethodPatch, updatePath, map[string]string{"body": body}, &comment); err != nil {
				return models.GitOpsCommentResult{}, err
			}
			return models.GitOpsCommentResult{Provider: "github", URL: comment.HTMLURL, CommentID: strconv.FormatInt(comment.ID, 10)}, nil
		}
	}
	if err := p.githubJSON(r, http.MethodPost, path, map[string]string{"body": body}, &comment); err != nil {
		return models.GitOpsCommentResult{}, err
	}
	return models.GitOpsCommentResult{Provider: "github", URL: comment.HTMLURL, CommentID: strconv.FormatInt(comment.ID, 10)}, nil
}

func (p *githubGitOpsProvider) HandleWebhook(_ *http.Request, req gitOpsWebhookRequest) (models.GitOpsWebhookResult, error) {
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			Body string `json:"body"`
			Head struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
	}
	_ = json.Unmarshal(req.Payload, &payload)
	return models.GitOpsWebhookResult{Provider: "github", Event: req.Event, Action: payload.Action, ValidationStatus: "received", SourcePath: gitOpsSourcePathFromMarker(payload.PullRequest.Body), SourceRef: payload.PullRequest.Head.Ref, CommitSHA: payload.PullRequest.Head.SHA}, nil
}

func (p *githubGitOpsProvider) githubBaseRefSHA(r *http.Request, repo, branch string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := p.githubJSON(r, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+url.PathEscape(branch), nil, &ref); err != nil {
		return "", err
	}
	return ref.Object.SHA, nil
}

func (p *githubGitOpsProvider) githubJSON(r *http.Request, method, path string, body any, out any) error {
	return doGitOpsJSON(r, p.client, method, "https://api.github.com"+path, map[string]string{"Authorization": "Bearer " + p.token, "Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}, body, out)
}

type gitlabGitOpsProvider struct {
	token   string
	baseURL string
	client  *http.Client
}

func (p *gitlabGitOpsProvider) ExportConfig(r *http.Request, req gitOpsExportRequest) (models.GitOpsExportResult, error) {
	project := url.PathEscape(req.Repository)
	apiBase := strings.TrimRight(p.baseURL, "/") + "/api/v4/projects/" + project
	if err := doGitOpsJSON(r, p.client, http.MethodPost, apiBase+"/repository/branches", map[string]string{"PRIVATE-TOKEN": p.token}, map[string]string{"branch": req.Branch, "ref": req.BaseBranch}, nil); err != nil {
		return models.GitOpsExportResult{}, err
	}
	var commit struct {
		ID string `json:"id"`
	}
	commitBody := map[string]any{"branch": req.Branch, "commit_message": req.Title, "actions": []map[string]string{{"action": "update", "file_path": req.Path, "content": req.Content}}}
	if err := doGitOpsJSON(r, p.client, http.MethodPost, apiBase+"/repository/commits", map[string]string{"PRIVATE-TOKEN": p.token}, commitBody, &commit); err != nil {
		return models.GitOpsExportResult{}, err
	}
	var mr struct {
		WebURL string `json:"web_url"`
		IID    int    `json:"iid"`
	}
	mrBody := map[string]string{"source_branch": req.Branch, "target_branch": req.BaseBranch, "title": req.Title, "description": gitOpsPRBodyWithSourceMarker(req.Body, req.Path)}
	if err := doGitOpsJSON(r, p.client, http.MethodPost, apiBase+"/merge_requests", map[string]string{"PRIVATE-TOKEN": p.token}, mrBody, &mr); err != nil {
		return models.GitOpsExportResult{}, err
	}
	return models.GitOpsExportResult{Provider: "gitlab", URL: mr.WebURL, Number: mr.IID, Branch: req.Branch, CommitSHA: commit.ID}, nil
}

func (p *gitlabGitOpsProvider) UpsertValidationComment(r *http.Request, req gitOpsValidationCommentRequest) (models.GitOpsCommentResult, error) {
	project := url.PathEscape(req.Repository)
	apiBase := strings.TrimRight(p.baseURL, "/") + "/api/v4/projects/" + project
	path := fmt.Sprintf("%s/merge_requests/%d/notes", apiBase, req.Number)
	body := gitOpsValidationCommentBodyWithMarker(req.Body)
	var notes []struct {
		ID     int64  `json:"id"`
		WebURL string `json:"web_url"`
		Body   string `json:"body"`
	}
	if err := doGitOpsJSON(r, p.client, http.MethodGet, path, map[string]string{"PRIVATE-TOKEN": p.token}, nil, &notes); err != nil {
		return models.GitOpsCommentResult{}, err
	}
	var note struct {
		ID     int64  `json:"id"`
		WebURL string `json:"web_url"`
	}
	for _, existing := range notes {
		if strings.Contains(existing.Body, gitOpsValidationCommentMarker) {
			updatePath := fmt.Sprintf("%s/merge_requests/%d/notes/%d", apiBase, req.Number, existing.ID)
			if err := doGitOpsJSON(r, p.client, http.MethodPut, updatePath, map[string]string{"PRIVATE-TOKEN": p.token}, map[string]string{"body": body}, &note); err != nil {
				return models.GitOpsCommentResult{}, err
			}
			return models.GitOpsCommentResult{Provider: "gitlab", URL: note.WebURL, CommentID: strconv.FormatInt(note.ID, 10)}, nil
		}
	}
	if err := doGitOpsJSON(r, p.client, http.MethodPost, path, map[string]string{"PRIVATE-TOKEN": p.token}, map[string]string{"body": body}, &note); err != nil {
		return models.GitOpsCommentResult{}, err
	}
	return models.GitOpsCommentResult{Provider: "gitlab", URL: note.WebURL, CommentID: strconv.FormatInt(note.ID, 10)}, nil
}

func (p *gitlabGitOpsProvider) HandleWebhook(_ *http.Request, req gitOpsWebhookRequest) (models.GitOpsWebhookResult, error) {
	var payload struct {
		ObjectKind       string `json:"object_kind"`
		ObjectAttributes struct {
			Action       string `json:"action"`
			Description  string `json:"description"`
			SourceBranch string `json:"source_branch"`
			LastCommit   struct {
				ID string `json:"id"`
			} `json:"last_commit"`
		} `json:"object_attributes"`
	}
	_ = json.Unmarshal(req.Payload, &payload)
	return models.GitOpsWebhookResult{Provider: "gitlab", Event: req.Event, Action: payload.ObjectAttributes.Action, ValidationStatus: "received", SourcePath: gitOpsSourcePathFromMarker(payload.ObjectAttributes.Description), SourceRef: payload.ObjectAttributes.SourceBranch, CommitSHA: payload.ObjectAttributes.LastCommit.ID}, nil
}

func gitOpsPRBodyWithSourceMarker(body, path string) string {
	marker := "<!-- otel-magnify: path=" + path + " -->"
	if strings.Contains(body, marker) {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return marker
	}
	return strings.TrimRight(body, "\n") + "\n\n" + marker
}

func gitOpsSourcePathFromMarker(body string) string {
	match := gitOpsSourcePathMarkerRegexp.FindStringSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	path := match[1]
	if err := validateGitPath(path); err != nil {
		return ""
	}
	return path
}

func doGitOpsJSON(r *http.Request, client *http.Client, method, endpoint string, headers map[string]string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	// #nosec G107,G704 -- endpoints are built by provider implementations from
	// fixed GitHub/GitLab API bases plus validated repository/path/ref inputs;
	// optional GitLab base URL is deployment-controlled configuration.
	req, err := http.NewRequestWithContext(r.Context(), method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// #nosec G107,G704 -- see NewRequestWithContext note above.
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gitops provider returned non-2xx status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
