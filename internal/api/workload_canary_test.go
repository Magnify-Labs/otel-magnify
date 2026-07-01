package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const validCanaryConfig = `receivers:
  otlp:
    protocols:
      grpc:
exporters:
  debug:
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [debug]
`

func seedCanaryWorkload(t *testing.T, db interface{ UpsertWorkload(models.Workload) error }, workloadID string, labels models.Labels) {
	t.Helper()
	if labels == nil {
		labels = models.Labels{}
	}
	if err := db.UpsertWorkload(models.Workload{
		ID: workloadID, DisplayName: workloadID, Type: "collector", Version: "0.128.0",
		Status: "connected", LastSeenAt: time.Now().UTC(), Labels: labels,
		AcceptsRemoteConfig: true,
		AvailableComponents: &models.AvailableComponents{Components: map[string][]string{
			"receivers": {"otlp"}, "exporters": {"debug"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func seedCanaryInstances(fake *fakeOpAMPPusher, workloadID string) {
	now := time.Now().UTC()
	fake.instances[workloadID] = []opamp.Instance{
		{InstanceUID: "inst-a", PodName: "pod-a", Healthy: true, LastMessageAt: now, EffectiveConfigHash: "old"},
		{InstanceUID: "inst-b", PodName: "pod-b", Healthy: true, LastMessageAt: now, EffectiveConfigHash: "old"},
		{InstanceUID: "inst-c", PodName: "pod-c", Healthy: true, LastMessageAt: now, EffectiveConfigHash: "old"},
	}
}

func postCanary(t *testing.T, router http.Handler, workloadID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := authedRequestForGroups(t, "POST", "/api/workloads/"+workloadID+"/config/canary", body, []string{"administrator"})
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCanaryValidateSupportsOneNPercentageAndLabelSelector(t *testing.T) {
	db, router, fake := newTestAPI(t)
	seedCanaryWorkload(t, db, "wl-canary", models.Labels{"env": "prod"})
	seedCanaryInstances(fake, "wl-canary")

	cases := []struct {
		name, selector string
		want           int
	}{
		{"one", `{"config":"` + jsonEsc(validCanaryConfig) + `","selection":{"strategy":"one","instance_uid":"inst-b"}}`, 1},
		{"count", `{"config":"` + jsonEsc(validCanaryConfig) + `","selection":{"strategy":"count","count":2}}`, 2},
		{"percentage", `{"config":"` + jsonEsc(validCanaryConfig) + `","selection":{"strategy":"percentage","percentage":50}}`, 2},
		{"label", `{"config":"` + jsonEsc(validCanaryConfig) + `","selection":{"strategy":"label_selector","labels":{"env":"prod"}}}`, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := authedRequestForGroups(t, "POST", "/api/workloads/wl-canary/config/canary/validate", tc.selector, []string{"administrator"})
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var got models.CanaryValidationResult
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Valid || len(got.Targets) != tc.want {
				t.Fatalf("valid=%v targets=%d want %d body=%s", got.Valid, len(got.Targets), tc.want, rec.Body.String())
			}
		})
	}
}

func TestCanaryStartPushesOnlySelectedInstanceAndStatusIsSanitized(t *testing.T) {
	db, router, fake, auditLog := newAuditTestAPI(t)
	seedCanaryWorkload(t, db, "wl-canary", models.Labels{})
	seedCanaryInstances(fake, "wl-canary")

	rec := postCanary(t, router, "wl-canary", `{"config":"`+jsonEsc(validCanaryConfig)+`","selection":{"strategy":"one","instance_uid":"inst-b"}}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(fake.pushed) != 1 || fake.pushed[0].Target != "inst-b" {
		t.Fatalf("pushes=%+v", fake.pushed)
	}
	if strings.Contains(rec.Body.String(), "receivers:") {
		t.Fatalf("response leaked raw config: %s", rec.Body.String())
	}
	var got models.CanaryStatus
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.ConfigHash == "" || got.Counts.Pending != 1 || len(got.Targets) != 1 {
		t.Fatalf("bad status: %+v", got)
	}
	if events := auditLog.snapshot(); len(events) != 1 || events[0].Action != "config.canary.start" || strings.Contains(events[0].Detail, "receivers:") {
		t.Fatalf("audit=%+v", events)
	}
}

func TestCanaryStartReturnsSanitizedOpAMPPushFailure(t *testing.T) {
	db, router, fake := newTestAPI(t)
	seedCanaryWorkload(t, db, "wl-canary", models.Labels{})
	seedCanaryInstances(fake, "wl-canary")
	fake.err = errors.New(sensitiveOpAMPErrorFixture)

	rec := postCanary(t, router, "wl-canary", `{"config":"`+jsonEsc(validCanaryConfig)+`","selection":{"strategy":"one","instance_uid":"inst-a"}}`)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SECRET_TOKEN") {
		t.Fatalf("response leaked secret: %s", rec.Body.String())
	}
}

func TestCanaryRejectsEmptyUnsupportedStaleDegradedInvalidAndViewer(t *testing.T) {
	db, router, fake := newTestAPI(t)
	seedCanaryWorkload(t, db, "wl-canary", models.Labels{})
	seedCanaryInstances(fake, "wl-canary")

	fake.instances["wl-canary"][1].Healthy = false
	fake.instances["wl-canary"][2].LastMessageAt = time.Now().UTC().Add(-10 * time.Minute)
	cases := []struct {
		name, body string
		want       int
	}{
		{"empty", `{"config":"` + jsonEsc(validCanaryConfig) + `","selection":{"strategy":"label_selector","labels":{"missing":"true"}}}`, http.StatusBadRequest},
		{"degraded", `{"config":"` + jsonEsc(validCanaryConfig) + `","selection":{"strategy":"one","instance_uid":"inst-b"}}`, http.StatusConflict},
		{"stale", `{"config":"` + jsonEsc(validCanaryConfig) + `","selection":{"strategy":"one","instance_uid":"inst-c"}}`, http.StatusConflict},
		{"invalid", `{"config":"receivers:\n  missing:\nservice:\n  pipelines:\n    traces:\n      receivers: [missing]\n      exporters: [debug]\n","selection":{"strategy":"one","instance_uid":"inst-a"}}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postCanary(t, router, "wl-canary", tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	req := authedRequestForGroups(t, "POST", "/api/workloads/wl-canary/config/canary", `{"config":"`+jsonEsc(validCanaryConfig)+`","selection":{"strategy":"one","instance_uid":"inst-a"}}`, []string{"viewer"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCanaryPromoteAbortRollbackAndAuditFailure(t *testing.T) {
	db, router, fake, auditLog := newAuditTestAPI(t)
	seedCanaryWorkload(t, db, "wl-canary", models.Labels{})
	seedCanaryInstances(fake, "wl-canary")
	start := postCanary(t, router, "wl-canary", `{"config":"`+jsonEsc(validCanaryConfig)+`","selection":{"strategy":"one","instance_uid":"inst-a"}}`)
	if start.Code != http.StatusAccepted {
		t.Fatalf("start=%d %s", start.Code, start.Body.String())
	}
	var status models.CanaryStatus
	_ = json.NewDecoder(start.Body).Decode(&status)

	// Simulate canary success before promote.
	status.Targets[0].Status = models.InstanceStatusApplied
	if err := db.UpdateCanaryStatus(status); err != nil {
		t.Fatal(err)
	}

	promoteReq := authedRequestForGroups(t, "POST", "/api/workloads/wl-canary/config/canary/"+status.ID+"/promote", "", []string{"administrator"})
	promoteRec := httptest.NewRecorder()
	router.ServeHTTP(promoteRec, promoteReq)
	if promoteRec.Code != http.StatusAccepted {
		t.Fatalf("promote=%d %s", promoteRec.Code, promoteRec.Body.String())
	}
	if len(fake.pushed) != 3 || fake.pushed[1].Target != "inst-b" || fake.pushed[2].Target != "inst-c" {
		t.Fatalf("pushes=%+v", fake.pushed)
	}

	abortReq := authedRequestForGroups(t, "POST", "/api/workloads/wl-canary/config/canary/"+status.ID+"/abort", "", []string{"administrator"})
	abortRec := httptest.NewRecorder()
	router.ServeHTTP(abortRec, abortReq)
	if abortRec.Code != http.StatusOK {
		t.Fatalf("abort=%d %s", abortRec.Code, abortRec.Body.String())
	}
	if len(fake.pushed) != 3 {
		t.Fatalf("abort pushed unexpectedly: %+v", fake.pushed)
	}

	rollbackReq := authedRequestForGroups(t, "POST", "/api/workloads/wl-canary/config/canary/"+status.ID+"/rollback", "", []string{"administrator"})
	rollbackRec := httptest.NewRecorder()
	router.ServeHTTP(rollbackRec, rollbackReq)
	if rollbackRec.Code != http.StatusConflict || !strings.Contains(rollbackRec.Body.String(), "no rollback target available") {
		t.Fatalf("rollback without target=%d %s", rollbackRec.Code, rollbackRec.Body.String())
	}
	if len(fake.pushed) != 3 {
		t.Fatalf("rollback without target pushed unexpectedly: %+v", fake.pushed)
	}

	if err := db.CreateConfig(models.Config{ID: "safe-config", Name: "safe", Content: "safe: true", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{WorkloadID: "wl-canary", ConfigID: "safe-config", Status: models.PushStatusApplied, AppliedAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rollbackRec = httptest.NewRecorder()
	router.ServeHTTP(rollbackRec, rollbackReq)
	if rollbackRec.Code != http.StatusAccepted {
		t.Fatalf("rollback with target=%d %s", rollbackRec.Code, rollbackRec.Body.String())
	}
	if len(fake.pushed) != 4 || fake.pushed[3].Target != "inst-a" || string(fake.pushed[3].Body) != "safe: true" {
		t.Fatalf("rollback did not push safe target only: %+v", fake.pushed)
	}

	fake.err = errors.New(sensitiveOpAMPErrorFixture)
	rollbackRec = httptest.NewRecorder()
	router.ServeHTTP(rollbackRec, rollbackReq)
	if rollbackRec.Code != http.StatusBadGateway {
		t.Fatalf("rollback failure=%d %s", rollbackRec.Code, rollbackRec.Body.String())
	}
	if strings.Contains(rollbackRec.Body.String(), "SECRET_TOKEN") {
		t.Fatalf("rollback leaked secret: %s", rollbackRec.Body.String())
	}

	fake.err = nil
	auditLog.failWith(errors.New("audit down"))
	failRec := postCanary(t, router, "wl-canary", `{"config":"`+jsonEsc(validCanaryConfig)+`","selection":{"strategy":"one","instance_uid":"inst-a"}}`)
	if failRec.Code != http.StatusServiceUnavailable || !strings.Contains(failRec.Body.String(), "side_effect_status") {
		t.Fatalf("audit failure=%d %s", failRec.Code, failRec.Body.String())
	}
}

func jsonEsc(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), "\"")
}
