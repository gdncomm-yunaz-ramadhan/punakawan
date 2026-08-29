package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/mcpserver"
	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/api"
	"github.com/ygrip/punakawan/internal/panel/deliverysource"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/session"
	"github.com/ygrip/punakawan/internal/panel/sources"
	"github.com/ygrip/punakawan/internal/panel/timing"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/workflowdef"
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
	// ServerTiming enables the dev-only per-request Server-Timing response
	// header (performance plan §17). Off by default (production); the
	// PUNAKAWAN_PANEL_SERVER_TIMING=1 environment variable also turns it on
	// (OR'd in when Start builds the middleware chain), so a developer can
	// flip it without a code change.
	ServerTiming bool
	// DaemonClient, when set, is this panel instance's connection to the
	// daemon's delivery data (internal/daemon/delivery.go); nil leaves the
	// delivery routes wired but answering 503, exactly like a project
	// missing the Contradiction/Impact/Dossier subsystems already degrades.
	// Resolving this (daemon.DiscoverDefault or similar) is the caller's
	// job, not New/Start's: a test building a Server should never risk
	// spawning or talking to a real system-wide daemon process just by
	// starting a server under test.
	DaemonClient *daemon.Client
}

// Server is the Punakawan Panel's loopback HTTP server.
type Server struct {
	app       *app.App
	registry  *registry.Store
	readers   panel.Readers
	opts      Options
	logger    *slog.Logger
	startedAt time.Time

	stopRuntimeSweep context.CancelFunc

	sessions     *session.Manager
	bootstrapURL string

	httpServer *http.Server
	listener   net.Listener
}

// New builds a Server for a, without starting it. reg is the global
// workspace registry (New requires a caller to have already opened one,
// per registry.Open).
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
	readers := panel.NewReaders(a, reg)
	if opts.DaemonClient != nil {
		readers.Delivery = &deliverysource.Source{Client: opts.DaemonClient}
	}
	return &Server{
		app:      a,
		registry: reg,
		readers:  readers,
		opts:     opts,
		logger:   logger,
		sessions: session.NewManager(),
	}
}

// resolveRoot maps a project id to the workspace root that backs it, via the
// registry, falling back to the primary workspace this server was loaded for.
// An unknown id yields an error so handlers answer 404 rather than 500.
func (s *Server) resolveRoot(projectID string) (string, error) {
	if s.registry != nil {
		if entry, err := s.registry.Get(projectID); err == nil {
			return entry.Path, nil
		}
	}
	if projectID == s.app.Workspace.ID {
		return s.app.Workspace.Root, nil
	}
	return "", fmt.Errorf("server: project %q is not registered", projectID)
}

