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
	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/api"
	"github.com/ygrip/punakawan/internal/panel/events"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/session"
	"github.com/ygrip/punakawan/internal/panel/sources"
	"github.com/ygrip/punakawan/internal/panel/timing"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/internal/revision"
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
}

// Server is the Punakawan Panel's loopback HTTP server.
type Server struct {
	app       *app.App
	registry  *registry.Store
	readers   panel.Readers
	opts      Options
	logger    *slog.Logger
	startedAt time.Time

	hub                *events.Hub
	stopReconciliation context.CancelFunc
	reconcileDone      chan struct{}

	sessions     *session.Manager
	bootstrapURL string

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
		readers:  panel.NewReaders(a, reg),
		opts:     opts,
		logger:   logger,
		hub:      events.NewHub(),
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
	plans := &artifact.PlanStore{WorkspaceRoot: s.app.Workspace.Root}
	reviews := &artifact.ReviewStore{WorkspaceRoot: s.app.Workspace.Root}
	dispatcher := &revision.BDDispatcher{Supervisor: s.app.Supervisor, WorkspaceRoot: s.app.Workspace.Root}

	// Recipes is resolved lazily (see ArtifactStores' own doc comment):
	// opening the knowledge store starts an external Dolt server process,
	// which every plan-only request (the overwhelming majority) should
	// never pay for. App.OpenKnowledge memoizes its own result, so this
	// closure only actually starts Dolt once, on the first
	// retrieval_recipe-typed request this server instance receives.
	stores := api.ArtifactStores{
		Plans: plans,
		Recipes: func() (*recipe.RecipeStore, error) {
			knowledgeStore, err := s.app.OpenKnowledge()
			if err != nil {
				return nil, err
			}
			return &recipe.RecipeStore{Repo: &recipe.Repository{Store: knowledgeStore}}, nil
		},
	}
	mux.HandleFunc("GET /api/v1/system", api.SystemHandler(cfg, s.registry))
	mux.HandleFunc("GET /api/v1/overview", api.OverviewHandler(s.readers, s.app.Workspace.ID))
	mux.HandleFunc("GET /api/v1/events", events.SSEHandler(s.hub))
	mux.HandleFunc("GET /api/v1/workspaces", api.WorkspacesHandler(s.readers.Workspace))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}", api.WorkspaceHandler(s.readers.Workspace))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/sessions", api.SessionsHandler(s.readers.Session))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/sessions/{sessionId}", api.SessionHandler(s.readers.Session))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/capsules", api.CapsulesHandler(s.app.Capsules))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/tasks", api.TasksHandler(s.readers.Task))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/tasks/{taskId}", api.TaskHandler(s.readers.Task))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/task-graph", api.TaskGraphHandler(s.readers.Task))
	mux.HandleFunc("GET /api/v1/search", api.GlobalSearchHandler(s.readers.GlobalSearch))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/knowledge", api.KnowledgeListHandler(s.readers.Knowledge))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/knowledge/{knowledgeRest...}", api.KnowledgeDetailHandler(s.readers.Knowledge))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/sessions/{sessionId}/evidence", api.EvidenceListHandler(s.readers.Evidence))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/evidence/{evidenceId}", api.EvidenceHandler(s.readers.Evidence))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/evidence/{evidenceId}/preview", api.EvidencePreviewHandler(s.readers.Evidence))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/approvals", api.ApprovalsHandler(s.readers.Approval))

	mux.HandleFunc("GET /api/v1/projects", api.ProjectsHandler(s.readers.Project))
	mux.HandleFunc("GET /api/v1/projects/{projectId}", api.ProjectHandler(s.readers.Project))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/metadata", api.MetadataListHandler(s.readers.Project))
	mux.HandleFunc("POST /api/v1/projects/{projectId}/metadata", session.RequireSession(s.sessions, api.MetadataCreateHandler(s.readers.Project)))
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}/metadata/{key}", session.RequireSession(s.sessions, api.MetadataUpdateHandler(s.readers.Project)))
	mux.HandleFunc("DELETE /api/v1/projects/{projectId}/metadata/{key}", session.RequireSession(s.sessions, api.MetadataDeleteHandler(s.readers.Project)))

	// Project-scoped plans (Phase 7), workflow definitions (Phase 6), and
	// cached health (Phase 8). All resolve a {projectId} to its workspace
	// root through the registry, falling back to the primary workspace so it
	// stays reachable even before it is registered.
	projectStores := artifact.NewProjectStores(s.resolveRoot)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/plans", api.ListPlansHandler(projectStores))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/plans/{planId}", api.PlanHandler(projectStores))

	caps := workflowdef.NewCapabilitySet(workflowdef.KnownMCPCapabilities(), nil)
	// Invocation is validated (enabled + capabilities) here, but binding a
	// definition to the run engine is deferred (punokawan follow-up): a
	// workflow definition id is not one of the fixed WorkflowRun name enums,
	// so the RunCreator returns a descriptive error until that linkage lands.
	newInvoker := func(root string) workflowdef.Invoker {
		return workflowdef.NewInvoker(caps, func(context.Context, workflowdef.Definition, map[string]any) (string, error) {
			return "", fmt.Errorf("workflow invocation is not yet connected to the run engine")
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

	mux.HandleFunc("POST /api/v1/session/exchange", session.ExchangeHandler(s.sessions))

	mux.HandleFunc("GET /api/v1/artifacts/{type}/{id}/current", api.ArtifactCurrentHandler(stores))
	mux.HandleFunc("POST /api/v1/artifacts/{type}/{id}/reviews", session.RequireSession(s.sessions, api.CreateReviewHandler(stores, reviews, s.app.Workspace.ID)))
	mux.HandleFunc("GET /api/v1/reviews/{reviewId}", api.ReviewHandler(reviews))
	mux.HandleFunc("PATCH /api/v1/reviews/{reviewId}", session.RequireSession(s.sessions, api.UpdateReviewHandler(reviews)))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/comments", session.RequireSession(s.sessions, api.CreateCommentHandler(reviews, stores)))
	mux.HandleFunc("GET /api/v1/reviews/{reviewId}/comments", api.CommentsHandler(reviews))
	mux.HandleFunc("PATCH /api/v1/reviews/{reviewId}/comments/{commentId}", session.RequireSession(s.sessions, api.UpdateCommentHandler(reviews)))
	mux.HandleFunc("DELETE /api/v1/reviews/{reviewId}/comments/{commentId}", session.RequireSession(s.sessions, api.DeleteCommentHandler(reviews)))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/submit", session.RequireSession(s.sessions, api.SubmitHandler(reviews, dispatcher)))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/cancel", session.RequireSession(s.sessions, api.CancelHandler(reviews)))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/fail", session.RequireSession(s.sessions, api.FailHandler(reviews, s.logger)))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/rebase", session.RequireSession(s.sessions, api.RebaseHandler(reviews, stores)))
	mux.HandleFunc("GET /api/v1/reviews/{reviewId}/timeline", api.TimelineHandler(reviews))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/proposals", session.RequireSession(s.sessions, api.CreateProposalHandler(reviews, stores)))
	mux.HandleFunc("GET /api/v1/reviews/{reviewId}/proposals", api.ListProposalsHandler(reviews))
	mux.HandleFunc("GET /api/v1/reviews/{reviewId}/proposals/{proposalId}", api.ProposalHandler(reviews))
	mux.HandleFunc("GET /api/v1/reviews/{reviewId}/proposals/{proposalId}/diff", api.ProposalDiffHandler(reviews, stores))
	mux.HandleFunc("GET /api/v1/reviews/{reviewId}/proposals/{proposalId}/validation", api.ProposalValidationHandler(reviews, stores))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/accept", session.RequireSession(s.sessions, api.AcceptProposalHandler(reviews, stores)))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/reject", session.RequireSession(s.sessions, api.RejectProposalHandler(reviews)))
	mux.HandleFunc("POST /api/v1/reviews/{reviewId}/proposals/{proposalId}/request-changes", session.RequireSession(s.sessions, api.RequestChangesHandler(reviews, dispatcher)))

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

	reconcileCtx, cancel := context.WithCancel(context.Background())
	s.stopReconciliation = cancel
	s.reconcileDone = make(chan struct{})
	reconciler := &events.Reconciler{Hub: s.hub, Readers: s.readers, WorkspaceID: s.app.Workspace.ID}
	go func() {
		defer close(s.reconcileDone)
		reconciler.Run(reconcileCtx)
	}()

	s.logger.Info("panel server started", "addr", listener.Addr().String())
	return nil
}

