package store

import (
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestConfigApprovalRequestLifecyclePersistsStateTransitions(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w-approval")

	draft := "receivers:\n  otlp:\nprocessors:\n  batch:\nexporters:\n  debug:\nservice:\n  pipelines:\n    traces:\n      receivers: [otlp]\n      processors: [batch]\n      exporters: [debug]\n"
	req, err := db.CreateOrUpdateConfigApprovalRequest(models.ConfigApprovalRequest{
		WorkloadID:       "w-approval",
		DraftYAML:        draft,
		TargetGroup:      "prod-collectors",
		TargetEnv:        "prod",
		Requester:        "operator@example.com",
		RequestComment:   "roll out safer collector config",
		ProdTarget:       true,
		ProdConfirmation: true,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateConfigApprovalRequest: %v", err)
	}
	if req.ID == "" || req.Status != models.ConfigApprovalStatusPending {
		t.Fatalf("created request = %+v", req)
	}

	approved, err := db.ApproveConfigApprovalRequest(req.ID, "approver@example.com", "approved", time.Now().UTC())
	if err != nil {
		t.Fatalf("ApproveConfigApprovalRequest: %v", err)
	}
	if approved.Status != models.ConfigApprovalStatusApproved || approved.ApprovedBy == nil || *approved.ApprovedBy != "approver@example.com" || approved.ApprovedAt == nil {
		t.Fatalf("approved request = %+v", approved)
	}

	pushed, err := db.MarkConfigApprovalRequestPushed(req.ID, "hash-123", "pushed by operator", true, false, "", time.Now().UTC())
	if err != nil {
		t.Fatalf("MarkConfigApprovalRequestPushed: %v", err)
	}
	if pushed.Status != models.ConfigApprovalStatusPushed || pushed.ConfigHash == nil || *pushed.ConfigHash != "hash-123" || pushed.PushComment == nil || *pushed.PushComment != "pushed by operator" {
		t.Fatalf("pushed request = %+v", pushed)
	}

	got, err := db.GetConfigApprovalRequest(req.ID)
	if err != nil {
		t.Fatalf("GetConfigApprovalRequest: %v", err)
	}
	if got.WorkloadID != "w-approval" || got.DraftYAML != draft || got.TargetGroup != "prod-collectors" || !got.ProdTarget || !got.ProdConfirmation {
		t.Fatalf("persisted request = %+v", got)
	}
}

func TestConfigApprovalRequestCreateOrUpdateKeepsOnePendingDraftPerWorkloadAndTarget(t *testing.T) {
	db := newTestDB(t)
	seedWorkload(t, db, "w-upsert")

	first, err := db.CreateOrUpdateConfigApprovalRequest(models.ConfigApprovalRequest{WorkloadID: "w-upsert", DraftYAML: "receivers: {}", TargetGroup: "staging", TargetEnv: "staging", Requester: "one@example.com", RequestComment: "first"})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := db.CreateOrUpdateConfigApprovalRequest(models.ConfigApprovalRequest{WorkloadID: "w-upsert", DraftYAML: "receivers: {otlp: {}}", TargetGroup: "staging", TargetEnv: "staging", Requester: "two@example.com", RequestComment: "second"})
	if err != nil {
		t.Fatalf("update second: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("ID = %q, want same pending request %q", second.ID, first.ID)
	}
	list, err := db.ListConfigApprovalRequests("w-upsert")
	if err != nil {
		t.Fatalf("ListConfigApprovalRequests: %v", err)
	}
	if len(list) != 1 || list[0].DraftYAML != "receivers: {otlp: {}}" || list[0].Requester != "two@example.com" {
		t.Fatalf("list = %+v", list)
	}
}
