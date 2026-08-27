package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/loopback"
)

// DefaultPort is the daemon's canonical loopback port. It sits adjacent
// to the Panel's documented default of 7331 (cmd/punakawan/panel_cmd.go).
const DefaultPort = "7330"

// shutdownGrace bounds how long Transport.Shutdown waits for in-flight
// requests to finish before the caller should move on to releasing the
// singleton lock and closing storage.
const shutdownGrace = 5 * time.Second

// Transport is the daemon's authenticated loopback HTTP server. Every
// route except /healthz requires the bearer token minted by
// LoadOrCreateToken - an unauthenticated request never reaches
// application logic.
type Transport struct {
	listener net.Listener
	http     *http.Server
	token    string
	ready    func() error
	delivery *delivery.Store
}

// NewTransport binds a loopback listener at host:port and wires the
// health/readiness routes plus the delivery data-serving routes over
// deliveryStore. ready is called on every /readyz request and should
// report whether the daemon can currently serve real work (e.g. storage
// opened); a nil ready always reports ready.
func NewTransport(host, port, token string, ready func() error, deliveryStore *delivery.Store) (*Transport, error) {
	listener, err := loopback.Listener(host, port)
	if err != nil {
		return nil, fmt.Errorf("daemon: bind transport: %w", err)
	}
	if ready == nil {
		ready = func() error { return nil }
	}
	t := &Transport{listener: listener, token: token, ready: ready, delivery: deliveryStore}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", t.handleHealth)
	mux.Handle("/readyz", t.authenticate(http.HandlerFunc(t.handleReady)))
	mux.Handle("GET /api/v1/deliveries", t.authenticate(http.HandlerFunc(t.handleListDeliveries)))
	mux.Handle("GET /api/v1/deliveries/{orchestrationId}", t.authenticate(http.HandlerFunc(t.handleDeliveryView)))
	mux.Handle("GET /api/v1/deliveries/{orchestrationId}/evidence/{evidenceId}", t.authenticate(http.HandlerFunc(t.handleDeliveryEvidence)))
	mux.Handle("POST /api/v1/deliveries/{orchestrationId}/answer-question", t.authenticate(http.HandlerFunc(t.handleAnswerDeliveryQuestion)))
	mux.Handle("POST /api/v1/deliveries/{orchestrationId}/cancel", t.authenticate(http.HandlerFunc(t.handleCancelDelivery)))
	t.http = &http.Server{Handler: t.securityHeaders(mux)}
	return t, nil
}

// Addr returns the bound loopback address, including the OS-assigned
// port when the transport was created with port "0" - callers that need
// to publish a discoverable endpoint read this after NewTransport
// rather than assuming DefaultPort was actually available.
func (t *Transport) Addr() string { return t.listener.Addr().String() }

// Serve blocks, accepting connections until Shutdown is called. It
// returns http.ErrServerClosed (not an error the caller should report)
// on a normal shutdown.
func (t *Transport) Serve() error {
	return t.http.Serve(t.listener)
}

// Shutdown gracefully stops the transport, waiting up to shutdownGrace
// for in-flight requests before returning.
func (t *Transport) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, shutdownGrace)
	defer cancel()
	return t.http.Shutdown(ctx)
}

// authenticate enforces the bearer token on every route it wraps, using
// a constant-time comparison so response timing cannot be used to guess
// the token byte-by-byte.
func (t *Transport) authenticate(next http.Handler) http.Handler {
	want := []byte("Bearer " + t.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders applies the same loopback Host/Origin validation and
// response headers the Panel's transport uses (internal/loopback),
// ahead of authentication - an unrecognized Host is rejected before the
// request even reaches the token check.
func (t *Transport) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := loopback.ValidateHost(r.Host); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		if err := loopback.ValidateOrigin(r.Header.Get("Origin"), r.Host); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// handleHealth is intentionally unauthenticated (a client must be able
// to tell "something is listening" before it has a token) and reveals
// nothing beyond bare liveness.
func (t *Transport) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

// handleReady requires authentication and reflects whether the daemon
// considers itself ready to serve real work.
func (t *Transport) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := t.ready(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not-ready", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
