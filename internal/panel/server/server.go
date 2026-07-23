package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/api"
	"github.com/ygrip/punakawan/internal/panel/registry"
)

// Options configures a Server, per §26's configuration keys this phase
// actually wires (host/port/read_only): cache TTLs, watcher, and other
// §26 keys belong to later phases.
type Options struct {
	// Host defaults to 127.0.0.1. loopbackListener rejects anything that
	// does not resolve to a loopback address, per §17.1.
	Host string
	// Port defaults to "0" (OS-assigned; useful for tests). §26's example
	// default is 7331.
	Port   string
	Logger *slog.Logger
}

// Server is the Punakawan Panel's loopback HTTP server.
type Server struct {
	app       *app.App
	registry  *registry.Store
	readers   panel.Readers
	opts      Options
	logger    *slog.Logger
	startedAt time.Time

	httpServer *http.Server
	listener   net.Listener
}

// New builds a Server for a, without starting it. reg is the global
// workspace registry (New requires a caller to have already opened one,
// per registry.Open/OpenAt).
func New(a *app.App, reg *registry.Store, opts Options) *Server {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == "" {
		opts.Port = "0"
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		app:      a,
		registry: reg,
		readers:  panel.NewReaders(a),
		opts:     opts,
		logger:   logger,
	}
}

// Addr returns the address the server is listening on, valid only after
// Start succeeds.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Start binds the loopback listener and begins serving in the
// background. Call Shutdown to stop it.
func (s *Server) Start() error {
	listener, err := loopbackListener(s.opts.Host, s.opts.Port)
	if err != nil {
		return err
	}
	s.listener = listener
	s.startedAt = time.Now().UTC()

	static, err := staticHandler()
	if err != nil {
		return fmt.Errorf("server: static assets: %w", err)
	}

	mux := http.NewServeMux()
	cfg := api.Config{
		PunakawanVersion: panel.Version,
		ReadOnly:         true, // §17.1's read-only MVP: no mutation endpoints exist yet
		BoundAddr:        listener.Addr().String(),
		StartedAt:        s.startedAt,
	}
	mux.HandleFunc("GET /api/v1/system", api.SystemHandler(cfg, s.registry))
	mux.HandleFunc("GET /api/v1/overview", api.OverviewHandler(s.readers, s.app.Workspace.ID))
	mux.HandleFunc("GET /api/v1/workspaces", api.WorkspacesHandler(s.readers.Workspace))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}", api.WorkspaceHandler(s.readers.Workspace))
	mux.Handle("/", static)

	s.httpServer = &http.Server{
		Handler:           securityMiddleware(loggingMiddleware(s.logger, mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("panel server exited", "error", err)
		}
	}()

	s.logger.Info("panel server started", "addr", listener.Addr().String())
	return nil
}

// Shutdown gracefully stops the server, per §21's "graceful shutdown":
// in-flight requests are given ctx's deadline to finish rather than being
// dropped. Stopping never modifies canonical workspace state (§30) - this
// server only reads from the stores behind its readers.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	s.logger.Info("panel server shutting down")
	return s.httpServer.Shutdown(ctx)
}

// loggingMiddleware writes one structured log line per request, per
// §27's observability expectations, without logging request bodies or
// headers that might carry secrets.
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("panel request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
	})
}
