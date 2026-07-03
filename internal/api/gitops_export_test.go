package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type fakeGitOpsProvider struct {
	exportReq  gitOpsExportRequest
	exportResp models.GitOpsExportResult
	commentReq gitOpsValidationCommentRequest
	webhookReq gitOpsWebhookRequest
}

func (f *fakeGitOpsProvider) ExportConfig(_ *http.Request, req gitOpsExportRequest) (models.GitOpsExportResult, error) {
	f.exportReq = req
	if f.exportResp.Provider == "" {
		f.exportResp = models.GitOpsExportResult{Provider: req.Provider, URL: "https://github.com/acme/collectors/pull/42", Number: 42, Branch: req.Branch, CommitSHA: "0123456789abcdef0123456789abcdef01234567"}
	}
	return f.exportResp, nil
}

func (f *fakeGitOpsProvider) UpsertValidationComment(_ *http.Request, req gitOpsValidationCommentRequest) (models.GitOpsCommentResult, error) {
	f.commentReq = req
	return models.GitOpsCommentResult{Provider: req.Provider, URL: "https://github.com/acme/collectors/pull/42#issuecomment-1", CommentID: "1"}, nil
}

func (f *fakeGitOpsProvider) HandleWebhook(_ *http.Request, req gitOpsWebhookRequest) (models.GitOpsWebhookResult, error) {
	f.webhookReq = req
	return models.GitOpsWebhookResult{Provider: req.Provider, Event: req.Event, Action: "opened", ValidationStatus: "pass", SourcePath: "otel/collector.yaml", SourceRef: "refs/pull/42/head", CommitSHA: "0123456789abcdef0123456789abcdef01234567"}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestExportConfigAsPR_UnconfiguredProviderReturnsDisabled(t *testing.T) {
	db, router, _ := newTestAPI(t)
	cfg := seedExportConfig(t, db, "cfg-valid", validGitOpsYAML())
	resetGitOpsProviderForTest(t, nil)
	t.Setenv("GITOPS_GITHUB_TOKEN", "")

	req := authedJSONRequest(t, http.MethodPost, "/api/configs/"+cfg.ID+"/export/git", `{"provider":"github","repository":"acme/collectors","path":"otel/collector.yaml","base_branch":"main","branch":"otel-magnify/cfg-valid","title":"Update collector config"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertJSONErrorContains(t, rec.Body.String(), "github gitops provider is disabled")
}

func TestExportConfigAsPR_RejectsValidationFailure(t *testing.T) {
	db, router, _ := newTestAPI(t)
	cfg := seedExportConfig(t, db, "cfg-invalid", "receivers:\n  otlp: {}\n")
	fake := &fakeGitOpsProvider{}
	resetGitOpsProviderForTest(t, fake)

	req := authedJSONRequest(t, http.MethodPost, "/api/configs/"+cfg.ID+"/export/git", `{"provider":"github","repository":"acme/collectors","path":"otel/collector.yaml","base_branch":"main","branch":"otel-magnify/cfg-invalid","title":"Update collector config"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.exportReq.ConfigID != "" {
		t.Fatalf("provider called despite validation failure: %+v", fake.exportReq)
	}
	if !strings.Contains(rec.Body.String(), "configuration failed validation") || !strings.Contains(rec.Body.String(), "missing_service") {
		t.Fatalf("validation details missing: %s", rec.Body.String())
	}
}

func TestExportConfigAsPR_RejectsUnsafeGitRefBeforeProviderCall(t *testing.T) {
	db, router, _ := newTestAPI(t)
	cfg := seedExportConfig(t, db, "cfg-valid", validGitOpsYAML())
	fake := &fakeGitOpsProvider{}
	resetGitOpsProviderForTest(t, fake)

	req := authedJSONRequest(t, http.MethodPost, "/api/configs/"+cfg.ID+"/export/git", `{"provider":"github","repository":"acme/collectors","path":"otel/collector.yaml","base_branch":"main","branch":"bad..branch","title":"Update collector config"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.exportReq.ConfigID != "" {
		t.Fatalf("provider called despite unsafe ref: %+v", fake.exportReq)
	}
	assertJSONErrorContains(t, rec.Body.String(), "invalid git ref")
}

func TestExportConfigAsPR_UsesProviderAndReturnsResult(t *testing.T) {
	db, router, _ := newTestAPI(t)
	cfg := seedExportConfig(t, db, "cfg-valid", validGitOpsYAML())
	seedGitOpsValidationPass(t, db, cfg)
	fake := &fakeGitOpsProvider{}
	resetGitOpsProviderForTest(t, fake)

	req := authedJSONRequest(t, http.MethodPost, "/api/configs/"+cfg.ID+"/export/git", `{"provider":"github","repository":"acme/collectors","path":"otel/collector.yaml","base_branch":"main","branch":"otel-magnify/cfg-valid","title":"Update collector config"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.exportReq.ConfigID != cfg.ID || fake.exportReq.Content != validGitOpsYAML() || fake.exportReq.Provider != "github" {
		t.Fatalf("provider request mismatch: %+v", fake.exportReq)
	}
	var resp struct {
		Result     models.GitOpsExportResult `json:"result"`
		Validation struct {
			Valid bool `json:"valid"`
		} `json:"validation"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Validation.Valid || resp.Result.URL == "" || resp.Result.Number != 42 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if !strings.Contains(fake.commentReq.Body, "Warnings:") || !strings.Contains(fake.commentReq.Body, "AvailableComponents") {
		t.Fatalf("validation comment omitted validator warnings: %s", fake.commentReq.Body)
	}
}

func TestExportConfigAsPR_RejectsMissingGitOpsValidationPass(t *testing.T) {
	db, router, _ := newTestAPI(t)
	cfg := seedExportConfig(t, db, "cfg-valid", validGitOpsYAML())
	fake := &fakeGitOpsProvider{}
	resetGitOpsProviderForTest(t, fake)

	req := authedJSONRequest(t, http.MethodPost, "/api/configs/"+cfg.ID+"/export/git", `{"provider":"github","repository":"acme/collectors","path":"otel/collector.yaml","base_branch":"main","branch":"otel-magnify/cfg-valid","title":"Update collector config"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.exportReq.ConfigID != "" {
		t.Fatalf("provider called despite missing gitops validation: %+v", fake.exportReq)
	}
	if !strings.Contains(rec.Body.String(), "gitops validation has not passed") || !strings.Contains(rec.Body.String(), "missing validation pass") {
		t.Fatalf("validation gate details missing: %s", rec.Body.String())
	}
}

func TestExportConfigAsPR_RejectsGitOpsValidationForDifferentRef(t *testing.T) {
	db, router, _ := newTestAPI(t)
	cfg := seedExportConfig(t, db, "cfg-valid", validGitOpsYAML())
	fake := &fakeGitOpsProvider{}
	resetGitOpsProviderForTest(t, fake)
	if err := db.RecordGitOpsValidationStatus(models.GitOpsValidationStatus{Provider: cfg.GitProvider, Event: "pull_request", Action: "synchronize", Status: "pass", SourcePath: cfg.GitPath, SourceRef: "other-ref", CommitSHA: cfg.CommitSHA, ObservedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}

	req := authedJSONRequest(t, http.MethodPost, "/api/configs/"+cfg.ID+"/export/git", `{"provider":"github","repository":"acme/collectors","path":"otel/collector.yaml","base_branch":"main","branch":"otel-magnify/cfg-valid","title":"Update collector config"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.exportReq.ConfigID != "" {
		t.Fatalf("provider called despite mismatched validation ref: %+v", fake.exportReq)
	}
	if !strings.Contains(rec.Body.String(), "gitops validation has not passed") || !strings.Contains(rec.Body.String(), "missing validation pass") {
		t.Fatalf("validation gate details missing: %s", rec.Body.String())
	}
}

func TestGitOpsWebhook_RejectsInvalidSignatureWhenSecretConfigured(t *testing.T) {
	_, router, _ := newTestAPI(t)
	resetGitOpsProviderForTest(t, &fakeGitOpsProvider{})
	setGitOpsWebhookSecretForTest(t, "github", "top-secret")

	req := httptest.NewRequest(http.MethodPost, "/api/gitops/webhooks/github", strings.NewReader(`{"action":"opened"}`))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGitOpsWebhook_AcceptsValidSignatureAndTriggersProvider(t *testing.T) {
	db, router, _ := newTestAPI(t)
	fake := &fakeGitOpsProvider{}
	resetGitOpsProviderForTest(t, fake)
	setGitOpsWebhookSecretForTest(t, "github", "top-secret")
	body := `{"action":"opened"}`
	sig := hmacSHA256Header("top-secret", body)

	req := httptest.NewRequest(http.MethodPost, "/api/gitops/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.webhookReq.Provider != "github" || fake.webhookReq.Event != "pull_request" || string(fake.webhookReq.Payload) != body {
		t.Fatalf("webhook request mismatch: %+v", fake.webhookReq)
	}
	stored, err := db.GetLatestGitOpsValidationStatus("github", "otel/collector.yaml", "refs/pull/42/head", "0123456789abcdef0123456789abcdef01234567")
	if err != nil {
		t.Fatalf("GetLatestGitOpsValidationStatus: %v", err)
	}
	if stored == nil || stored.Status != "pass" || stored.SourceRef != "refs/pull/42/head" {
		t.Fatalf("stored webhook validation status = %+v", stored)
	}
}

func TestGitOpsWebhook_RejectsMissingSignatureWhenSecretConfigured(t *testing.T) {
	_, router, _ := newTestAPI(t)
	resetGitOpsProviderForTest(t, &fakeGitOpsProvider{})
	setGitOpsWebhookSecretForTest(t, "github", "top-secret")

	req := httptest.NewRequest(http.MethodPost, "/api/gitops/webhooks/github", strings.NewReader(`{"action":"opened"}`))
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGitHubWebhookMapsPathRefAndCommitFromPRBodyMarker(t *testing.T) {
	provider := &githubGitOpsProvider{}
	payload := `{"action":"synchronize","pull_request":{"body":"Update config\n\n<!-- otel-magnify: path=otel/collector.yaml -->","head":{"ref":"otel-magnify/cfg-valid","sha":"0123456789abcdef0123456789abcdef01234567"}}}`

	result, err := provider.HandleWebhook(httptest.NewRequest(http.MethodPost, "/", nil), gitOpsWebhookRequest{Provider: "github", Event: "pull_request", Payload: []byte(payload)})
	if err != nil {
		t.Fatal(err)
	}

	if result.SourcePath != "otel/collector.yaml" || result.SourceRef != "otel-magnify/cfg-valid" || result.CommitSHA != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("webhook result did not map source: %+v", result)
	}
}

func TestGitLabWebhookAcceptsTokenAndStoresMappedPathRefCommit(t *testing.T) {
	db, router, _ := newTestAPI(t)
	resetGitOpsProviderForTest(t, nil)
	setGitOpsWebhookSecretForTest(t, "gitlab", "top-secret")
	t.Setenv("GITOPS_GITLAB_TOKEN", "provider-token")
	body := `{"object_kind":"merge_request","object_attributes":{"action":"update","source_branch":"otel-magnify/cfg-valid","description":"Update config\n\n<!-- otel-magnify: path=otel/collector.yaml -->","last_commit":{"id":"fedcba9876543210fedcba9876543210fedcba98"}}}`

	req := httptest.NewRequest(http.MethodPost, "/api/gitops/webhooks/gitlab", strings.NewReader(body))
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	req.Header.Set("X-Gitlab-Token", "top-secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	stored, err := db.GetLatestGitOpsValidationStatus("gitlab", "otel/collector.yaml", "otel-magnify/cfg-valid", "fedcba9876543210fedcba9876543210fedcba98")
	if err != nil {
		t.Fatalf("GetLatestGitOpsValidationStatus: %v", err)
	}
	if stored == nil || stored.Status != "received" || stored.SourceRef != "otel-magnify/cfg-valid" {
		t.Fatalf("stored webhook validation status = %+v", stored)
	}
}

func TestValidationCommentBodyRedactsSecretsAndIncludesProvenance(t *testing.T) {
	result := gitOpsValidationCommentResult{
		Valid:    false,
		Errors:   []gitOpsValidationIssue{{Code: "secret", Message: "bad token SECRET_TOKEN=abc123", Path: "exporters.otlp.headers.Authorization"}},
		Warnings: []string{"endpoint contains Bearer super-secret"},
	}
	body := buildGitOpsValidationComment(models.Config{GitURL: "https://token:secret@github.com/acme/collectors.git", GitRef: "main", GitPath: "otel/collector.yaml", CommitSHA: "0123456789abcdef0123456789abcdef01234567"}, result)

	for _, forbidden := range []string{"SECRET_TOKEN", "abc123", "super-secret", "token:secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("comment leaked %q: %s", forbidden, body)
		}
	}
	for _, required := range []string{"otel-magnify validation", "fail", "otel/collector.yaml", "main", "0123456789abcdef0123456789abcdef01234567", "exporters.otlp.headers.Authorization"} {
		if !strings.Contains(body, required) {
			t.Fatalf("comment missing %q: %s", required, body)
		}
	}
}

func TestGitHubValidationCommentUpdatesExistingBotComment(t *testing.T) {
	var calls []string
	var updatedBody string
	provider := &githubGitOpsProvider{token: "token", client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/acme/collectors/issues/42/comments":
			return jsonResponse(http.StatusOK, `[{"id":123,"html_url":"https://github.com/acme/collectors/pull/42#issuecomment-123","body":"old\n<!-- otel-magnify: validation-comment -->"}]`), nil
		case "PATCH /repos/acme/collectors/issues/comments/123":
			body, _ := io.ReadAll(r.Body)
			updatedBody = string(body)
			return jsonResponse(http.StatusOK, `{"id":123,"html_url":"https://github.com/acme/collectors/pull/42#issuecomment-123"}`), nil
		default:
			return jsonResponse(http.StatusNotFound, `{}`), nil
		}
	})}}

	result, err := provider.UpsertValidationComment(httptest.NewRequest(http.MethodPost, "/", nil), gitOpsValidationCommentRequest{Provider: "github", Repository: "acme/collectors", Number: 42, Body: "new validation body"})
	if err != nil {
		t.Fatalf("%v; calls=%v", err, calls)
	}

	if result.CommentID != "123" || result.URL == "" {
		t.Fatalf("unexpected comment result: %+v", result)
	}
	if strings.Join(calls, ",") != "GET /repos/acme/collectors/issues/42/comments,PATCH /repos/acme/collectors/issues/comments/123" {
		t.Fatalf("unexpected provider calls: %v", calls)
	}
	if !strings.Contains(updatedBody, "new validation body") || !strings.Contains(updatedBody, "otel-magnify: validation-comment") {
		t.Fatalf("updated body missing validation content/marker: %s", updatedBody)
	}
}

func seedExportConfig(t *testing.T, db interface{ CreateConfig(models.Config) error }, id, content string) models.Config {
	t.Helper()
	cfg := models.Config{ID: id, Name: id, Content: content, CreatedAt: time.Now().UTC(), CreatedBy: "admin@test.com", SourceType: models.ConfigSourceGit, GitURL: "https://github.com/acme/collectors.git", GitProvider: "github", GitRef: "main", GitPath: "otel/collector.yaml", CommitSHA: "0123456789abcdef0123456789abcdef01234567"}
	if err := db.CreateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func seedGitOpsValidationPass(t *testing.T, db interface {
	RecordGitOpsValidationStatus(models.GitOpsValidationStatus) error
}, cfg models.Config) {
	t.Helper()
	if err := db.RecordGitOpsValidationStatus(models.GitOpsValidationStatus{Provider: cfg.GitProvider, Event: "pull_request", Action: "synchronize", Status: "pass", SourcePath: cfg.GitPath, SourceRef: cfg.GitRef, CommitSHA: cfg.CommitSHA, ObservedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func validGitOpsYAML() string {
	return "receivers:\n  otlp: {}\nexporters:\n  logging: {}\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [logging]\n"
}

func hmacSHA256Header(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func assertJSONErrorContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("body %q does not contain %q", body, want)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
