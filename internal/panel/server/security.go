// Package server implements the Punakawan Panel's loopback-only HTTP
// server: security middleware, static asset serving, and route wiring,
// per punakawan-panel-implementation-plan.md §17 and §21.
package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// loopbackHosts is what Host header values a request is allowed to carry,
// per §17.1: "reject unexpected Host headers." A browser fetch to
// 127.0.0.1:<port> or localhost:<port> sends one of these; anything else
// (a DNS-rebinding attempt, or a request smuggled in from a non-loopback
// interface) is rejected.
var loopbackHosts = map[string]bool{
	"127.0.0.1": true,
	"localhost": true,
	"[::1]":     true,
}

// validateHost rejects a request whose Host header does not name a
// loopback address, regardless of what interface accepted the TCP
// connection - this is the DNS-rebinding defense §17.1 and §29 call for.
func validateHost(host string) error {
	h := host
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		h = hostOnly
	}
	if !loopbackHosts[h] {
		return fmt.Errorf("server: unexpected Host %q", host)
	}
	return nil
}

// validateOrigin rejects a cross-origin request, per §17.1: a page loaded
// from any other origin (including another localhost port) must not be
// able to call this API using the browser's ambient credentials. A
// missing Origin header (same-origin navigations, curl, non-browser
// clients) is allowed through - only a *mismatching* Origin is rejected.
func validateOrigin(origin, host string) error {
	if origin == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(origin, "http://"), "/")
	trimmed = strings.TrimSuffix(strings.TrimPrefix(trimmed, "https://"), "/")
	if h, _, err := net.SplitHostPort(host); err == nil {
		if oh, _, err := net.SplitHostPort(trimmed); err == nil {
			trimmed = oh
			host = h
		}
	}
	if !loopbackHosts[trimmed] {
		return fmt.Errorf("server: unexpected Origin %q", origin)
	}
	return nil
}

// securityMiddleware enforces §17.1's network boundary (Host/Origin
// validation) and §17.1's response headers on every request.
func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := validateHost(r.Host); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := validateOrigin(r.Header.Get("Origin"), r.Host); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}

		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")

		next.ServeHTTP(w, r)
	})
}

// loopbackListener resolves host to a loopback-only bind address, per
// §17.1: "reject non-loopback binding unless an explicit future feature
// enables it." host is expected to be "127.0.0.1", "localhost", or "::1";
// anything else is rejected rather than silently binding somewhere else.
func loopbackListener(host, port string) (net.Listener, error) {
	resolved := host
	if resolved == "" {
		resolved = "127.0.0.1"
	}
	ip := net.ParseIP(resolved)
	isLoopback := resolved == "localhost" || (ip != nil && ip.IsLoopback())
	if !isLoopback {
		return nil, fmt.Errorf("server: refusing non-loopback bind address %q", host)
	}
	return net.Listen("tcp", net.JoinHostPort(resolved, port))
}
