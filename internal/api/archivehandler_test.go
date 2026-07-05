package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestHandleArchiveWorkload_EditorCanArchive(t *testing.T) {
	db, auth := newMeTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "disconnected",
		LastSeenAt:      time.Now().UTC(),
		Labels:          models.Labels{},
		FingerprintKeys: models.FingerprintKeys{},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"editor"})
	req := httptest.NewRequest(http.MethodPost, "/api/workloads/w1/archive", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	wl, err := db.GetWorkload("w1")
	if err != nil {
		t.Fatalf("get workload: %v", err)
	}
	if wl.ArchivedAt == nil {
		t.Fatal("expected workload to be archived immediately")
	}
	if wl.Status != "disconnected" {
		t.Fatalf("status = %q, want disconnected", wl.Status)
	}
}

func TestHandleArchiveWorkload_ViewerForbidden(t *testing.T) {
	db, auth := newMeTestAPI(t)
	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"viewer"})
	req := httptest.NewRequest(http.MethodPost, "/api/workloads/w1/archive", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", rec.Code)
	}
}

func TestHandleArchiveWorkload_ConnectedWorkloadRejected(t *testing.T) {
	db, auth := newMeTestAPI(t)
	if err := db.UpsertWorkload(models.Workload{
		ID: "w1", Type: "collector", Status: "connected",
		LastSeenAt:      time.Now().UTC(),
		Labels:          models.Labels{},
		FingerprintKeys: models.FingerprintKeys{},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tok, _ := auth.GenerateToken("u1", "u1@x.com", []string{"editor"})
	req := httptest.NewRequest(http.MethodPost, "/api/workloads/w1/archive", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	buildMeTestRouter(db, auth).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
