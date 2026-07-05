package httputil

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"golang.org/x/net/http2"
)

// SetSecurityHeaders applies standard security headers to all responses.
func SetSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
}

// WriteError sends an OpenAI-compatible error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	SetSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errorTypeForStatus(status),
			"code":    http.StatusText(status),
		},
	})
}

// errorTypeForStatus maps HTTP status codes to OpenAI-compatible error types.
func errorTypeForStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "authentication_error"
	case status == http.StatusForbidden:
		return "permission_error"
	case status == http.StatusNotFound:
		return "not_found_error"
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= 500:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

// WriteAnthropicError sends an Anthropic-compatible error response.
// Claude Code expects this format for all Messages API errors.
func WriteAnthropicError(w http.ResponseWriter, status int, errType, message string) {
	SetSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": message,
		},
	})
}

// RecoveryMiddleware catches panics in handlers and returns a generic 500 error.
// The stack trace is logged server-side but never exposed to the client.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered",
					"error", err,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				WriteError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// NewHTTPClient returns the standard HTTP client used for upstream calls.
//
// Design decisions:
//
//  1. **No redirect following.** A compromised or misconfigured backend
//     could 3xx the proxy into an internal address; we refuse redirects
//     outright and let the caller decide.
//
//  2. **No ResponseHeaderTimeout.** Thinking models (e.g. glm-5.2,
//     deepseek-v4) can spend 60-120+ seconds in the thinking phase before
//     sending the first response header. A fixed transport-level header
//     timeout races with the per-request context timeout and kills
//     legitimate long-thinking requests. The context timeout (set per
//     model via the `timeout` config field) is the sole authority.
//
//  3. **HTTP/2 PING keepalive.** Without explicit PING frames, idle HTTP/2
//     connections to external backends (Ollama Cloud, etc.) can be silently
//     dropped by intermediate NAT/firewall devices during long thinking
//     phases. The 30s PING interval keeps the connection alive without
//     adding per-request latency. This is the key fix for the "direct
//     connection works but proxy disconnects" issue.
//
//  4. **Bounded idle-connection pool.** Prevents unbounded growth of
//     keepalive sockets against a single upstream. Stale connections are
//     reaped after 60s idle — shorter than the 90s default to reduce
//     reuse of potentially-broken connections.
func NewHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		ForceAttemptHTTP2:     true,
	}
	// Configure HTTP/2 PING keepalive: send a PING frame every 30s on idle
	// connections to detect dead connections and prevent intermediate devices
	// (NAT, firewalls, Tailscale) from silently dropping the TCP connection
	// during long thinking phases. This is the key fix for the "direct
	// connection works but proxy disconnects" issue.
	t2, err := http2.ConfigureTransports(transport)
	if err != nil {
		slog.Warn("failed to configure HTTP/2 PING keepalive", "error", err)
	} else {
		t2.ReadIdleTimeout = 30 * time.Second
		t2.PingTimeout = 15 * time.Second
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
