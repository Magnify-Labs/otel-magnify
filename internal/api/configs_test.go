package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestCreateAndListConfigs(t *testing.T) {
	_, router, _ := newTestAPI(t)

	body := `{"name":"collector-base","content":"receivers:\n  otlp:"}`
	a := auth.New("test-secret-key-at-least-32-bytes!")
	token, _ := a.GenerateToken("user-001", "admin@test.com", []string{"administrator"})

	req := httptest.NewRequest("POST", "/api/configs", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = authedRequest(t, "GET", "/api/configs")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("list status = %d", rec.Code)
	}

	var configs []models.Config
	json.NewDecoder(rec.Body).Decode(&configs)
	if len(configs) < 1 {
		t.Errorf("len = %d, want at least 1", len(configs))
	}
	var saved *models.Config
	for i := range configs {
		if configs[i].Name == "collector-base" {
			saved = &configs[i]
		}
	}
	if saved == nil {
		t.Fatalf("created config not listed: %+v", configs)
	}
	if saved.Kind != models.ConfigKindSaved || saved.Status != models.ConfigStatusReady || saved.BuiltIn {
		t.Fatalf("created config metadata = kind %q status %q built_in %v, want saved/ready/not built-in", saved.Kind, saved.Status, saved.BuiltIn)
	}
}

