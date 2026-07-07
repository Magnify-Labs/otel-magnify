package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
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

func TestNewWebhookNotifier_BlocksUnsafeTargets(t *testing.T) {
	unsafeURLs := []string{
		"http://hooks.example.com/webhook",
		"ftp://hooks.example.com/webhook",
		"https:///missing-host",
		"https://user:pass@hooks.example.com/webhook",
		"https://localhost/webhook",
		"https://collector.localhost/webhook",
		"https://127.0.0.1/webhook",
		"https://[::1]/webhook",
		"https://169.254.169.254/latest/meta-data/",
		"https://10.0.0.5/webhook",
		"https://172.16.0.5/webhook",
		"https://192.168.1.10/webhook",
		"https://[fc00::1]/webhook",
		"https://[fe80::1]/webhook",
		"https://[64:ff9b::a9fe:a9fe]/webhook",
		"https://[64:ff9b:1::a9fe:a9fe]/webhook",
		"https://[2001:0:4136:e378:8000:63bf:3fff:fdd2]/webhook",
		"https://[2001:2::1]/webhook",
		"https://[2001:20::1]/webhook",
		"https://[2002:a9fe:a9fe::1]/webhook",
		"https://192.0.2.1/webhook",
	}

	for _, rawURL := range unsafeURLs {
		t.Run(rawURL, func(t *testing.T) {
			if got := NewWebhookNotifier(rawURL); got != nil {
				t.Fatalf("NewWebhookNotifier(%q) = %#v, want nil", rawURL, got)
			}
		})
	}
}

func TestIsSafeWebhookIP_BlocksIPv6TransitionAndSpecialUseRanges(t *testing.T) {
	unsafeIPs := []string{
		"64:ff9b::a9fe:a9fe",
		"64:ff9b:1::a9fe:a9fe",
		"2001:0:4136:e378:8000:63bf:3fff:fdd2",
		"2001:2::1",
		"2001:20::1",
		"2002:a9fe:a9fe::1",
	}

	for _, rawIP := range unsafeIPs {
		t.Run(rawIP, func(t *testing.T) {
			addr := netip.MustParseAddr(rawIP)
			if isSafeWebhookIP(addr) {
				t.Fatalf("isSafeWebhookIP(%q) = true, want false", rawIP)
			}
		})
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

	(&WebhookNotifier{url: server.URL, client: server.Client()}).Send(alert)

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
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer server.Close()

			logs := captureWebhookLogs(t, func() {
				(&WebhookNotifier{url: server.URL, client: server.Client()}).Send(testWebhookAlert())
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
		(&WebhookNotifier{url: "://example.com/webhook", client: &http.Client{Timeout: 10 * time.Second}}).Send(testWebhookAlert())
	})
	if !strings.Contains(logs, "webhook: send error:") {
		t.Fatalf("logs = %q, want send error", logs)
	}
}

func TestWebhookNotifierSend_LogsClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	notifier := &WebhookNotifier{url: server.URL, client: &http.Client{Timeout: 10 * time.Millisecond}}

	logs := captureWebhookLogs(t, func() {
		notifier.Send(testWebhookAlert())
	})
	if !strings.Contains(logs, "webhook: send error:") {
		t.Fatalf("logs = %q, want timeout send error", logs)
	}
}

func TestSafeWebhookClientRejectsUnsafeRedirectTargets(t *testing.T) {
	client := newSafeWebhookHTTPClient()
	redirectURL, err := url.Parse("https://169.254.169.254/latest/meta-data/")
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}

	err = client.CheckRedirect(&http.Request{URL: redirectURL}, nil)
	if !errors.Is(err, errUnsafeWebhookURL) {
		t.Fatalf("CheckRedirect error = %v, want %v", err, errUnsafeWebhookURL)
	}
}

func TestSafeWebhookDialAddressRejectsUnsafeDNSResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := safeWebhookDialAddress(ctx, "localhost", "443")
	if !errors.Is(err, errUnsafeWebhookURL) {
		t.Fatalf("safeWebhookDialAddress(localhost) error = %v, want %v", err, errUnsafeWebhookURL)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		t.Fatalf("safeWebhookDialAddress(localhost) returned DNS error %v, want unsafe URL rejection", err)
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
