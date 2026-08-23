// Package server implements the Punakawan Panel's loopback-only HTTP
// server: security middleware, static asset serving, and route wiring,
// per punakawan-panel-implementation-plan.md §17 and §21.
package server

import (
	"net"
	"net/http"

	"github.com/ygrip/punakawan/internal/loopback"
)

// validateHost rejects a request whose Host header does not name a
// loopback address, defending against DNS rebinding. See internal/loopback,
// shared with the daemon transport.
func validateHost(host string) error { return loopback.ValidateHost(host) }

// validateOrigin rejects a cross-origin request, per §17.1: a page
// loaded from any other origin (including another localhost port) must
// not be able to call this API using the browser's ambient credentials.
func validateOrigin(origin, host string) error { return loopback.ValidateOrigin(origin, host) }

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
// enables it."
func loopbackListener(host, port string) (net.Listener, error) {
	return loopback.Listener(host, port)
}
