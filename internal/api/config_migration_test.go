package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestPreviewConfigMigration_ReturnsDraftAndDoesNotPersist(t *testing.T) {
	db, router, _ := newTestAPI(t)
	before, err := db.ListConfigs()
	if err != nil {
		t.Fatal(err)
	}

	body := `{"vendor":"datadog_agent","source":"api_key: dd-secret-token\nlogs:\n  - type: file\n    path: /var/log/app.log\n    service: checkout\n","source_format":"yaml","context":{"otlp_endpoint":"${OTLP_EXPORT_ENDPOINT}"}}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/migration-assistant/preview", body, []string{"editor"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "dd-secret-token") {
		t.Fatalf("response leaked secret: %s", rec.Body.String())
	}
	var resp models.ConfigMigrationPreviewResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.SchemaVersion != models.ConfigMigrationPreviewSchemaVersion || resp.Vendor != models.ConfigMigrationVendorDatadogAgent {
		t.Fatalf("unexpected response identity: %+v", resp)
	}
	if !strings.Contains(resp.DraftYAML, "filelog/datadog_logs_0") || resp.Validation == nil || !resp.Validation.Valid {
		t.Fatalf("unexpected draft/validation: %+v", resp)
	}
	after, err := db.ListConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("preview persisted configs: before=%d after=%d", len(before), len(after))
	}
}

func TestPreviewConfigMigration_RejectsInvalidRequests(t *testing.T) {
	_, router, _ := newTestAPI(t)
	tests := []struct {
		name string
		body string
		want int
	}{
		{"invalid json", `{`, http.StatusBadRequest},
		{"empty source", `{"vendor":"datadog_agent","source":"   "}`, http.StatusBadRequest},
		{"unsupported vendor", `{"vendor":"other","source":"logs: []"}`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := authedJSONRequest(t, http.MethodPost, "/api/configs/migration-assistant/preview", tt.body, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestPreviewConfigMigration_AcceptsSourceAtAssistantLimit(t *testing.T) {
	_, router, _ := newTestAPI(t)
	sourceAtLimit := strings.Repeat("x", 1<<20)
	body := `{"vendor":"datadog_agent","source":"` + sourceAtLimit + `"}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/migration-assistant/preview", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPreviewConfigMigration_RejectsSourceAboveAssistantLimit(t *testing.T) {
	_, router, _ := newTestAPI(t)
	oversizedSource := strings.Repeat("x", (1<<20)+1)
	body := `{"vendor":"datadog_agent","source":"` + oversizedSource + `"}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/migration-assistant/preview", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPreviewConfigMigration_RejectsOverLimitRequestBodyBeforeDecode(t *testing.T) {
	_, router, _ := newTestAPI(t)
	body := `{"vendor":"datadog_agent","source":"dogstatsd_port: 8125","context":{"notes":"` + strings.Repeat("x", (2<<20)) + `"}}`
	req := authedJSONRequest(t, http.MethodPost, "/api/configs/migration-assistant/preview", body, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}
}