func TestCreateConfig_PersistsMigrationAssistantDraftMetadata(t *testing.T) {
	_, router, _ := newTestAPI(t)

	body := `{
		"name":"Migrated Datadog Agent draft",
		"content":"receivers:\n  filelog/datadog_app_0: {}",
		"kind":"draft",
		"status":"draft",
		"category":"migration",
		"stack":"datadog",
		"tags":["migration","datadog_agent","migration"],
		"source_type":"migration_assistant"
	}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cfg models.Config
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != models.ConfigKindDraft || cfg.Status != models.ConfigStatusDraft || cfg.SourceType != models.ConfigSourceMigrationAssistant {
		t.Fatalf("migration draft metadata = kind %q status %q source %q", cfg.Kind, cfg.Status, cfg.SourceType)
	}
	if cfg.Category != "migration" || cfg.Stack != "datadog" {
		t.Fatalf("migration draft display metadata = category %q stack %q", cfg.Category, cfg.Stack)
	}
	if got, want := strings.Join(cfg.Tags, ","), "migration,datadog_agent"; got != want {
		t.Fatalf("tags = %q, want %q", got, want)
	}
}

func TestListConfigs_IncludesRequiredBuiltInTemplatesWithSafePlaceholders(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, http.MethodGet, "/api/configs")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var configs []models.Config
	if err := json.NewDecoder(rec.Body).Decode(&configs); err != nil {
		t.Fatal(err)
	}

	required := map[string]bool{
		"tpl-k8s-otlp-grafana":          false,
		"tpl-k8s-otlp-datadog":          false,
		"tpl-logs-loki":                 false,
		"tpl-traces-tempo":              false,
		"tpl-metrics-prometheus-remote": false,
		"tpl-jvm-services":              false,
		"tpl-nginx":                     false,
		"tpl-postgresql":                false,
		"tpl-redis":                     false,
	}
	for _, cfg := range configs {
		if _, ok := required[cfg.ID]; !ok {
			continue
		}
		required[cfg.ID] = true
		if cfg.Kind != models.ConfigKindTemplate || !cfg.BuiltIn || cfg.Status != models.ConfigStatusReady {
			t.Fatalf("template %s metadata = kind %q built_in %v status %q", cfg.ID, cfg.Kind, cfg.BuiltIn, cfg.Status)
		}
		if cfg.Category == "" || cfg.Stack == "" || cfg.Description == "" || len(cfg.Tags) == 0 {
			t.Fatalf("template %s missing display metadata: %+v", cfg.ID, cfg)
		}
		assertTemplateHasVariables(t, cfg, "endpoint", "headers", "environment", "resource_attributes", "tls")
		assertTemplateHasNoPlaintextSecrets(t, cfg)
	}
	for id, seen := range required {
		if !seen {
			t.Fatalf("required built-in template %s not returned", id)
		}
	}
}

func TestGetConfig_ResolvesBuiltInTemplateID(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, http.MethodGet, "/api/configs/tpl-k8s-otlp-datadog")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cfg models.Config
	if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ID != "tpl-k8s-otlp-datadog" || cfg.Kind != models.ConfigKindTemplate || !cfg.BuiltIn {
		t.Fatalf("unexpected template response: %+v", cfg)
	}
	assertTemplateHasVariables(t, cfg, "endpoint", "headers", "environment", "resource_attributes", "tls")
	assertTemplateHasNoPlaintextSecrets(t, cfg)
	if !strings.Contains(cfg.Content, "${DATADOG_API_KEY}") {
		t.Fatalf("datadog template should use DATADOG_API_KEY placeholder, content: %s", cfg.Content)
	}
}

func TestListConfigs_CanFilterByKind(t *testing.T) {
	_, router, _ := newTestAPI(t)

	req := authedRequest(t, http.MethodGet, "/api/configs?kind=template")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var configs []models.Config
	if err := json.NewDecoder(rec.Body).Decode(&configs); err != nil {
		t.Fatal(err)
	}
	if len(configs) == 0 {
		t.Fatal("expected templates")
	}
	for _, cfg := range configs {
		if cfg.Kind != models.ConfigKindTemplate {
			t.Fatalf("filtered list returned non-template: %+v", cfg)
		}
	}
}

func assertTemplateHasVariables(t *testing.T, cfg models.Config, names ...string) {
	t.Helper()
	got := map[string]bool{}
	for _, variable := range cfg.Variables {
		got[variable.Name] = true
	}
	for _, name := range names {
		if !got[name] {
			t.Fatalf("template %s missing variable %q in %+v", cfg.ID, name, cfg.Variables)
		}
	}
}

func assertTemplateHasNoPlaintextSecrets(t *testing.T, cfg models.Config) {
	t.Helper()
	for _, forbidden := range []string{"secret-token", "api-key-", "Bearer eyJ", "password123", "supersecret"} {
		if strings.Contains(strings.ToLower(cfg.Content), strings.ToLower(forbidden)) {
			t.Fatalf("template %s contains real-looking secret literal %q", cfg.ID, forbidden)
		}
	}
	if strings.Contains(strings.ToLower(cfg.Content), "authorization: bearer ") && !strings.Contains(cfg.Content, "${") {
		t.Fatalf("template %s contains bearer authorization without placeholder", cfg.ID)
	}
}

func TestDiffConfigs_ReturnsRedactedOTelDiff(t *testing.T) {
	_, router, _ := newTestAPI(t)

	body := `{"base_yaml":"receivers:\n  otlp: {}\nexporters:\n  otlp:\n    endpoint: https://old.example:4317\n    headers:\n      Authorization: Bearer secret-token\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [otlp]\n","target_yaml":"receivers:\n  otlp: {}\nexporters:\n  otlp:\n    endpoint: https://new.example:4317\n    headers:\n      Authorization: Bearer changed-token\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [otlp]\n"}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/diff", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("diff status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("secret-token")) || bytes.Contains(rec.Body.Bytes(), []byte("changed-token")) {
		t.Fatalf("diff response leaked raw secret: %s", rec.Body.String())
	}
	var resp struct {
		SchemaVersion string `json:"schema_version"`
		Valid         bool   `json:"valid"`
		Summary       struct {
			OverallRisk string `json:"overall_risk"`
		} `json:"summary"`
		Endpoints []struct {
			Risk string `json:"risk"`
		} `json:"endpoints"`
		Security []struct {
			Message string `json:"message"`
		} `json:"security"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.SchemaVersion != "otel-config-diff.v1" || !resp.Valid || resp.Summary.OverallRisk == "none" || len(resp.Endpoints) == 0 || len(resp.Security) == 0 {
		t.Fatalf("unexpected diff response: %+v", resp)
	}
}

func TestDiffConfigs_EnrichesBlastRadiusFromWorkloadContext(t *testing.T) {
	db, router, _ := newTestAPI(t)
	now := time.Now().UTC()
	if err := db.UpsertWorkload(models.Workload{
		ID:          "wl-checkout",
		DisplayName: "checkout collector",
		Type:        "collector",
		Status:      "degraded",
		LastSeenAt:  now,
		Labels: models.Labels{
			"service.name":     "checkout-api",
			"k8s.cluster.name": "prod-eu-1",
			"critical":         "true",
			"authorization":    "Bearer should-not-leak",
		},
		FingerprintKeys:     models.FingerprintKeys{"cluster": "prod-eu-1"},
		AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertWorkload(models.Workload{
		ID:                  "wl-billing",
		DisplayName:         "billing sdk",
		Type:                "sdk",
		Status:              "connected",
		LastSeenAt:          now,
		Labels:              models.Labels{"service": "billing-api", "cluster": "prod-eu-1"},
		FingerprintKeys:     models.FingerprintKeys{},
		AcceptsRemoteConfig: false,
	}); err != nil {
		t.Fatal(err)
	}

	body := `{"base_yaml":"receivers:\n  otlp: {}\nexporters:\n  otlp:\n    endpoint: https://old.example:4317?token=old-secret\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: [otlp]\n","target_yaml":"receivers:\n  otlp: {}\nexporters:\n  otlp/archive:\n    endpoint: https://archive.example:4317?token=new-secret\nservice:\n  pipelines:\n    metrics:\n      receivers: [otlp]\n      exporters: [otlp/archive]\n","context":{"workload_id":"wl-checkout"}}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/diff", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("diff status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("old-secret")) || bytes.Contains(rec.Body.Bytes(), []byte("new-secret")) || bytes.Contains(rec.Body.Bytes(), []byte("should-not-leak")) {
		t.Fatalf("diff response leaked secret: %s", rec.Body.String())
	}
	var resp struct {
		BlastRadius struct {
			SchemaVersion    string   `json:"schema_version"`
			AffectedSignals  []string `json:"affected_signals"`
			TouchedExporters []string `json:"touched_exporters"`
			ImpactedClusters []string `json:"impacted_clusters"`
			ImpactedServices []struct {
				ServiceName string `json:"service_name"`
			} `json:"impacted_services"`
			CriticalCollectors []struct {
				WorkloadID string `json:"workload_id"`
			} `json:"critical_collectors"`
		} `json:"blast_radius"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.BlastRadius.SchemaVersion != "otel-config-blast-radius.v1" {
		t.Fatalf("blast radius schema = %q", resp.BlastRadius.SchemaVersion)
	}
	assertStringSet(t, resp.BlastRadius.AffectedSignals, []string{"metrics", "traces"})
	assertStringSet(t, resp.BlastRadius.TouchedExporters, []string{"otlp", "otlp/archive"})
	assertStringSet(t, resp.BlastRadius.ImpactedClusters, []string{"prod-eu-1"})
	if len(resp.BlastRadius.ImpactedServices) != 2 {
		t.Fatalf("impacted services = %#v", resp.BlastRadius.ImpactedServices)
	}
	if len(resp.BlastRadius.CriticalCollectors) != 1 || resp.BlastRadius.CriticalCollectors[0].WorkloadID != "wl-checkout" {
		t.Fatalf("critical collectors = %#v", resp.BlastRadius.CriticalCollectors)
	}
}

func assertStringSet(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("strings = %#v, want %#v", got, want)
	}
	seen := map[string]bool{}
	for _, v := range got {
		seen[v] = true
	}
	for _, v := range want {
		if !seen[v] {
			t.Fatalf("strings = %#v, missing %q", got, v)
		}
	}
}

func TestDiffConfigs_RejectsInvalidRequest(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/diff", `{"base_yaml":"receivers: {}"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestImportConfigFromGit_PersistsProvenanceAndValidation(t *testing.T) {
	db, router, _ := newTestAPI(t)
	importedAt := time.Now().UTC().Truncate(time.Second)
	old := gitImportConfig
	oldAllowPrivate := allowPrivateGitURLs
	allowPrivateGitURLs = true
	gitImportConfig = func(_ context.Context, req gitImportRequest) (gitImportResult, error) {
		if req.GitURL != "https://token:secret@github.com/acme/collectors.git" {
			t.Fatalf("GitURL = %q", req.GitURL)
		}
		return gitImportResult{
			Content:     "receivers:\n  otlp:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      exporters: []\n",
			CommitSHA:   "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
			GitURL:      "https://github.com/acme/collectors.git",
			GitProvider: "github",
			ImportedAt:  importedAt,
		}, nil
	}
	t.Cleanup(func() {
		gitImportConfig = old
		allowPrivateGitURLs = oldAllowPrivate
	})

	body := `{"name":"collector-from-git","git_url":"https://token:secret@github.com/acme/collectors.git","git_ref":"main","git_path":"otel/collector.yaml"}`
	req := authedRequest(t, "POST", "/api/configs/import/git")
	req.Body = httptest.NewRequest("POST", "/", bytes.NewBufferString(body)).Body
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 201 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Config     models.Config `json:"config"`
		Validation struct {
			Valid bool `json:"valid"`
		} `json:"validation"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Validation.Valid {
		t.Fatalf("validation = false, response = %s", rec.Body.String())
	}
	if resp.Config.SourceType != models.ConfigSourceGit || resp.Config.GitProvider != "github" {
		t.Fatalf("provenance missing from response: %+v", resp.Config)
	}
	if resp.Config.GitURL != "https://github.com/acme/collectors.git" {
		t.Fatalf("GitURL persisted credentials: %q", resp.Config.GitURL)
	}
	if resp.Config.CommitSHA != "abcdefabcdefabcdefabcdefabcdefabcdefabcd" {
		t.Fatalf("CommitSHA = %q", resp.Config.CommitSHA)
	}
	stored, err := db.GetConfig(resp.Config.ID)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if stored.GitURL != "https://github.com/acme/collectors.git" || stored.GitRef != "main" || stored.GitPath != "otel/collector.yaml" {
		t.Fatalf("stored provenance mismatch: %+v", stored)
	}
}

func TestImportConfigFromGit_RejectsUnsafeURLBeforeFetch(t *testing.T) {
	_, router, _ := newTestAPI(t)
	old := gitImportConfig
	called := false
	gitImportConfig = func(context.Context, gitImportRequest) (gitImportResult, error) {
		called = true
		return gitImportResult{}, errors.New("fetch should not be called")
	}
	t.Cleanup(func() { gitImportConfig = old })

	for _, gitURL := range []string{"file:///tmp/repo", "http://127.0.0.1/repo.git", "http://239.1.2.3/repo.git"} {
		called = false
		body := `{"name":"unsafe","git_url":"` + gitURL + `","git_ref":"main","git_path":"otel.yaml"}`
		req := authedRequest(t, "POST", "/api/configs/import/git")
		req.Body = httptest.NewRequest("POST", "/", bytes.NewBufferString(body)).Body
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != 400 {
			t.Fatalf("%s status = %d, body = %s", gitURL, rec.Code, rec.Body.String())
		}
		if called {
			t.Fatalf("fetch called for unsafe URL %s", gitURL)
		}
	}
}

func TestImportConfigFromGit_RedactsFetchFailure(t *testing.T) {
	_, router, _ := newTestAPI(t)
	old := gitImportConfig
	oldAllowPrivate := allowPrivateGitURLs
	allowPrivateGitURLs = true
	gitImportConfig = func(context.Context, gitImportRequest) (gitImportResult, error) {
		return gitImportResult{}, errors.New("git fetch failed for http://token:secret@git.example.com/repo.git ssh://ssh-token:ssh-secret@git.example.com/repo.git git://git-token:git-secret@git.example.com/repo.git with access_token=query-secret")
	}
	t.Cleanup(func() {
		gitImportConfig = old
		allowPrivateGitURLs = oldAllowPrivate
	})

	body := `{"name":"unsafe","git_url":"http://token:secret@git.example.com/repo.git","git_ref":"main","git_path":"otel.yaml"}`
	req := authedRequest(t, "POST", "/api/configs/import/git")
	req.Body = httptest.NewRequest("POST", "/", bytes.NewBufferString(body)).Body
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, forbidden := range []string{"token:secret", "ssh-token:ssh-secret", "git-token:git-secret", "access_token", "query-secret"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("import failure leaked %q: %s", forbidden, rec.Body.String())
		}
	}
}

func TestPreviewConfigPolicy_ReturnsStableFindingsContract(t *testing.T) {
	_, router, _ := newTestAPI(t)
	body := `{
		"target_yaml":"receivers:\n  otlp: {}\nprocessors:\n  batch: {}\nexporters:\n  otlp:\n    endpoint: https://evil.example:4317\n    tls:\n      insecure: true\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [otlp]\n",
		"context":{"environment":"production","endpoint_allowlist":["allowed.example"],"required_resource_attributes":["service.name"],"max_sampling_percentage":50}
	}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/policy/preview", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("policy status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") || strings.Contains(rec.Body.String(), "Bearer ") {
		t.Fatalf("policy response leaked raw secret-like content: %s", rec.Body.String())
	}
	var resp models.ConfigPolicyEvaluation
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.SchemaVersion != "config-policy.v1" || resp.Valid || resp.Allowed || resp.Decision != models.PolicyDecisionBlock || resp.Severity != models.PolicySeverityCritical {
		t.Fatalf("unexpected policy response: %+v", resp)
	}
	assertPolicyAPIFinding(t, resp, "community.production.insecure_tls")
	assertPolicyAPIFinding(t, resp, "community.exporters.otlp_endpoint.allowlist")
	assertPolicyAPIFinding(t, resp, "community.resource.attributes.required")
}

func TestPreviewConfigPolicy_RejectsInvalidRequest(t *testing.T) {
	_, router, _ := newTestAPI(t)
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/policy/preview", `{"base_yaml":"receivers: {}"}`, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func assertPolicyAPIFinding(t *testing.T, resp models.ConfigPolicyEvaluation, ruleID string) {
	t.Helper()
	for _, finding := range resp.Findings {
		if finding.RuleID == ruleID && len(finding.Paths) > 0 && finding.Packaging == "community" && finding.Tier == "core" {
			return
		}
	}
	t.Fatalf("finding %s not found in %+v", ruleID, resp.Findings)
}

func TestListConfigs_RedactsContentForViewer(t *testing.T) {
	db, router, _ := newTestAPI(t)
	const content = `receivers:
  otlp: {}
exporters:
  otlphttp:
    headers:
      api-key: ***
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [otlphttp]`
	if err := db.CreateConfig(models.Config{
		ID:        "hash-a",
		Name:      "collector-base",
		Content:   content,
		CreatedAt: time.Now().UTC(),
		CreatedBy: "admin@test.com",
	}); err != nil {
		t.Fatal(err)
	}

	req := authedJSONRequest(t, http.MethodGet, "/api/configs", "", []string{"viewer"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	assertResponseDoesNotContainConfigYAML(t, rec.Body.String())
	var configs []models.Config
	if err := json.NewDecoder(rec.Body).Decode(&configs); err != nil {
		t.Fatal(err)
	}
	var saved *models.Config
	for i := range configs {
		if configs[i].ID == "hash-a" {
			saved = &configs[i]
		}
		if configs[i].Content != "" {
			t.Fatalf("config %q Content = %q, want redacted empty content", configs[i].ID, configs[i].Content)
		}
	}
	if saved == nil {
		t.Fatalf("created config not listed: %+v", configs)
	}
	if saved.Name != "collector-base" {
		t.Fatalf("metadata was not preserved: %+v", saved)
	}
}

func TestListConfigs_OperatorsAndAdminsReceiveMetadataOnly(t *testing.T) {
	for _, role := range []string{"editor", "administrator"} {
		t.Run(role, func(t *testing.T) {
			db, router, _ := newTestAPI(t)
			const content = "receivers:\n  otlp: {}\nexporters:\n  otlphttp:\n    endpoint: https://example.invalid:4318"
			if err := db.CreateConfig(models.Config{
				ID:        "hash-a",
				Name:      "collector-base",
				Content:   content,
				CreatedAt: time.Now().UTC(),
				CreatedBy: "admin@test.com",
			}); err != nil {
				t.Fatal(err)
			}

			req := authedJSONRequest(t, http.MethodGet, "/api/configs", "", []string{role})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var configs []models.Config
			if err := json.NewDecoder(rec.Body).Decode(&configs); err != nil {
				t.Fatal(err)
			}
			for _, cfg := range configs {
				if cfg.ID == "hash-a" {
					if cfg.Content != "" {
						t.Fatalf("Content = %q, want metadata-only list payload", cfg.Content)
					}

					detailReq := authedJSONRequest(t, http.MethodGet, "/api/configs/hash-a", "", []string{role})
					detailRec := httptest.NewRecorder()
					router.ServeHTTP(detailRec, detailReq)
					var detail models.Config
					if err := json.NewDecoder(detailRec.Body).Decode(&detail); err != nil {
						t.Fatal(err)
					}
					if detailRec.Code != http.StatusOK || detail.Content != content {
						t.Fatalf("detail status=%d content=%q, want explicit detail content", detailRec.Code, detail.Content)
					}
					return
				}
			}
			t.Fatalf("created config not listed: %+v", configs)
		})
	}
}

func TestGetConfig_403ForViewer(t *testing.T) {
	db, router, _ := newTestAPI(t)
	if err := db.CreateConfig(models.Config{
		ID:        "hash-a",
		Name:      "collector-base",
		Content:   "receivers:\n  otlp: {}\nexporters:\n  otlphttp: {}",
		CreatedAt: time.Now().UTC(),
		CreatedBy: "admin@test.com",
	}); err != nil {
		t.Fatal(err)
	}

	req := authedJSONRequest(t, http.MethodGet, "/api/configs/hash-a", "", []string{"viewer"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	assertResponseDoesNotContainConfigYAML(t, rec.Body.String())
}

func TestGetConfig_OperatorsAndAdminsRetainContentAccess(t *testing.T) {
	for _, role := range []string{"editor", "administrator"} {
		t.Run(role, func(t *testing.T) {
			db, router, _ := newTestAPI(t)
			const content = "receivers:\n  otlp: {}\nexporters:\n  otlphttp: {}"
			if err := db.CreateConfig(models.Config{
				ID:        "hash-a",
				Name:      "collector-base",
				Content:   content,
				CreatedAt: time.Now().UTC(),
				CreatedBy: "admin@test.com",
			}); err != nil {
				t.Fatal(err)
			}

			req := authedJSONRequest(t, http.MethodGet, "/api/configs/hash-a", "", []string{role})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			var cfg models.Config
			if err := json.NewDecoder(rec.Body).Decode(&cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Content != content {
				t.Fatalf("Content = %q, want unredacted config content", cfg.Content)
			}
		})
	}
}

func assertResponseDoesNotContainConfigYAML(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"receivers:", "exporters:", "service:", "otlphttp", "TEST_API_KEY"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked config marker %q: %s", forbidden, body)
		}
	}
}

func TestListConfigsOmitsYAMLContent(t *testing.T) {
	db, router, _ := newTestAPI(t)
	content := "receivers:\n  otlp:\n    protocols:\n      grpc: {}"
	if err := db.CreateConfig(models.Config{
		ID: "cfg-large", Name: "collector-large", Content: content,
		CreatedAt: time.Now().UTC(), CreatedBy: "admin@test.com",
	}); err != nil {
		t.Fatal(err)
	}

	req := authedRequest(t, "GET", "/api/configs")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(content)) || bytes.Contains(rec.Body.Bytes(), []byte(`"content"`)) {
		t.Fatalf("list payload leaked config content: %s", rec.Body.String())
	}

	var configs []struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
		CreatedBy string    `json:"created_by"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &configs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cfg := range configs {
		if cfg.ID == "cfg-large" && cfg.Name == "collector-large" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("metadata shape = %+v", configs)
	}

	detailReq := authedRequest(t, "GET", "/api/configs/cfg-large")
	detailRec := httptest.NewRecorder()
	router.ServeHTTP(detailRec, detailReq)
	var detail models.Config
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detailRec.Code != 200 || detail.Content != content {
		t.Fatalf("detail payload should include config content, status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
}

func TestLoginHandler(t *testing.T) {
	db, router, _ := newTestAPI(t)

	hash, _ := hashPassword("testpass123")
	db.CreateUser(models.User{
		ID: "user-001", Email: "admin@test.com", PasswordHash: hash,
	})

	body := `{"email":"admin@test.com","password":"testpass123"}`
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["token"] == "" {
		t.Error("expected non-empty token")
	}
}

func TestListAlerts_Handler(t *testing.T) {
	db, router, _ := newTestAPI(t)
	db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt: time.Now().UTC(), Labels: models.Labels{},
	})
	db.CreateAlert(models.Alert{
		ID: "alert-1", WorkloadID: "w1", Rule: "workload_down",
		Severity: "critical", Message: "down", FiredAt: time.Now().UTC(),
	})

	req := authedRequest(t, "GET", "/api/alerts")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}

	var alerts []models.Alert
	json.NewDecoder(rec.Body).Decode(&alerts)
	if len(alerts) != 1 {
		t.Errorf("len = %d, want 1", len(alerts))
	}
}
