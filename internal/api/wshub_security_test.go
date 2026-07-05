package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type wsTestAuth struct {
	expires map[string]time.Time
}

func (a wsTestAuth) GenerateToken(_, _ string, _ []string) (string, error) { return "", nil }

func (a wsTestAuth) ValidateToken(token string) (*ext.UserInfo, error) {
	expiresAt, ok := a.expires[token]
	if !ok {
		return nil, fmt.Errorf("unknown token")
	}
	if !expiresAt.IsZero() && !time.Now().Before(expiresAt) {
		return nil, fmt.Errorf("expired token")
	}
	return &ext.UserInfo{UserID: "u1", Email: "u1@example.com", Groups: []string{"viewer"}}, nil
}

func (a wsTestAuth) TokenExpiresAt(token string) (time.Time, bool, error) {
	expiresAt, ok := a.expires[token]
	if !ok {
		return time.Time{}, false, fmt.Errorf("unknown token")
	}
	return expiresAt, !expiresAt.IsZero(), nil
}

func (a wsTestAuth) Middleware(next http.Handler) http.Handler { return next }

func newWSTestServer(t *testing.T, corsOrigins string, expires map[string]time.Time) (*httptest.Server, *Hub) {
	t.Helper()
	hub := NewHub()
	go hub.Run()
	t.Cleanup(hub.Stop)
	router := NewRouter(nil, wsTestAuth{expires: expires}, hub, nil, nil, corsOrigins, nil, nil, 30*24*time.Hour, nil, nil, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return server, hub
}

func wsURL(serverURL, token string) string {
	u, _ := url.Parse(serverURL)
	u.Scheme = "ws"
	u.Path = "/ws"
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func closeWSHandshakeResponse(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp != nil && resp.Body != nil {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("close handshake response body: %v", err)
		}
	}
}

func dialWS(t *testing.T, serverURL, token, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial(wsURL(serverURL, token), header)
}

func TestWebSocketAcceptsConfiguredCORSOrigin(t *testing.T) {
	server, _ := newWSTestServer(t, "http://app.example.com", map[string]time.Time{
		"valid": time.Now().Add(time.Hour),
	})

	conn, resp, err := dialWS(t, server.URL, "valid", "http://app.example.com")
	defer closeWSHandshakeResponse(t, resp)
	if err != nil {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("dial configured origin failed with %s: %v", status, err)
	}
	defer conn.Close()
}

func TestWebSocketRejectsUnconfiguredCrossOrigin(t *testing.T) {
	server, _ := newWSTestServer(t, "http://app.example.com", map[string]time.Time{
		"valid": time.Now().Add(time.Hour),
	})

	conn, resp, err := dialWS(t, server.URL, "valid", "http://evil.example.com")
	defer closeWSHandshakeResponse(t, resp)
	if err == nil {
		conn.Close()
		t.Fatal("expected cross-origin WebSocket handshake to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("expected 403 for rejected origin, got %s (err=%v)", status, err)
	}
}

func TestWebSocketRejectsMalformedOriginWithPath(t *testing.T) {
	server, _ := newWSTestServer(t, "http://*.example.com", map[string]time.Time{
		"valid": time.Now().Add(time.Hour),
	})

	conn, resp, err := dialWS(t, server.URL, "valid", "http://evil.test/path.example.com")
	defer closeWSHandshakeResponse(t, resp)
	if err == nil {
		conn.Close()
		t.Fatal("expected origin with path to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("expected 403 for malformed origin, got %s (err=%v)", status, err)
	}
}

func TestWebSocketAcceptsSameHostOriginWithoutConfiguredCORSOrigin(t *testing.T) {
	server, _ := newWSTestServer(t, "", map[string]time.Time{
		"valid": time.Now().Add(time.Hour),
	})
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	conn, resp, err := dialWS(t, server.URL, "valid", "http://"+u.Host)
	defer closeWSHandshakeResponse(t, resp)
	if err != nil {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("dial same-host origin failed with %s: %v", status, err)
	}
	defer conn.Close()
}

func TestWebSocketRejectsExpiredToken(t *testing.T) {
	server, _ := newWSTestServer(t, "", map[string]time.Time{
		"expired": time.Now().Add(-time.Minute),
	})

	conn, resp, err := dialWS(t, server.URL, "expired", "")
	defer closeWSHandshakeResponse(t, resp)
	if err == nil {
		conn.Close()
		t.Fatal("expected expired token WebSocket handshake to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("expected 401 for expired token, got %s (err=%v)", status, err)
	}
}

func TestWebSocketClosesWhenTokenExpires(t *testing.T) {
	expiresAt := time.Now().Add(150 * time.Millisecond)
	server, hub := newWSTestServer(t, "", map[string]time.Time{
		"soon-expiring": expiresAt,
	})

	conn, resp, err := dialWS(t, server.URL, "soon-expiring", "")
	defer closeWSHandshakeResponse(t, resp)
	if err != nil {
		status := "no response"
		if resp != nil {
			status = resp.Status
		}
		t.Fatalf("dial near-expiry token failed with %s: %v", status, err)
	}
	defer conn.Close()

	time.Sleep(time.Until(expiresAt) + 100*time.Millisecond)
	hub.BroadcastWorkloadUpdate(models.Workload{ID: "w1", Status: "connected"}, 1, 0)

	if err := conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatal("expected WebSocket to close after token expiration")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatalf("expected close frame/error after token expiration, got read timeout: %v", err)
	}
	if strings.Contains(err.Error(), "alert_update") {
		t.Fatalf("expired connection received broadcast: %v", err)
	}
}