// Addr returns the address the server is listening on, valid only after
// Start succeeds.
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// BootstrapURL returns the one-time URL - the bound address plus a fresh
// bootstrap token as a query parameter - that trades for a session on
// first load, valid only after Start succeeds. Per §15, the token is
// single-use: opening this URL a second time (e.g. from browser history)
// will not grant a session, since the first exchange already invalidated
// it.
func (s *Server) BootstrapURL() string {
	return s.bootstrapURL
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

	bootstrapToken, err := s.sessions.IssueBootstrapToken()
	if err != nil {
		return fmt.Errorf("server: issue bootstrap token: %w", err)
	}
	s.bootstrapURL = "http://" + listener.Addr().String() + "/?bootstrap=" + bootstrapToken

	mux := http.NewServeMux()
	cfg := api.Config{
		PunakawanVersion: panel.Version,
		ReadOnly:         false, // §15's mutation session + CSRF layer now gates the write endpoints below
		BoundAddr:        listener.Addr().String(),
		StartedAt:        s.startedAt,
	}
	mux.HandleFunc("GET /api/v1/system", api.SystemHandler(cfg, s.registry))
	mux.HandleFunc("GET /api/v1/system/settings", api.GetPanelSettingsHandler(s.app.Workspace.Root, s.readers.Runtime))
	mux.HandleFunc("PATCH /api/v1/system/settings", session.RequireSession(s.sessions, api.UpdatePanelSettingsHandler(s.app.Workspace.Root, s.readers.Runtime)))

	// Delivery orchestrations: served straight from the daemon's own
	// delivery.Store over its authenticated loopback transport
	// (internal/daemon/delivery.go), not through storage.Open/*app.App -
	// s.readers.Delivery is nil when no daemon connection was available at
	// startup, and every handler here degrades to 503 rather than panicking.
	mux.HandleFunc("GET /api/v1/deliveries", api.ListDeliveriesHandler(s.readers.Delivery))
	mux.HandleFunc("GET /api/v1/deliveries/{orchestrationId}", api.DeliveryViewHandler(s.readers.Delivery))
	mux.HandleFunc("GET /api/v1/deliveries/{orchestrationId}/evidence/{evidenceId}", api.DeliveryEvidenceHandler(s.readers.Delivery))
	mux.HandleFunc("POST /api/v1/deliveries/{orchestrationId}/answer-question", session.RequireSession(s.sessions, api.AnswerDeliveryQuestionHandler(s.readers.Delivery)))
	mux.HandleFunc("POST /api/v1/deliveries/{orchestrationId}/cancel", session.RequireSession(s.sessions, api.CancelDeliveryHandler(s.readers.Delivery)))

	mux.HandleFunc("GET /api/v1/projects", api.ProjectsHandler(s.readers.Project))
	mux.HandleFunc("GET /api/v1/projects/{projectId}", api.ProjectHandler(s.readers.Project))
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}", session.RequireSession(s.sessions, api.ProjectDeleteHandler(s.readers.Project)))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/metadata", api.MetadataListHandler(s.readers.Project))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/metadata", session.RequireSession(s.sessions, api.MetadataCreateHandler(s.readers.Project)))
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}/metadata/{key}", session.RequireSession(s.sessions, api.MetadataUpdateHandler(s.readers.Project)))
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}/metadata/{key}", session.RequireSession(s.sessions, api.MetadataDeleteHandler(s.readers.Project)))

	mux.HandleFunc("GET /api/v1/projects/{projectId}/roles", api.RolesListHandler(s.readers.Roles))
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}/roles/{role}", session.RequireSession(s.sessions, api.RoleUpdateHandler(s.readers.Roles)))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/roles/{role}/reset", session.RequireSession(s.sessions, api.RoleResetHandler(s.readers.Roles)))

	// Project-scoped plans read the same shared SQLite kernel every delivery
	// tool writes through - internal/plan's plan_revisions and
	// internal/delivery's project registry/plan links - rather than a
	// per-workspace filesystem store, so a plan a delivery links stays
	// exact (its own revision, not whatever the lineage moved on to) no
	// matter which project page renders it from.
	planDeliveries, err := s.app.OpenStorage(context.Background())
	if err != nil {
		return fmt.Errorf("server: open storage kernel for project plans: %w", err)
	}
	mux.HandleFunc("GET /api/v1/projects/{projectId}/plans", api.ListPlansHandler(delivery.NewStore(planDeliveries), plan.NewStore(planDeliveries)))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/plans/{planId}", api.PlanHandler(delivery.NewStore(planDeliveries), plan.NewStore(planDeliveries)))

	// The capability set a workflow definition is validated against is derived
	// from the MCP server's actual tool registration (agent-context plan §4.3),
	// not a hand-maintained mirror — so the two can no longer drift. Adapter
	// operations are contributed to the registry when adapters load (a later
	// phase); MCP tool names are the source that had actually drifted.
	caps := workflowdef.NewCapabilitySet(mcpserver.CapabilityRegistry(s.app).Names(), nil)
	// Invoke validates enabled + capabilities in workflowdef, then this
	// RunCreator resolves the right *app.App for root (the primary project's
	// long-lived one, or a non-primary project's Acquire'd from the runtime
	// pool per punokawan-hbm) and hands it to mcpserver.CreateWorkflowRun -
	// the same shape-aware dispatch (Roles present -> delivery,
	// otherwise -> legacy workflow run, either way via workcontext.Prepare
	// against that App's own workspace root) the MCP invoke_workflow_definition
	// tool uses, so the panel and MCP can no longer diverge on a Roles-shaped
	// definition (punokawan-pkcd.3).
	newInvoker := func(projectID, root string) workflowdef.Invoker {
		return workflowdef.NewInvoker(caps, func(ctx context.Context, def workflowdef.Definition, inputs map[string]any) (string, error) {
			if root == s.app.Workspace.Root {
				return mcpserver.CreateWorkflowRun(s.app)(ctx, def, inputs)
			}
			if s.readers.Runtime == nil {
				return "", fmt.Errorf("workflow invocation for non-primary project %q is unavailable: no runtime pool", projectID)
			}
			rt, release, err := s.readers.Runtime.Acquire(ctx, projectID, root)
			if err != nil {
				return "", fmt.Errorf("acquire project %q for workflow invocation: %w", projectID, err)
			}
			defer release()
			return mcpserver.CreateWorkflowRun(rt.App)(ctx, def, inputs)
		})
	}
	wf := api.NewWorkflowDefHandlers(s.resolveRoot, caps, newInvoker)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/workflows", wf.List())
	mux.HandleFunc("POST /api/v1/projects/{projectId}/workflows", session.RequireSession(s.sessions, wf.Create()))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/workflows/{workflowId}", wf.Get())
	mux.HandleFunc("POST /api/v1/projects/{projectId}/workflows/{workflowId}/enable", session.RequireSession(s.sessions, wf.Enable()))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/workflows/{workflowId}/disable", session.RequireSession(s.sessions, wf.Disable()))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/workflows/{workflowId}/invoke", session.RequireSession(s.sessions, wf.Invoke()))

	healthCache := sources.NewHealthCache(s.readers.Workspace, sources.DefaultHealthTTL)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/health", api.HealthHandler(healthCache))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/health/refresh", session.RequireSession(s.sessions, api.HealthRefreshHandler(healthCache)))

	// Project-scoped Knowledge reads resolve the
	// backing *app.App per project id through the runtime pool (the primary
	// is used directly), so Knowledge works for any registered project.
	projResolver := &sources.AppResolver{
		PrimaryID: s.app.Workspace.ID,
		Primary:   s.app,
		Runtime:   s.readers.Runtime,
		Resolve:   s.resolveRoot,
	}
	projKnowledge := sources.ProjectKnowledgeReader{AppResolver: projResolver}
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/knowledge", api.KnowledgeListHandler(projKnowledge))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/knowledge/{knowledgeRest...}", api.KnowledgeDetailHandler(projKnowledge))

	mux.HandleFunc("POST /api/v1/session/exchange", session.ExchangeHandler(s.sessions))

	mux.Handle("/", static)

	// Dev-only Server-Timing: enabled by the Options flag OR the
	// PUNAKAWAN_PANEL_SERVER_TIMING=1 env var. timingMiddleware sits inside
	// security (which owns the Host/Origin gate and response security
	// headers) but outside logging, so the Collector's context reaches every
	// handler and source, and the logged duration still brackets the full
	// handler run.
	serverTiming := s.opts.ServerTiming || os.Getenv("PUNAKAWAN_PANEL_SERVER_TIMING") == "1"
	s.httpServer = &http.Server{
		Handler:           securityMiddleware(timingMiddleware(serverTiming, loggingMiddleware(s.logger, mux))),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("panel server exited", "error", err)
		}
	}()

	// Periodically close project runtimes that have gone idle, so the pool does
	// not hold Dolt/adapter processes open for workspaces no longer being
	// browsed (Phase 3, §10.3). The primary and in-use runtimes are never
	// closed by CloseIdle; Shutdown closes the rest.
	if mgr := s.readers.Runtime; mgr != nil {
		sweepCtx, cancelSweep := context.WithCancel(context.Background())
		s.stopRuntimeSweep = cancelSweep
		go func() {
			t := time.NewTicker(2 * time.Minute)
			defer t.Stop()
			for {
				select {
				case <-sweepCtx.Done():
					return
				case <-t.C:
					if err := mgr.CloseIdle(sweepCtx); err != nil {
						s.logger.Warn("panel runtime idle sweep", "error", err)
					}
				}
			}
		}()
	}

	s.logger.Info("panel server started", "addr", listener.Addr().String())
	return nil
}

