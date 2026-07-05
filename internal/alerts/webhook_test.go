package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestNewWebhookNotifier_EmptyURLDisablesNotifier(t *testing.T) {
	if got := NewWebhookNotifier(""); got != nil {
		t.Fatalf("NewWebhookNotifier(empty) = %#v, want nil", got)
	}
}

func TestNewWebhookNotifier_ConfiguresEndpointAndTimeout(t *testing.T) {
	notifier := NewWebhookNotifier("https://example.com/webhook")
	if notifier == nil {
		t.Fatal("NewWebhookNotifier(non-empty) = nil, want notifier")
	}
	if notifier.url != "https://example.com/webhook" {
		t.Fatalf("url = %q, want configured endpoint", notifier.url)
	}
	if notifier.client == nil {
		t.Fatal("client = nil, want HTTP client")
	}
	if notifier.client.Timeout != 10*time.Second {
		t.Fatalf("client timeout = %s, want 10s", notifier.client.Timeout)
	}
}

func TestWebhookNotifierSend_PostsAlertPayloadOn2xx(t *testing.T) {
	alert := testWebhookAlert()
	requests := make(chan webhookRequest, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		requests <- webhookRequest{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			body:        body,
			err:         err,
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	NewWebhookNotifier(server.URL).Send(alert)

	var got webhookRequest
	select {
	case got = <-requests:
	case <-time.After(time.Second):
		t.Fatal("webhook server did not receive a request")
	}
	if got.err != nil {
		t.Fatalf("read request body: %v", got.err)
	}
	if got.method != http.MethodPost {
		t.Fatalf("method = %s, want POST", got.method)
	}
	if got.contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got.contentType)
	}

	var payload struct {
		Alert   models.Alert `json:"alert"`
		Event   string       `json:"event"`
		FiredAt string       `json:"fired_at"`
	}
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("unmarshal payload %q: %v", string(got.body), err)
	}
	if payload.Event != "alert_fired" {
		t.Fatalf("event = %q, want alert_fired", payload.Event)
	}
	if payload.FiredAt != alert.FiredAt.Format(time.RFC3339) {
		t.Fatalf("fired_at = %q, want %q", payload.FiredAt, alert.FiredAt.Format(time.RFC3339))
	}
	if payload.Alert.ID != alert.ID || payload.Alert.WorkloadID != alert.WorkloadID || payload.Alert.Rule != alert.Rule || payload.Alert.Severity != alert.Severity || payload.Alert.Message != alert.Message {
		t.Fatalf("alert payload = %+v, want %+v", payload.Alert, alert)
	}
	if !payload.Alert.FiredAt.Equal(alert.FiredAt) {
		t.Fatalf("alert.fired_at = %s, want %s", payload.Alert.FiredAt, alert.FiredAt)
	}
}

func TestWebhookNotifierSend_LogsServerErrorsFor4xxAnd5xx(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		status := status
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			logs := captureWebhookLogs(t, func() {
				NewWebhookNotifier(server.URL).Send(testWebhookAlert())
			})
			want := fmt.Sprintf("webhook: server returned %d", status)
			if !strings.Contains(logs, want) {
				t.Fatalf("logs = %q, want substring %q", logs, want)
			}
		})
	}
}

func TestWebhookNotifierSend_LogsMalformedURLError(t *testing.T) {
	logs := captureWebhookLogs(t, func() {
		NewWebhookNotifier("://example.com/webhook").Send(testWebhookAlert())
	})
	if !strings.Contains(logs, "webhook: send error:") {
		t.Fatalf("logs = %q, want send error", logs)
	}
}

func TestWebhookNotifierSend_LogsClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	notifier := NewWebhookNotifier(server.URL)
	notifier.client = &http.Client{Timeout: 10 * time.Millisecond}

	logs := captureWebhookLogs(t, func() {
		notifier.Send(testWebhookAlert())
	})
	if !strings.Contains(logs, "webhook: send error:") {
		t.Fatalf("logs = %q, want timeout send error", logs)
	}
}

type webhookRequest struct {
	method      string
	contentType string
	body        []byte
	err         error
}

func testWebhookAlert() models.Alert {
	return models.Alert{
		ID:         "alert-1",
		WorkloadID: "workload-1",
		Rule:       "workload_down",
		Severity:   "critical",
		Message:    "workload has not reported telemetry",
		FiredAt:    time.Date(2026, 7, 5, 12, 34, 56, 0, time.UTC),
	}
}

func captureWebhookLogs(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	}()

	fn()
	return buf.String()
}
