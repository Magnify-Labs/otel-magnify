package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	loginRateLimitMaxFailures = 5
	loginRateLimitWindow      = time.Minute
)

type loginRateLimiter struct {
	mu       sync.Mutex
	failures map[string]loginFailureWindow
	now      func() time.Time
}

type loginFailureWindow struct {
	count int
	first time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		failures: make(map[string]loginFailureWindow),
		now:      time.Now,
	}
}

func (l *loginRateLimiter) allow(key string) bool {
	if key == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	window, ok := l.failures[key]
	if !ok {
		return true
	}
	now := l.now()
	if now.Sub(window.first) > loginRateLimitWindow {
		delete(l.failures, key)
		return true
	}
	return window.count < loginRateLimitMaxFailures
}

func (l *loginRateLimiter) recordFailure(key string) {
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	window, ok := l.failures[key]
	if !ok || now.Sub(window.first) > loginRateLimitWindow {
		l.failures[key] = loginFailureWindow{count: 1, first: now}
		return
	}
	window.count++
	l.failures[key] = window
}

func (l *loginRateLimiter) reset(key string) {
	if key == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

func loginRateLimitKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