// Shutdown gracefully stops the server, per §21's "graceful shutdown":
// in-flight requests are given ctx's deadline to finish rather than being
// dropped. Stopping never modifies canonical workspace state (§30) - this
// server only reads from the stores behind its readers.
//
// It also waits (bounded by ctx) for the background reconciler goroutine
// started in Start to actually exit, not just for its context to be
// cancelled. Cancelling reconcileCtx alone does not stop reconcileOnce
// synchronously - without this wait, the goroutine can still be mid-poll
// (or start one more poll) after Shutdown returns and after the caller
// proceeds to close/dispose of App, which raced with test cleanup: a poll
// that calls a.OpenKnowledge() after App.Close has already run and nil'd
// out its memoized store would silently start a brand new, un-tracked Dolt
// sql-server process writing into a directory the caller believes is now
// quiescent (root cause of punokawan-q9r.6.1's flaky TempDir cleanup).
func (s *Server) Shutdown(ctx context.Context) error {
	s.sessions.InvalidateAll()
	if s.stopReconciliation != nil {
		s.stopReconciliation()
	}
	if s.reconcileDone != nil {
		select {
		case <-s.reconcileDone:
		case <-ctx.Done():
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
		logger.Info("panel request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(start).Milliseconds())
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
// is also the moment the handler has finished its probed work. A handler that
// streams (e.g. the SSE endpoint) would flush headers early and thus capture
// only the timings recorded so far; that is acceptable for a dev-only
// diagnostic and those endpoints are not the warm-read paths §18 targets.
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

// Flush forwards to the underlying writer when it supports flushing (the SSE
// endpoint relies on it), injecting the header first so streamed responses
// still carry whatever timings were recorded before the first flush.
func (w *timingResponseWriter) Flush() {
	w.injectServerTiming()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
