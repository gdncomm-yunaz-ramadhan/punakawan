package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/panel"
	"github.com/ygrip/punakawan/internal/panel/api"
	"github.com/ygrip/punakawan/internal/panel/events"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/session"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/internal/revision"
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

	hub                *events.Hub
	stopReconciliation context.CancelFunc

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

	s.httpServer = &http.Server{
		Handler:           securityMiddleware(loggingMiddleware(s.logger, mux)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := s.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			s.logger.Error("panel server exited", "error", err)
		}
	}()

	reconcileCtx, cancel := context.WithCancel(context.Background())
	s.stopReconciliation = cancel
	reconciler := &events.Reconciler{Hub: s.hub, Readers: s.readers, WorkspaceID: s.app.Workspace.ID}
	go reconciler.Run(reconcileCtx)

	s.logger.Info("panel server started", "addr", listener.Addr().String())
	return nil
}

// Shutdown gracefully stops the server, per §21's "graceful shutdown":
// in-flight requests are given ctx's deadline to finish rather than being
// dropped. Stopping never modifies canonical workspace state (§30) - this
// server only reads from the stores behind its readers.
func (s *Server) Shutdown(ctx context.Context) error {
	s.sessions.InvalidateAll()
	if s.stopReconciliation != nil {
		s.stopReconciliation()
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
