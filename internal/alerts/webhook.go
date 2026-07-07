package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

var errUnsafeWebhookURL = errors.New("unsafe webhook URL")

// WebhookNotifier POSTs alert payloads to a configured HTTP endpoint.
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier returns a notifier that posts to rawURL, or nil when rawURL is empty.
func NewWebhookNotifier(rawURL string) *WebhookNotifier {
	if rawURL == "" {
		return nil
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil || !isSafeWebhookURL(parsedURL) {
		log.Printf("webhook: unsafe URL rejected")
		return nil
	}

	return &WebhookNotifier{
		url:    parsedURL.String(),
		client: newSafeWebhookHTTPClient(),
	}
}

// Send marshals the alert as JSON and POSTs it to the configured webhook URL.
func (w *WebhookNotifier) Send(alert models.Alert) {
	payload, err := json.Marshal(map[string]any{
		"alert":    alert,
		"event":    "alert_fired",
		"fired_at": alert.FiredAt.Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("webhook: marshal error: %v", err)
		return
	}

	resp, err := w.client.Post(w.url, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("webhook: send error: %v", err)
		return
	}
	//nolint:errcheck // deferred cleanup of HTTP response body; close error is not actionable
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("webhook: server returned %d", resp.StatusCode)
	}
}

func newSafeWebhookHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		dialAddress, err := safeWebhookDialAddress(ctx, host, port)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, dialAddress)
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if !isSafeWebhookURL(req.URL) {
				return errUnsafeWebhookURL
			}
			return nil
		},
	}
}

func safeWebhookDialAddress(ctx context.Context, host, port string) (string, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		if !isSafeWebhookIP(addr) {
			return "", fmt.Errorf("%w: %s", errUnsafeWebhookURL, host)
		}
		return net.JoinHostPort(addr.String(), port), nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	for _, resolved := range addrs {
		addr, err := netip.ParseAddr(resolved.IP.String())
		if err != nil || !isSafeWebhookIP(addr) {
			return "", fmt.Errorf("%w: %s resolved to %s", errUnsafeWebhookURL, host, resolved.IP)
		}
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("%w: %s did not resolve", errUnsafeWebhookURL, host)
	}
	return net.JoinHostPort(addrs[0].IP.String(), port), nil
}

func isSafeWebhookURL(candidate *url.URL) bool {
	if candidate == nil || candidate.Scheme != "https" || candidate.Hostname() == "" || candidate.User != nil {
		return false
	}

	host := strings.TrimSuffix(strings.ToLower(candidate.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return isSafeWebhookIP(addr)
	}
	return true
}

func isSafeWebhookIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}

	blockedPrefixes := []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("224.0.0.0/4"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("::/128"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("fc00::/7"),
		netip.MustParsePrefix("fe80::/10"),
		netip.MustParsePrefix("ff00::/8"),
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("2001:2::/48"),
		netip.MustParsePrefix("2001:20::/28"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}
