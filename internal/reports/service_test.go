package reports

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func newReportTestStore(t *testing.T) ext.Store {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedReportWorkload(t *testing.T, db ext.Store, id, name string, at time.Time) {
	t.Helper()
	if err := db.UpsertWorkload(models.Workload{
		ID: id, DisplayName: name, Type: "collector", Version: "0.99.0", Status: "connected", LastSeenAt: at,
		Labels:          models.Labels{"otel.magnify/selector.env": "prod", "SECRET_TOKEN": "should-not-leak"},
		FingerprintKeys: models.FingerprintKeys{"service.name": name}, ActiveConfigHash: "cfg-" + id,
		AcceptsRemoteConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateConfig(models.Config{ID: "cfg-" + id, Name: "cfg-" + id, Content: "receivers:\n  otlp:\nexporters:\n  debug:", CreatedAt: at}); err != nil {
		t.Fatal(err)
	}
	label := "stable"
	if err := db.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: id, ConfigID: "cfg-" + id, ConfigHash: "cfg-" + id, AppliedAt: at, SubmittedAt: at, Status: models.PushStatusFailed,
		ErrorMessage: "authorization=Bearer secret endpoint=https://tenant-a.internal:4318 SECRET_TOKEN=abc", PushedBy: "operator@example.com",
		Content: "receivers:\n  otlp:\nexporters:\n  debug:", Label: &label, TargetCount: 2, FailedCount: 1, ContentAvailable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.InsertWorkloadEvent(models.WorkloadEvent{WorkloadID: id, InstanceUID: "pod-a", PodName: "pod-a", EventType: "connected", Version: "0.99.0", OccurredAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAlert(models.Alert{ID: "alert-" + id, WorkloadID: id, Rule: "version_outdated", Severity: "warning", Message: "collector outdated", FiredAt: at}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildEvidencePack_DeterministicRedactedAndSigned(t *testing.T) {
	db := newReportTestStore(t)
	fixed := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	seedReportWorkload(t, db, "w-b", "zeta", fixed.Add(-time.Hour))
	seedReportWorkload(t, db, "w-a", "alpha", fixed.Add(-2*time.Hour))

	svc := NewService(db, ServiceOptions{Now: func() time.Time { return fixed }, Signer: ext.NopReportSigner{}})
	req := models.ReportExportRequest{ReportType: models.ReportTypeEvidencePack, Scope: models.ReportScope{WorkloadIDs: []string{"w-b", "w-a"}}}
	pack1, err := svc.BuildEvidencePack(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	pack2, err := svc.BuildEvidencePack(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if pack1.InputsHash == "" || pack1.ReportHash == "" || pack1.InputsHash != pack2.InputsHash || pack1.ReportHash != pack2.ReportHash {
		t.Fatalf("hashes not stable: %#v %#v", pack1, pack2)
	}
	if len(pack1.Signatures) != 1 || pack1.Signatures[0].Scheme != models.ReportSignatureSchemeNone || pack1.Signatures[0].PayloadHash != pack1.ReportHash {
		t.Fatalf("nop signature not attached to report hash: %+v", pack1.Signatures)
	}
	if got := pack1.Sections[0].Items[0].ResourceID; got != "w-a" {
		t.Fatalf("workloads not sorted by display name/id, first=%q", got)
	}
	sections := map[string]bool{}
	for _, sec := range pack1.Sections {
		sections[sec.ID] = true
	}
	for _, want := range []string{"workloads", "config_history", "current_config", "drift", "version_intelligence", "alerts", "workload_events", "rollback_readiness", "audit_verification"} {
		if !sections[want] {
			t.Fatalf("missing evidence section %q in %+v", want, sections)
		}
	}
	if len(pack1.Warnings) == 0 || pack1.Warnings[0].Code != "config_plan_not_persisted" {
		t.Fatalf("missing config plan availability warning: %+v", pack1.Warnings)
	}
	md, err := RenderMarkdown(pack1)
	if err != nil {
		t.Fatal(err)
	}
	out := string(md)
	for _, leaked := range []string{"SECRET_TOKEN", "tenant-a.internal", "Bearer secret", "operator@example.com"} {
		if strings.Contains(out, leaked) {
			t.Fatalf("markdown leaked %q:\n%s", leaked, out)
		}
	}
}

func TestRenderCSV_FixedColumnsAndSortedFactKeys(t *testing.T) {
	at := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	pack := models.EvidencePack{SchemaVersion: models.EvidencePackSchemaVersion, GeneratedAt: at, InputsHash: "abc", ReportHash: "def", Sections: []models.EvidenceSection{{ID: "workloads", Title: "Workloads", Order: 10, Items: []models.EvidenceItem{{ID: "i1", Resource: "workload", ResourceID: "w1", ObservedAt: &at, Summary: "one", Facts: map[string]any{"z": "last", "a": "first"}, ContentHash: "h", Redacted: true}}}}}
	csvBytes, err := RenderCSV(pack)
	if err != nil {
		t.Fatal(err)
	}
	got := string(csvBytes)
	wantPrefix := "section_id,item_id,resource,resource_id,observed_at,severity,summary,key,value,content_hash,redacted\nworkloads,i1,workload,w1,2026-07-02T12:00:00Z,,one,a,first,h,true\nworkloads,i1,workload,w1,2026-07-02T12:00:00Z,,one,z,last,h,true\n"
	if got != wantPrefix {
		t.Fatalf("csv mismatch:\n%s", got)
	}
}

func TestRenderPDFMinimal_DeterministicPDFBytes(t *testing.T) {
	pack := models.EvidencePack{SchemaVersion: models.EvidencePackSchemaVersion, GeneratedAt: time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC), InputsHash: "abc", ReportHash: "def"}
	pdf1, err := RenderPDFMinimal(pack)
	if err != nil {
		t.Fatal(err)
	}
	pdf2, _ := RenderPDFMinimal(pack)
	if !strings.HasPrefix(string(pdf1), "%PDF-1.4") || string(pdf1) != string(pdf2) || !strings.Contains(string(pdf1), "Evidence Pack") {
		t.Fatalf("unexpected pdf bytes: %q", string(pdf1))
	}
}
