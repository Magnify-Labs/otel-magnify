package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func newFeatureGateTestAPI(t *testing.T, features map[string]bool, checker ext.LicenseChecker) (ext.Store, http.Handler, *fakeOpAMPPusher) {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	a := auth.New("test-secret-key-at-least-32-bytes!")
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)

	fake := &fakeOpAMPPusher{instances: make(map[string][]opamp.Instance)}
	router := NewRouter(db, a, hub, fake, nil, "", nil, nil, 30*24*time.Hour, features, checker, nil)
	return db, router, fake
}

type testLicenseChecker map[string]bool

func (t testLicenseChecker) FeatureEnabled(feature string) bool { return t[feature] }

func TestFeatureGate_DeniesPaidEndpointGroupsWithStableContract(t *testing.T) {
	db, router, fake := newFeatureGateTestAPI(t, nil, nil)
	seedHistory(t, db, "w1", "hash-a", validRollbackYAML)

	cases := []struct {
		name    string
		method  string
		path    string
		body    string
		feature string
	}{
		{"drift dashboard", http.MethodGet, "/api/config-safety/drift", "", FeatureConfigSafetyDriftDashboard},
		{"evidence pack", http.MethodPost, "/api/reports/evidence-pack", `{"report_type":"evidence_pack","scope":{"workload_ids":["w1"]}}`, FeatureReportsEvidencePack},
		{"version intelligence", http.MethodGet, "/api/workloads/version-intelligence?recommended_version=0.100.0", "", FeatureConfigSafetyVersionIntelligence},
		{"approvals", http.MethodGet, "/api/workloads/w1/config/approvals", "", FeatureConfigSafetyApprovals},
		{"policy preview", http.MethodPost, "/api/workloads/w1/config/plan", validRollbackYAML, FeatureConfigSafetyPolicyPreview},
		{"canary rollout", http.MethodPost, "/api/workloads/w1/config/canary", `{"config":"receivers:\n  otlp: {}\n","selection":{"strategy":"one"}}`, FeatureConfigSafetyCanaryRollout},
		{"guided rollback", http.MethodPost, "/api/workloads/w1/configs/hash-a/rollback", "", FeatureConfigSafetyGuidedRollback},
		{"gitops export", http.MethodPost, "/api/configs/hash-a/export/git", `{}`, FeatureConfigSafetyGitOpsExport},
		{"audit viewer", http.MethodGet, "/api/audit/events", "", FeatureAuditViewer},
		{"scoped push", http.MethodPost, "/api/pushes/preview", `{}`, FeatureConfigSafetyScopedPush},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authedJSONRequest(t, tc.method, tc.path, tc.body, []string{"administrator"})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assertFeatureDisabled(t, rec, tc.feature)
		})
	}

	if len(fake.pushed) != 0 {
		t.Fatalf("feature-denied requests performed OpAMP pushes: %+v", fake.pushed)
	}
	history, err := db.GetWorkloadConfigHistory("w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history len = %d, want original row only", len(history))
	}
}

func TestFeatureGate_DeniesPublicGitOpsWebhookWithoutFeature(t *testing.T) {
	_, router, _ := newFeatureGateTestAPI(t, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/gitops/webhooks/github", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertFeatureDisabled(t, rec, FeatureConfigSafetyGitOpsExport)
}

func TestFeatureGate_AllowsStaticOrLicenseEnabledFeature(t *testing.T) {
	for _, tc := range []struct {
		name     string
		features map[string]bool
		checker  ext.LicenseChecker
	}{
		{"static feature", map[string]bool{FeatureConfigSafetyVersionIntelligence: true}, nil},
		{"license checker", nil, testLicenseChecker{FeatureConfigSafetyVersionIntelligence: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, router, _ := newFeatureGateTestAPI(t, tc.features, tc.checker)
			req := authedRequest(t, http.MethodGet, "/api/workloads/version-intelligence?recommended_version=0.100.0")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestFeatureGate_PreservesCommunityPrimitives(t *testing.T) {
	db, router, _ := newFeatureGateTestAPI(t, nil, nil)
	if err := db.UpsertWorkload(models.Workload{ID: "w1", Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"validation", http.MethodPost, "/api/workloads/w1/config/validate", validRollbackYAML},
		{"config diff", http.MethodPost, "/api/configs/diff", `{"base_yaml":"receivers:\n  otlp: {}\n","target_yaml":"receivers:\n  otlp: {}\nprocessors:\n  batch: {}\n"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := authedJSONRequest(t, tc.method, tc.path, tc.body, []string{"administrator"})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func assertFeatureDisabled(t *testing.T, rec *httptest.ResponseRecorder, feature string) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Feature string `json:"feature"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "feature disabled" || body.Code != "feature_disabled" || body.Feature != feature {
		t.Fatalf("body = %+v, want feature_disabled for %q", body, feature)
	}
}
