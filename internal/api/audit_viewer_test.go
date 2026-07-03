package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type queryableAuditLogger struct {
	recordingAuditLogger
	gotFilter ext.AuditEventFilter
	result    ext.AuditEventPage
	err       error
}

func (q *queryableAuditLogger) ListAuditEvents(_ context.Context, filter ext.AuditEventFilter) (ext.AuditEventPage, error) {
	q.gotFilter = filter
	return q.result, q.err
}

func newAuditViewerTestAPI(t *testing.T, logger ext.AuditLogger) http.Handler {
	t.Helper()
	db, err := store.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	authSvc := auth.New("test-secret-key-at-least-32-bytes!")
	return NewRouter(db, authSvc, nil, &fakeOpAMPPusher{}, logger, "", nil, nil, 30*24*time.Hour, map[string]bool{FeatureAuditViewer: true}, nil, nil)
}

func TestAuditEventsRequiresAuditViewPermission(t *testing.T) {
	router := newAuditViewerTestAPI(t, &queryableAuditLogger{})

	req := authedJSONRequest(t, http.MethodGet, "/api/audit/events", "", []string{"viewer"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAuditEventsReturnsUnavailablePageWhenLoggerIsWriteOnly(t *testing.T) {
	router := newAuditViewerTestAPI(t, ext.NopAuditLogger{})

	req := authedJSONRequest(t, http.MethodGet, "/api/audit/events", "", []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Available bool              `json:"available"`
		Events    []ext.AuditRecord `json:"events"`
		Limit     int               `json:"limit"`
		Offset    int               `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Available {
		t.Fatal("available = true, want false")
	}
	if len(body.Events) != 0 {
		t.Fatalf("events length = %d, want 0", len(body.Events))
	}
	if body.Limit != 50 || body.Offset != 0 {
		t.Fatalf("pagination = limit %d offset %d, want 50/0", body.Limit, body.Offset)
	}
}

func TestAuditEventsPassesFiltersAndReturnsRecords(t *testing.T) {
	from := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	logger := &queryableAuditLogger{result: ext.AuditEventPage{
		Total: 1,
		Events: []ext.AuditRecord{{
			ID:           "evt-1",
			OccurredAt:   from.Add(time.Hour),
			Action:       "config.push",
			UserID:       "u-1",
			Email:        "admin@example.com",
			Resource:     "workload",
			ResourceID:   "workload-1",
			WorkloadID:   "workload-1",
			ConfigHash:   "sha256:abc",
			Detail:       "sha256:abc",
			PrevHash:     "prev-chain",
			EventHash:    "event-chain",
			ImmutableRef: "ledger-42",
		}},
	}}
	router := newAuditViewerTestAPI(t, logger)

	url := "/api/audit/events?user=admin%40example.com&user_id=u-1&email=admin%40example.com&action=config.push&resource_id=workload-1&workload_id=workload-1&config_hash=sha256%3Aabc&from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&limit=25&offset=50"
	req := authedJSONRequest(t, http.MethodGet, url, "", []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if logger.gotFilter.User != "admin@example.com" || logger.gotFilter.Action != "config.push" || logger.gotFilter.ResourceID != "workload-1" || logger.gotFilter.WorkloadID != "workload-1" || logger.gotFilter.ConfigHash != "sha256:abc" {
		t.Fatalf("filter not propagated: %+v", logger.gotFilter)
	}
	if logger.gotFilter.UserID != "u-1" || logger.gotFilter.Email != "admin@example.com" {
		t.Fatalf("user/email filter not propagated: %+v", logger.gotFilter)
	}
	if !logger.gotFilter.From.Equal(from) || !logger.gotFilter.To.Equal(to) {
		t.Fatalf("date filter = %s/%s, want %s/%s", logger.gotFilter.From, logger.gotFilter.To, from, to)
	}
	if logger.gotFilter.Limit != 25 || logger.gotFilter.Offset != 50 {
		t.Fatalf("pagination = %d/%d, want 25/50", logger.gotFilter.Limit, logger.gotFilter.Offset)
	}

	var body struct {
		Available bool              `json:"available"`
		Events    []ext.AuditRecord `json:"events"`
		Total     int               `json:"total"`
		Limit     int               `json:"limit"`
		Offset    int               `json:"offset"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Available || body.Total != 1 || len(body.Events) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if body.Events[0].WorkloadID != "workload-1" || body.Events[0].ConfigHash != "sha256:abc" || body.Events[0].EventHash != "event-chain" {
		t.Fatalf("record metadata missing: %+v", body.Events[0])
	}
}

func TestAuditEventsQueryFailureReturnsGenericUnavailable(t *testing.T) {
	router := newAuditViewerTestAPI(t, &queryableAuditLogger{err: errors.New("backend detail: secret DSN")})

	req := authedJSONRequest(t, http.MethodGet, "/api/audit/events", "", []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "audit unavailable") || strings.Contains(body, "secret DSN") {
		t.Fatalf("body leaked backend detail or missed generic error: %s", body)
	}
}

func TestAuditEventsCSVExportsRecords(t *testing.T) {
	occurredAt := time.Date(2026, 7, 1, 11, 0, 0, 0, time.UTC)
	logger := &queryableAuditLogger{result: ext.AuditEventPage{Total: 1, Events: []ext.AuditRecord{{
		ID: "evt-1", OccurredAt: occurredAt, Action: "config.push", UserID: "u-1", Email: "admin@example.com", Resource: "workload", ResourceID: "workload-1", WorkloadID: "workload-1", ConfigHash: "sha256:abc", PrevHash: "prev-chain", EventHash: "event-chain", ImmutableRef: "ledger-42",
	}}}}
	router := newAuditViewerTestAPI(t, logger)

	req := authedJSONRequest(t, http.MethodGet, "/api/audit/events.csv?limit=10", "", []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("Content-Type = %q, want text/csv", ct)
	}
	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv read: %v", err)
	}
	wantHeader := []string{"id", "occurred_at", "action", "user_id", "email", "resource", "resource_id", "workload_id", "config_hash", "detail", "prev_hash", "event_hash", "immutable_ref"}
	if strings.Join(records[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("header = %v, want %v", records[0], wantHeader)
	}
	if records[1][7] != "workload-1" || records[1][8] != "sha256:abc" || records[1][11] != "event-chain" {
		t.Fatalf("row metadata = %v", records[1])
	}
}

func TestAuditEventsCSVNeutralizesSpreadsheetFormulas(t *testing.T) {
	occurredAt := time.Date(2026, 7, 1, 11, 0, 0, 0, time.UTC)
	logger := &queryableAuditLogger{result: ext.AuditEventPage{Total: 1, Events: []ext.AuditRecord{{
		ID: "=evt-1", OccurredAt: occurredAt, Action: "+config.push", UserID: "-u-1", Email: "@admin.example", Resource: "	workload", ResourceID: "\rworkload-1", WorkloadID: "=workload-1", ConfigHash: "+sha256:abc", Detail: "-detail", PrevHash: "@prev-chain", EventHash: "	event-chain", ImmutableRef: "\rledger-42",
	}}}}
	router := newAuditViewerTestAPI(t, logger)

	req := authedJSONRequest(t, http.MethodGet, "/api/audit/events.csv?limit=10", "", []string{"administrator"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	records, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("csv read: %v", err)
	}
	want := []string{"'=evt-1", occurredAt.UTC().Format(time.RFC3339Nano), "'+config.push", "'-u-1", "'@admin.example", "'	workload", "'\rworkload-1", "'=workload-1", "'+sha256:abc", "'-detail", "'@prev-chain", "'	event-chain", "'\rledger-42"}
	if len(records) != 2 {
		t.Fatalf("records length = %d, want 2", len(records))
	}
	if strings.Join(records[1], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("row = %#v, want %#v", records[1], want)
	}

	jsonReq := authedJSONRequest(t, http.MethodGet, "/api/audit/events?limit=10", "", []string{"administrator"})
	jsonRec := httptest.NewRecorder()
	router.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("json status = %d, body = %s", jsonRec.Code, jsonRec.Body.String())
	}
	var body struct {
		Events []ext.AuditRecord `json:"events"`
	}
	if err := json.Unmarshal(jsonRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if len(body.Events) != 1 || body.Events[0].ID != "=evt-1" || body.Events[0].Detail != "-detail" {
		t.Fatalf("json response was unexpectedly neutralized: %+v", body.Events)
	}
}
