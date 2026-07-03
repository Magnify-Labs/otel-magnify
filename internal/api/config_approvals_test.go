package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

const approvalDraftYAML = "receivers:\n  otlp:\nprocessors:\n  batch:\nexporters:\n  debug:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]\n"

func seedApprovalWorkload(t *testing.T, db ext.Store, id string) {
	t.Helper()
	if err := db.UpsertWorkload(models.Workload{ID: id, Type: "collector", Status: "connected", LastSeenAt: time.Now().UTC(), Labels: models.Labels{}, AcceptsRemoteConfig: true}); err != nil {
		t.Fatal(err)
	}
}

func requestApproval(t *testing.T, router http.Handler, workloadID, body string) (int, models.ConfigApprovalRequest, string) {
	t.Helper()
	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/"+workloadID+"/config/approvals", body, []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var approval models.ConfigApprovalRequest
	_ = json.NewDecoder(rec.Body).Decode(&approval)
	return rec.Code, approval, rec.Body.String()
}

func approveApproval(t *testing.T, router http.Handler, workloadID, approvalID string) (int, string) {
	t.Helper()
	body := `{"comment":"looks good"}`
	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/"+workloadID+"/config/approvals/"+approvalID+"/approve", body, []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func pushApproval(t *testing.T, router http.Handler, workloadID, approvalID, body string) (int, string) {
	t.Helper()
	req := authedJSONRequest(t, http.MethodPost, "/api/workloads/"+workloadID+"/config/approvals/"+approvalID+"/push", body, []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestConfigApprovals_RequestRejectsEmptyAndInvalidDrafts(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-invalid")

	for _, tc := range []struct {
		name string
		body string
	}{
		{"empty", `{"draft_yaml":"","target_group":"staging","comment":"please review"}`},
		{"invalid_yaml", `{"draft_yaml":"receivers: [","target_group":"staging","comment":"please review"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, body := requestApproval(t, router, "w-approval-invalid", tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, body=%s", code, body)
			}
		})
	}
}

func TestConfigApprovals_ApproveAndPushRejectInvalidStoredDrafts(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-stored-invalid")

	pending, err := db.CreateOrUpdateConfigApprovalRequest(models.ConfigApprovalRequest{
		WorkloadID:     "w-approval-stored-invalid",
		DraftYAML:      "receivers: [",
		TargetGroup:    "staging",
		Requester:      "operator@example.com",
		RequestComment: "seed invalid pending draft",
		Status:         models.ConfigApprovalStatusPending,
	})
	if err != nil {
		t.Fatalf("seed pending approval: %v", err)
	}
	code, body := approveApproval(t, router, "w-approval-stored-invalid", pending.ID)
	if code != http.StatusBadRequest {
		t.Fatalf("approve invalid draft status = %d, body=%s", code, body)
	}

	approved, err := db.CreateOrUpdateConfigApprovalRequest(models.ConfigApprovalRequest{
		WorkloadID:     "w-approval-stored-invalid",
		DraftYAML:      "receivers: [",
		TargetGroup:    "breakfix",
		Requester:      "operator@example.com",
		RequestComment: "seed invalid approved draft",
		Status:         models.ConfigApprovalStatusApproved,
	})
	if err != nil {
		t.Fatalf("seed approved approval: %v", err)
	}
	code, body = pushApproval(t, router, "w-approval-stored-invalid", approved.ID, `{"comment":"try invalid approved draft"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("push invalid draft status = %d, body=%s", code, body)
	}
}

func TestConfigApprovals_NormalRequestApprovePushAuditsAndPushes(t *testing.T) {
	db, router, opamp, audit := newAuditTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-normal")

	requestBody := `{"draft_yaml":` + strconvQuote(approvalDraftYAML) + `,"target_group":"staging","target_env":"staging","comment":"please review staging rollout"}`
	code, approval, body := requestApproval(t, router, "w-approval-normal", requestBody)
	if code != http.StatusCreated {
		t.Fatalf("request status = %d, body=%s", code, body)
	}
	if approval.Status != models.ConfigApprovalStatusPending || approval.ID == "" {
		t.Fatalf("approval = %+v", approval)
	}
	if got := findEvent(audit.snapshot(), "config.approval.request"); got == nil || got.ResourceID != "w-approval-normal" || !strings.Contains(got.Detail, approval.ID) {
		t.Fatalf("missing request audit event: %+v", audit.snapshot())
	}

	code, body = pushApproval(t, router, "w-approval-normal", approval.ID, `{"comment":"trying before approval"}`)
	if code != http.StatusConflict {
		t.Fatalf("unapproved push status = %d, body=%s", code, body)
	}

	code, body = approveApproval(t, router, "w-approval-normal", approval.ID)
	if code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", code, body)
	}
	if got := findEvent(audit.snapshot(), "config.approval.approve"); got == nil || got.ResourceID != "w-approval-normal" || !strings.Contains(got.Detail, approval.ID) {
		t.Fatalf("missing approval audit event: %+v", audit.snapshot())
	}

	code, body = pushApproval(t, router, "w-approval-normal", approval.ID, `{"comment":"roll out approved staging config"}`)
	if code != http.StatusAccepted {
		t.Fatalf("push status = %d, body=%s", code, body)
	}
	if len(opamp.pushed) != 1 || string(opamp.pushed[0].Body) != approvalDraftYAML {
		t.Fatalf("pushed = %+v", opamp.pushed)
	}
	if got := findEvent(audit.snapshot(), "config.approval.push"); got == nil || got.ResourceID != "w-approval-normal" || !strings.Contains(got.Detail, approval.ID) {
		t.Fatalf("missing push audit event: %+v", audit.snapshot())
	}
}

func TestConfigApprovals_ProdPushRequiresCommentAndDoubleConfirmation(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-prod")
	missingConfirmationBody := `{"draft_yaml":` + strconvQuote(approvalDraftYAML) + `,"target_group":"prod-collectors","target_env":"prod","comment":"please review prod"}`
	if code, _, body := requestApproval(t, router, "w-approval-prod", missingConfirmationBody); code != http.StatusBadRequest {
		t.Fatalf("prod request without confirmation status = %d, want %d, body=%s", code, http.StatusBadRequest, body)
	}

	requestBody := `{"draft_yaml":` + strconvQuote(approvalDraftYAML) + `,"target_group":"prod-collectors","target_env":"prod","comment":"please review prod","prod_confirmation":true}`
	code, approval, body := requestApproval(t, router, "w-approval-prod", requestBody)
	if code != http.StatusCreated {
		t.Fatalf("request status = %d, body=%s", code, body)
	}
	if code, body = approveApproval(t, router, "w-approval-prod", approval.ID); code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", code, body)
	}

	if code, body = pushApproval(t, router, "w-approval-prod", approval.ID, `{"prod_double_confirmed":true}`); code != http.StatusBadRequest {
		t.Fatalf("push without comment status = %d, body=%s", code, body)
	}
	if code, body = pushApproval(t, router, "w-approval-prod", approval.ID, `{"comment":"prod rollout"}`); code != http.StatusBadRequest {
		t.Fatalf("push without double confirm status = %d, body=%s", code, body)
	}
}

func TestConfigApprovals_BreakGlassRequiresReasonAndDistinctAudit(t *testing.T) {
	db, router, opamp, audit := newAuditTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-breakglass")
	requestBody := `{"draft_yaml":` + strconvQuote(approvalDraftYAML) + `,"target_group":"prod-collectors","target_env":"prod","comment":"prod emergency draft","prod_confirmation":true}`
	code, approval, body := requestApproval(t, router, "w-approval-breakglass", requestBody)
	if code != http.StatusCreated {
		t.Fatalf("request status = %d, body=%s", code, body)
	}

	if code, body = pushApproval(t, router, "w-approval-breakglass", approval.ID, `{"break_glass":true,"comment":"emergency push","prod_double_confirmed":true}`); code != http.StatusBadRequest {
		t.Fatalf("break-glass without reason status = %d, body=%s", code, body)
	}
	if code, body = pushApproval(t, router, "w-approval-breakglass", approval.ID, `{"break_glass":true,"break_glass_reason":"stop outage","comment":"emergency push","prod_double_confirmed":true}`); code != http.StatusAccepted {
		t.Fatalf("break-glass push status = %d, body=%s", code, body)
	}
	if len(opamp.pushed) != 1 {
		t.Fatalf("pushed = %+v", opamp.pushed)
	}
	if got := findEvent(audit.snapshot(), "config.approval.break_glass_push"); got == nil || got.ResourceID != "w-approval-breakglass" || !strings.Contains(got.Detail, "stop outage") {
		t.Fatalf("missing break-glass audit event: %+v", audit.snapshot())
	}
}

func TestConfigApprovals_BreakGlassCannotPushAlreadyPushedApproval(t *testing.T) {
	db, router, opamp, _ := newAuditTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-breakglass-pushed")
	requestBody := `{"draft_yaml":` + strconvQuote(approvalDraftYAML) + `,"target_group":"prod-collectors","target_env":"prod","comment":"prod draft","prod_confirmation":true}`
	code, approval, body := requestApproval(t, router, "w-approval-breakglass-pushed", requestBody)
	if code != http.StatusCreated {
		t.Fatalf("request status = %d, body=%s", code, body)
	}
	if code, body = approveApproval(t, router, "w-approval-breakglass-pushed", approval.ID); code != http.StatusOK {
		t.Fatalf("approve status = %d, body=%s", code, body)
	}
	if code, body = pushApproval(t, router, "w-approval-breakglass-pushed", approval.ID, `{"comment":"approved prod rollout","prod_double_confirmed":true}`); code != http.StatusAccepted {
		t.Fatalf("first push status = %d, body=%s", code, body)
	}

	code, body = pushApproval(t, router, "w-approval-breakglass-pushed", approval.ID, `{"break_glass":true,"break_glass_reason":"retry outage","comment":"do not push twice","prod_double_confirmed":true}`)
	if code != http.StatusConflict {
		t.Fatalf("second break-glass push status = %d, want %d, body=%s", code, http.StatusConflict, body)
	}
	if len(opamp.pushed) != 1 {
		t.Fatalf("pushed calls = %+v, want exactly first push", opamp.pushed)
	}
}

func TestConfigApprovals_RBACMatchesPushPermission(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-rbac")
	body := `{"draft_yaml":` + strconvQuote(approvalDraftYAML) + `,"target_group":"staging","comment":"please review"}`

	for _, tc := range []struct {
		groups []string
		want   int
	}{
		{[]string{"viewer"}, http.StatusForbidden},
		{[]string{"editor"}, http.StatusCreated},
		{[]string{"administrator"}, http.StatusCreated},
	} {
		req := authedJSONRequest(t, http.MethodPost, "/api/workloads/w-approval-rbac/config/approvals", body, tc.groups)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("groups %v status = %d, want %d, body=%s", tc.groups, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestConfigApprovals_LegacyDirectPushCannotBypassApprovalFlow(t *testing.T) {
	db, router, opamp := newTestAPI(t)
	seedApprovalWorkload(t, db, "w-legacy-direct-push")

	req := authedPost(t, "/api/workloads/w-legacy-direct-push/config", approvalDraftYAML)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("legacy direct push status = %d, want %d, body=%s", rec.Code, http.StatusGone, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/config/approvals") {
		t.Fatalf("legacy direct push response should point clients at approvals endpoint, body=%s", rec.Body.String())
	}
	if len(opamp.pushed) != 0 {
		t.Fatalf("legacy direct push reached OpAMP: %+v", opamp.pushed)
	}
	history, err := db.GetWorkloadConfigHistory("w-legacy-direct-push")
	if err != nil {
		t.Fatalf("load workload config history: %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("legacy direct push recorded workload config history despite rejection: %+v", history)
	}
}

func TestConfigApprovals_ListRequiresPushPermissionAndDoesNotLeakDraftsToViewers(t *testing.T) {
	db, router, _ := newTestAPI(t)
	seedApprovalWorkload(t, db, "w-approval-list-rbac")
	body := `{"draft_yaml":` + strconvQuote(approvalDraftYAML) + `,"target_group":"staging","comment":"contains sensitive draft"}`
	code, approval, responseBody := requestApproval(t, router, "w-approval-list-rbac", body)
	if code != http.StatusCreated {
		t.Fatalf("request approval status = %d, body=%s", code, responseBody)
	}

	viewerReq := authedRequestForGroups(t, http.MethodGet, "/api/workloads/w-approval-list-rbac/config/approvals", "", []string{"viewer"})
	viewerRec := httptest.NewRecorder()
	router.ServeHTTP(viewerRec, viewerReq)
	if viewerRec.Code != http.StatusForbidden {
		t.Fatalf("viewer list approvals status = %d, want %d, body=%s", viewerRec.Code, http.StatusForbidden, viewerRec.Body.String())
	}
	if strings.Contains(viewerRec.Body.String(), approvalDraftYAML) || strings.Contains(viewerRec.Body.String(), approval.RequestComment) {
		t.Fatalf("viewer response leaked approval internals: %s", viewerRec.Body.String())
	}

	editorReq := authedRequestForGroups(t, http.MethodGet, "/api/workloads/w-approval-list-rbac/config/approvals", "", []string{"editor"})
	editorRec := httptest.NewRecorder()
	router.ServeHTTP(editorRec, editorReq)
	if editorRec.Code != http.StatusOK {
		t.Fatalf("editor list approvals status = %d, want %d, body=%s", editorRec.Code, http.StatusOK, editorRec.Body.String())
	}
	var editorApprovals []models.ConfigApprovalRequest
	if err := json.NewDecoder(editorRec.Body).Decode(&editorApprovals); err != nil {
		t.Fatalf("decode editor approvals: %v", err)
	}
	if len(editorApprovals) != 1 || editorApprovals[0].DraftYAML != approvalDraftYAML {
		t.Fatalf("editor response should include draft for reviewers, approvals=%+v", editorApprovals)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
