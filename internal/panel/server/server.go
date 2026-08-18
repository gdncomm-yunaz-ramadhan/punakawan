package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/mcpserver"
	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/api"
	"github.com/ygrip/punakawan/internal/panel/deliverysource"
	"github.com/ygrip/punakawan/internal/panel/events"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/session"
	"github.com/ygrip/punakawan/internal/panel/sources"
	"github.com/ygrip/punakawan/internal/panel/timing"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/internal/workcontext"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
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

	hub                *events.Hub
	stopReconciliation context.CancelFunc
	reconcileDone      chan struct{}
	stopDeliveryWatch  context.CancelFunc
	deliveryWatchDone  chan struct{}
	stopRuntimeSweep   context.CancelFunc

	// shutdownCtx/cancelShutdown are the server's shutdown signal, separate
	// from any one request's context: it is cancelled once, at the very start
	// of Shutdown, so every long-lived handler (currently just the SSE
	// stream) can stop waiting on its own client and return immediately
	// instead of holding httpServer.Shutdown's connection-draining open.
	shutdownCtx    context.Context
	cancelShutdown context.CancelFunc

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
	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	return &Server{
		app:            a,
		registry:       reg,
		readers:        readers,
		opts:           opts,
		logger:         logger,
		hub:            events.NewHub(),
		sessions:       session.NewManager(),
		shutdownCtx:    shutdownCtx,
		cancelShutdown: cancelShutdown,
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
	// Recipes is resolved lazily (see ArtifactStores' own doc comment):
	// opening the knowledge store starts an external Dolt server process,
	// which every plan-only request (the overwhelming majority) should
	// never pay for. App.OpenKnowledge memoizes its own result, so this
	// closure only actually starts Dolt once, on the first
	// retrieval_recipe-typed request this server instance receives.
	recipesFactory := func() (*recipe.RecipeStore, error) {
		knowledgeStore, err := s.app.OpenKnowledge()
		if err != nil {
			return nil, err
		}
		return &recipe.RecipeStore{Repo: &recipe.Repository{Store: knowledgeStore}}, nil
	}
	mux.HandleFunc("GET /api/v1/system", api.SystemHandler(cfg, s.registry))
	mux.HandleFunc("GET /api/v1/system/settings", api.GetPanelSettingsHandler(s.app.Workspace.Root, s.readers.Runtime))
	mux.HandleFunc("PATCH /api/v1/system/settings", session.RequireSession(s.sessions, api.UpdatePanelSettingsHandler(s.app.Workspace.Root, s.readers.Runtime)))
	mux.HandleFunc("GET /api/v1/overview", api.OverviewHandler(s.readers, s.app.Workspace.ID))
	mux.HandleFunc("GET /api/v1/events", events.SSEHandler(s.hub, s.shutdownCtx))
	mux.HandleFunc("GET /api/v1/search", api.GlobalSearchHandler(s.readers.GlobalSearch))
	mux.HandleFunc("GET /api/v1/workspaces/{workspaceId}/approvals", api.ApprovalsHandler(s.readers.Approval))

	// Delivery orchestrations: served straight from the daemon's own
	// delivery.Store over its authenticated loopback transport
	// (internal/daemon/delivery.go), not through storage.Open/*app.App -
	// s.readers.Delivery is nil when no daemon connection was available at
	// startup, and every handler here degrades to 503 rather than panicking.
	mux.HandleFunc("GET /api/v1/deliveries", api.ListDeliveriesHandler(s.readers.Delivery))
	mux.HandleFunc("GET /api/v1/deliveries/{orchestrationId}", api.DeliveryViewHandler(s.readers.Delivery))
	mux.HandleFunc("GET /api/v1/deliveries/{orchestrationId}/evidence/{evidenceId}", api.DeliveryEvidenceHandler(s.readers.Delivery))
	mux.HandleFunc("POST /api/v1/deliveries/{orchestrationId}/answer-question", session.RequireSession(s.sessions, api.AnswerDeliveryQuestionHandler(s.readers.Delivery)))
	mux.HandleFunc("POST /api/v1/deliveries/{orchestrationId}/approve", session.RequireSession(s.sessions, api.ApproveProjectDeliveryHandler(s.readers.Delivery)))
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

	// Project-scoped plans (Phase 7), workflow definitions (Phase 6), and
	// cached health (Phase 8). All resolve a {projectId} to its workspace
	// root through the registry, falling back to the primary workspace so it
	// stays reachable even before it is registered.
	projectStores := artifact.NewProjectStores(s.resolveRoot)
	mux.HandleFunc("GET /api/v1/projects/{projectId}/plans", api.ListPlansHandler(projectStores))
	mux.HandleFunc("GET /api/v1/projects/{projectId}/plans/{planId}", api.PlanHandler(projectStores))

	// The capability set a workflow definition is validated against is derived
	// from the MCP server's actual tool registration (agent-context plan §4.3),
	// not a hand-maintained mirror — so the two can no longer drift. Adapter
	// operations are contributed to the registry when adapters load (a later
	// phase); MCP tool names are the source that had actually drifted.
	caps := workflowdef.NewCapabilitySet(mcpserver.CapabilityRegistry(s.app).Names(), nil)
	// Invoke validates enabled + capabilities in workflowdef, then this
	// RunCreator binds the accepted definition to the run engine by creating a
	// WorkflowRun. A definition id is not one of the fixed WorkflowRun name
	// enums, so the run is created under the generic "implementation-only"
	// carrier with its Objective set to the originating definition id (that is
	// how a run traces back to the definition that spawned it). The primary
	// project uses its long-lived run store directly; a non-primary project is
	// Acquire'd from the runtime pool (punokawan-hbm) so its own
	// .punakawan/workflow/runs.jsonl receives the run, then released.
	newInvoker := func(projectID, root string) workflowdef.Invoker {
		return workflowdef.NewInvoker(caps, func(ctx context.Context, def workflowdef.Definition, inputs map[string]any) (string, error) {
			now := time.Now().UTC()
			// Compose the bounded context through the shared workcontext
			// service — the same path prepare_work_context uses — so the panel
			// invoke route and the MCP tool cannot diverge (agent-context plan
			// §5.2). No retrieval query is passed here, so no knowledge store is
			// opened; this validates+defaults inputs, resolves required metadata
			// (missing → awaiting-clarification), and builds the snapshot+digest.
			prepared, err := workcontext.Prepare(workcontext.Request{
				WorkspaceRoot: root,
				Definitions:   []workflowdef.Definition{def},
				WorkflowID:    def.ID,
				Inputs:        inputs,
				Now:           now,
			}, nil, nil)
			if err != nil {
				return "", err
			}
			defRef := &protocol.WorkflowRunDefinitionRef{Id: def.ID, Revision: def.Revision, ContentHash: def.ContentHash()}
			stepProgress := prepared.StepProgress
			snapshot := prepared.Snapshot
			createRun := func(a *app.App) (string, error) {
				runID := fmt.Sprintf("pkw:run/%s/%s-%d", a.Workspace.ID, def.ID, now.UnixNano())
				// A definition id is not one of the fixed WorkflowRun name enums,
				// so the run is created under the generic "implementation-only"
				// carrier; the binding to the originating definition is now the
				// immutable definition_ref (id/revision/content_hash), NOT a
				// magic prefix parsed back out of Objective.
				run := workflow.New(runID, a.Workspace.ID, protocol.WorkflowRunWorkflowNameImplementationOnly, now)
				objective := def.Name
				run.Objective = &objective
				run, err := workflow.StampContext(run, defRef, prepared.ResolvedInputs, stepProgress, &snapshot, now)
				if err != nil {
					return "", fmt.Errorf("stamp context onto run for definition %q: %w", def.ID, err)
				}
				if err := a.Workflow.Append(run); err != nil {
					return "", fmt.Errorf("create workflow run for definition %q: %w", def.ID, err)
				}
				return runID, nil
			}
			if root == s.app.Workspace.Root {
				return createRun(s.app)
			}
			if s.readers.Runtime == nil {
				return "", fmt.Errorf("workflow invocation for non-primary project %q is unavailable: no runtime pool", projectID)
			}
			rt, release, err := s.readers.Runtime.Acquire(ctx, projectID, root)
			if err != nil {
				return "", fmt.Errorf("acquire project %q for workflow invocation: %w", projectID, err)
			}
			defer release()
			return createRun(rt.App)
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

	// Project-scoped Tasks / Sessions / Evidence / Knowledge reads. These
	// resolve the backing *app.App per project id through the runtime pool
	// (the primary is used directly), so a project's Tasks/Knowledge/Sessions
	// tabs (including a session's evidence) work for any registered project,
	// not only the startup workspace. The {workspaceId} path-value name is
	// intentional: it lets the existing workspace-scoped handlers be reused
	// verbatim over a project-aware reader.
	projResolver := &sources.AppResolver{
		PrimaryID: s.app.Workspace.ID,
		Primary:   s.app,
		Runtime:   s.readers.Runtime,
		Resolve:   s.resolveRoot,
	}
	projTasks := sources.ProjectTaskReader{AppResolver: projResolver}
	projSessions := sources.ProjectSessionReader{AppResolver: projResolver}
	projKnowledge := sources.ProjectKnowledgeReader{AppResolver: projResolver}
	projApprovals := sources.ProjectApprovalReader{AppResolver: projResolver}
	projEvidence := sources.ProjectEvidenceReader{AppResolver: projResolver}
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/approvals", api.ApprovalsHandler(projApprovals))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/tasks", api.TasksHandler(projTasks))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/tasks/{taskId}", api.TaskHandler(projTasks))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/task-graph", api.TaskGraphHandler(projTasks))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/sessions", api.SessionsHandler(projSessions))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/sessions/{sessionId}", api.SessionHandler(projSessions))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/sessions/{sessionId}/evidence", api.EvidenceListHandler(projEvidence))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/evidence/{evidenceId}", api.EvidenceHandler(projEvidence))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/evidence/{evidenceId}/preview", api.EvidencePreviewHandler(projEvidence))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/knowledge", api.KnowledgeListHandler(projKnowledge))
	mux.HandleFunc("GET /api/v1/projects/{workspaceId}/knowledge/{knowledgeRest...}", api.KnowledgeDetailHandler(projKnowledge))

	mux.HandleFunc("POST /api/v1/session/exchange", session.ExchangeHandler(s.sessions))

	// Project-scoped Context Improvements inbox (agent-context plan §8): the
	// artifact review/proposal protocol it used to be part of is gone, but this
	// endpoint still resolves per-project Plan/Learning stores via the same
	// artifact.ProjectStores resolver used by /projects/.../plans.
	projectArtifacts := api.NewProjectArtifactStores(
		projectStores,
		recipesFactory,
		s.app.OpenKnowledge,
		s.app.OpenLearning,
	)
	pa := "/api/v1/projects/{projectId}"
	mux.HandleFunc("GET "+pa+"/context-improvements", projectArtifacts.ContextImprovements())
	mux.HandleFunc("POST "+pa+"/reviews/{reviewId}/proposals/{proposalId}/accept", session.RequireSession(s.sessions, projectArtifacts.AcceptProposal()))
	mux.HandleFunc("POST "+pa+"/reviews/{reviewId}/proposals/{proposalId}/reject", session.RequireSession(s.sessions, projectArtifacts.RejectProposal()))

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

	// DeliveryWatcher long-polls the daemon per orchestration instead of
	// ticking over every entity like Reconciler's tiers do, so it runs as
	// its own goroutine rather than a fourth Reconciler tier (see its own
	// doc comment). A nil s.readers.Delivery makes Run an immediate no-op.
	deliveryCtx, cancelDelivery := context.WithCancel(context.Background())
	s.stopDeliveryWatch = cancelDelivery
	s.deliveryWatchDone = make(chan struct{})
	watcher := &events.DeliveryWatcher{Hub: s.hub, Reader: s.readers.Delivery}
	go func() {
		defer close(s.deliveryWatchDone)
		watcher.Run(deliveryCtx)
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
	// Fire the shutdown signal first, before anything else: this is what lets
	// an open SSE connection's handler goroutine (events.SSEHandler) notice
	// shutdown has begun and return right away, instead of blocking
	// s.httpServer.Shutdown below on a client that only disconnects on its
	// own initiative (which, for a browser tab left open, may be never).
	s.cancelShutdown()
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
	if s.stopDeliveryWatch != nil {
		s.stopDeliveryWatch()
	}
	if s.deliveryWatchDone != nil {
		select {
		case <-s.deliveryWatchDone:
		case <-ctx.Done():
		}
	}
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
//
// An SSE handler (events.SSEHandler) does not return until the client
// disconnects, so ServeHTTP blocks for the connection's whole lifetime -
// often minutes - rather than the time spent doing work. Logging that
// under the same "panel request"/duration_ms shape as every other route
// makes it read as a multi-minute stall, when nothing was actually slow.
// Detecting the response's Content-Type (set by the handler before this
// middleware ever logs) lets this stay handler-agnostic - it labels any
// streaming response this way, not just today's one SSE route.
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start).Milliseconds()
		if strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
			logger.Info("panel stream closed", "method", r.Method, "path", r.URL.Path, "connection_duration_ms", duration)
			return
		}
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