// Shutdown gracefully stops the server, per §21's "graceful shutdown":
// in-flight requests are given ctx's deadline to finish rather than being
// dropped. Stopping never modifies canonical workspace state (§30) - this
// server only reads from the stores behind its readers.
func (s *Server) Shutdown(ctx context.Context) error {
	s.sessions.InvalidateAll()
	if s.stopRuntimeSweep != nil {
		s.stopRuntimeSweep()
	}
	if mgr := s.readers.Runtime; mgr != nil {
		if err := mgr.Close(ctx); err != nil {
			s.logger.Warn("panel runtime pool close", "error", err)
		}
	}
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
		duration := time.Since(start).Milliseconds()
		logger.Info("panel request", "method", r.Method, "path", r.URL.Path, "duration_ms", duration)
	})
}

// timingMiddleware, when enabled, attaches a fresh timing.Collector to the
// request context so handlers and sources can probe their sub-reads, then
// writes the accumulated durations back as a Server-Timing response header
// (performance plan §17). When disabled it is a pure pass-through - handlers
// still call timing.Probe unconditionally, but with no Collector in context
// those probes are no-ops.
//
// Header ordering: Server-Timing must be set before the response status line
// and body are written, but a Collector is only fully populated once the
// handler returns. Panel handlers assemble their whole response and call
// WriteHeader/Write exactly once at the end (see writeJSON), so we wrap the
// ResponseWriter and inject the header just-in-time on the first
// WriteHeader/Write call - the moment before the headers are flushed, which
// is also the moment the handler has finished its probed work.
func timingMiddleware(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		collector := timing.NewCollector()
		ctx := timing.WithCollector(r.Context(), collector)
		tw := &timingResponseWriter{ResponseWriter: w, collector: collector}
		next.ServeHTTP(tw, r.WithContext(ctx))
		// Fallback for a handler that never wrote anything (no WriteHeader/
		// Write, so the wrapper's just-in-time hook never fired): set the
		// header now. net/http will still emit an implicit 200 for us.
		tw.injectServerTiming()
	})
}

// timingResponseWriter defers the Server-Timing header until the first
// WriteHeader or Write, when the handler's probed work is complete but the
// headers have not yet been flushed.
type timingResponseWriter struct {
	http.ResponseWriter
	collector *timing.Collector
	injected  bool
}

// injectServerTiming sets the Server-Timing header from the collector exactly
// once, and only while the headers are still mutable (before the first real
// WriteHeader). A no-op if nothing was recorded, so unprobed requests carry no
// empty header.
func (w *timingResponseWriter) injectServerTiming() {
	if w.injected {
		return
	}
	w.injected = true
	if v := w.collector.ServerTiming(); v != "" {
		w.ResponseWriter.Header().Set("Server-Timing", v)
	}
}

func (w *timingResponseWriter) WriteHeader(status int) {
	w.injectServerTiming()
	w.ResponseWriter.WriteHeader(status)
}

func (w *timingResponseWriter) Write(b []byte) (int, error) {
	w.injectServerTiming()
	return w.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it supports flushing.
func (w *timingResponseWriter) Flush() {
	w.injectServerTiming()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
