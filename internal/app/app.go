// Package app wires a discovered workspace to the services built from it
// (policy, tool supervisor, git inspection, worktree lifecycle), giving the
// CLI (and eventually the daemon, §3.1) a single bootstrap path instead of
// each entrypoint wiring these individually.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/contextrequest"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/planexec"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/providercreds"
	"github.com/ygrip/punakawan/internal/prreview"
	"github.com/ygrip/punakawan/internal/roleconfig"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workspace"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// App bundles a loaded workspace and the services built from it.
type App struct {
	Workspace       *workspace.Workspace
	Policy          *policy.Policy
	Supervisor      *tools.Supervisor
	Inspector       *gitops.Inspector
	Workflow        *workflow.Store
	AdapterRegistry *adapters.Registry
	// Credentials is the host's provider organisation store, or nil when
	// this host has no readable credentials path. It is held here rather
	// than reopened per call site so every consumer resolves an
	// organisation against exactly the file the adapter registry does.
	Credentials     *providercreds.Store
	PrReviews       *prreview.Store
	ContextRequests *contextrequest.Store
	// RoleConfig resolves a project's persisted role prompt preferences
	// (style + free-text instructions) for prompt rendering. It shapes prompt
	// wording only - it never authorizes a tool or gates a workflow stage.
	// May be nil if construction failed; every call site must guard nil.
	RoleConfig *roleconfig.Resolver

	storageMu sync.Mutex
	storageDB *storage.DB

	planMu    sync.Mutex
	planStore *plan.Store

	planExecMu    sync.Mutex
	planExecStore *planexec.Store

	learningMu    sync.Mutex
	learningStore *learning.Store

	outboxMu sync.Mutex
	outbox   *outbox.Store

	// closed is set by Close, under closedMu, so that a lazy-open call
	// racing with or arriving after Close - e.g. a background goroutine a
	// caller started but did not fully join before calling Close, per
	// punokawan-q9r.6.1 - fails loudly instead of silently starting a
	// brand new, untracked external process (Dolt's sql-server) that
	// Close will never get a chance to stop.
	closedMu sync.Mutex
	closed   bool

	jiraWorkflowMu     sync.Mutex
	jiraWorkflowConfig *jiraworkflow.Config
}

// Load discovers the workspace containing startDir and wires up its
// services. Finding no project above startDir is not an error: punakawan
// is a machine-wide control plane, so it loads against the global
// workspace and the caller works out from Workspace.Global whether a
// project is in scope.
func Load(startDir string) (*App, error) {
	ws, err := workspace.Discover(startDir)
	if err != nil {
		return nil, err
	}
	return load(ws)
}

// LoadProject is Load for a caller that genuinely needs a project and has
// nothing sensible to do without one - registering a workspace, or serving
// a project the panel's registry still lists.
//
// Load answering with the global workspace is what makes running from
// anywhere work, but it also means a path that has been deleted or is no
// longer a checkout now loads successfully. Without this a project whose
// directory is gone would render as healthy with no repositories, instead
// of as unavailable.
func LoadProject(startDir string) (*App, error) {
	a, err := Load(startDir)
	if err != nil {
		return nil, err
	}
	if a.Workspace.Global {
		_ = a.Close()
		return nil, fmt.Errorf("app: %s is not a project: no workspace.yaml or git repository found there", startDir)
	}
	return a, nil
}

// load wires up an *App's services from an already-resolved workspace.
func load(ws *workspace.Workspace) (*App, error) {
	pol, err := policy.Load(ws.PolicyPath())
	if err != nil {
		return nil, err
	}

	roots := make([]string, 0, len(ws.Repositories)+2)
	roots = append(roots, ws.Root)
	for _, r := range ws.Repositories {
		path, err := ws.RepositoryPath(r.ID)
		if err != nil {
			return nil, err
		}
		roots = append(roots, path)
	}
	sup := tools.New(roots...)

	workflowRoot, err := ws.WorkflowRoot()
	if err != nil {
		return nil, err
	}
	wf, err := workflow.Open(workflowRoot)
	if err != nil {
		return nil, err
	}

	global, err := workspace.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	trustFilePath, err := storage.AdapterTrustFilePath()
	if err != nil {
		return nil, err
	}
	trust, err := adapters.LoadTrustFile(trustFilePath)
	if err != nil {
		return nil, err
	}

	mergedAdapters := ws.MergeAdapters(global)
	specs := make(map[string]adapters.AdapterSpec, len(mergedAdapters))
	for id, cfg := range mergedAdapters {
		// A repository-local adapter command (one resolving inside this
		// checkout) can be swapped out by anyone who can write into the
		// checkout, so it must be explicitly trusted by this host before
		// Punakawan will ever start it - see adapters.TrustStore.
		if err := adapters.RequireTrustedIfRepositoryLocal(cfg.Command, ws.Root, trust); err != nil {
			return nil, fmt.Errorf("app: adapter %q: %w", id, err)
		}
		specs[id] = adapters.AdapterSpec{
			Command:        cfg.Command,
			Args:           cfg.Args,
			Env:            []string{"PUNAKAWAN_WORKSPACE_ROOT=" + ws.Root},
			EnvPassthrough: cfg.EnvPassthrough,
		}
	}

	prReviews, err := prreview.OpenStore(ws.Root)
	if err != nil {
		return nil, err
	}

	contextRequests, err := contextrequest.OpenStore(ws.Root)
	if err != nil {
		return nil, err
	}

	roleResolver := newRoleResolver(ws)

	a := &App{
		Workspace:       ws,
		Policy:          pol,
		Supervisor:      sup,
		Inspector:       gitops.NewInspector(sup),
		Workflow:        wf,
		PrReviews:       prReviews,
		ContextRequests: contextRequests,
		RoleConfig:      roleResolver,
	}
	registry := adapters.NewRegistry(specs)
	// An org-qualified adapter id ("atlassian:gdncomm") has no spec of its
	// own; it is served by its program's spec plus that organisation's
	// credentials, read here rather than inherited from this process's
	// environment - which holds at most one organisation's.
	if credsPath, err := workspace.GlobalCredentialsPath(); err == nil {
		creds := providercreds.Open(credsPath)
		a.Credentials = creds
		registry.SetOrgEnvResolver(creds.AdapterOrgEnv())
	}
	a.AdapterRegistry = registry

	return a, nil
}

// newRoleResolver builds the shared role prompt-preference resolver for a
// workspace. A read failure surfaces as an error to the caller (which guards
// it) but never panics, and a nil resolver is tolerated everywhere it is
// used.
//
// Limitation: the App currently holds no registry of non-primary project
// roots, so Load resolves every project id to the primary workspace root. An
// empty id, or an id equal to the primary workspace id, is the primary
// workspace; any other id also falls back to the primary root today (there is
// no other root to resolve it to). When multi-project support lands this is the
// single seam to extend.
func newRoleResolver(ws *workspace.Workspace) *roleconfig.Resolver {
	rootFor := func(projectID string) string {
		// Only the primary workspace root is known here; see the limitation
		// documented above.
		return ws.Root
	}
	return &roleconfig.Resolver{
		Load: func(projectID string) (*protocol.RolePreferences, error) {
			return roleconfig.Load(rootFor(projectID))
		},
	}
}

// RepoPath resolves a repository id declared in the workspace to its
// absolute path.
func (a *App) RepoPath(repoID string) (string, error) {
	return a.Workspace.RepositoryPath(repoID)
}

// errAppClosed is returned by lazy-open accessors once Close has run, so a
// stray caller (typically a background goroutine a caller failed to fully
// join before calling Close) gets a clear error instead of silently
// starting a fresh, un-tracked external process or file handle nothing
// will ever release.
var errAppClosed = errors.New("app: already closed")

func (a *App) isClosed() bool {
	a.closedMu.Lock()
	defer a.closedMu.Unlock()
	return a.closed
}

// OpenStorage lazily opens the shared SQLite storage kernel, memoizing the
// result. This is one database shared by every local project checkout on
// this machine; callers scope their own rows by project id.
func (a *App) OpenStorage(ctx context.Context) (*storage.DB, error) {
	if a.isClosed() {
		return nil, errAppClosed
	}
	a.storageMu.Lock()
	defer a.storageMu.Unlock()

	if a.storageDB != nil {
		return a.storageDB, nil
	}
	path, err := storage.DBPath()
	if err != nil {
		return nil, err
	}
	if err := storage.CheckLocation(path); err != nil {
		return nil, err
	}
	db, err := storage.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	a.storageDB = db
	return db, nil
}

// OpenPlan lazily opens the first-class Plan aggregate's store, memoizing
// the result. Like internal/delivery.Store, it is not scoped to this
// workspace's id: a Plan can name several ProjectIDs, so there is no
// single project id to partition it by.
func (a *App) OpenPlan() (*plan.Store, error) {
	if a.isClosed() {
		return nil, errAppClosed
	}
	a.planMu.Lock()
	defer a.planMu.Unlock()

	if a.planStore != nil {
		return a.planStore, nil
	}
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		return nil, err
	}
	a.planStore = plan.NewStore(db)
	return a.planStore, nil
}

// OpenPlanExec lazily opens the plan-step execution domain's store,
// memoizing the result: tracks a plan step's execution lifecycle
// (ready/claimed/committed/reopened) for a project that wants plan-native
// step tracking. Like OpenPlan, it is not scoped to this workspace's id,
// since a Plan (and its steps) is not scoped to one workspace either.
func (a *App) OpenPlanExec() (*planexec.Store, error) {
	if a.isClosed() {
		return nil, errAppClosed
	}
	a.planExecMu.Lock()
	defer a.planExecMu.Unlock()

	if a.planExecStore != nil {
		return a.planExecStore, nil
	}
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		return nil, err
	}
	plans, err := a.OpenPlan()
	if err != nil {
		return nil, err
	}
	a.planExecStore = planexec.NewStore(db, plans)
	return a.planExecStore, nil
}

// OpenLearning lazily opens the learning-proposal side-store, memoizing the
// result, scoped to this workspace's id within the shared storage kernel. It
// is a thin scope over the one shared *storage.DB rather than a per-project
// server, so it starts nothing: the deferral simply avoids opening the
// kernel for commands that never touch a learning proposal.
func (a *App) OpenLearning() (*learning.Store, error) {
	if a.isClosed() {
		return nil, errAppClosed
	}
	a.learningMu.Lock()
	defer a.learningMu.Unlock()

	if a.learningStore != nil {
		return a.learningStore, nil
	}
	if a.isClosed() {
		return nil, errAppClosed
	}
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		return nil, err
	}
	store := learning.New(db, a.Workspace.ID)
	// One-time import of any pre-kernel JSONL proposals file this workspace
	// still has on disk. A failure is non-fatal: the store must still open,
	// so the warning is logged rather than returned (losing old data beats a
	// store that will not open). Runs once - OpenLearning memoizes the store.
	if warn := store.ImportLegacy(a.Workspace.Root); warn != nil {
		slog.Warn("learning: legacy import failed; opening without imported data", "error", warn)
	}
	a.learningStore = store
	return a.learningStore, nil
}

// OpenOutbox lazily opens the durable provider-write outbox, memoizing the
// result. Unlike per-workspace stores, the outbox is not scoped to this
// workspace's id: every enqueued intent already names its own
// orchestration/execution/session, and the outbox's exactly-once claim
// semantics require every worker (regardless of which workspace enqueued a
// given intent) to see the same rows through the one shared kernel.
func (a *App) OpenOutbox() (*outbox.Store, error) {
	if a.isClosed() {
		return nil, errAppClosed
	}
	a.outboxMu.Lock()
	defer a.outboxMu.Unlock()

	if a.outbox != nil {
		return a.outbox, nil
	}
	if a.isClosed() {
		return nil, errAppClosed
	}
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		return nil, err
	}
	a.outbox = outbox.New(db)
	return a.outbox, nil
}

// JiraWorkflow lazily loads and memoizes the workspace's Jira workflow
// config (.punakawan/jira-workflow.yaml). Safe to call even if the file
// does not exist: jiraworkflow.Load returns a safe empty default in that
// case rather than erroring.
func (a *App) JiraWorkflow() (*jiraworkflow.Config, error) {
	a.jiraWorkflowMu.Lock()
	defer a.jiraWorkflowMu.Unlock()

	if a.jiraWorkflowConfig != nil {
		return a.jiraWorkflowConfig, nil
	}
	cfg, err := jiraworkflow.Load(a.Workspace.JiraWorkflowPath())
	if err != nil {
		return nil, err
	}
	a.jiraWorkflowConfig = cfg
	return cfg, nil
}

// Close releases resources opened on demand (the shared storage kernel, if
// it was ever opened) and shuts down any adapter processes the
// AdapterRegistry has started.
func (a *App) Close() error {
	a.closedMu.Lock()
	a.closed = true
	a.closedMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adapterErr := a.AdapterRegistry.Close(ctx)

	a.storageMu.Lock()
	var storageErr error
	if a.storageDB != nil {
		storageErr = a.storageDB.Close()
		a.storageDB = nil
	}
	a.storageMu.Unlock()

	a.learningMu.Lock()
	a.learningStore = nil
	a.learningMu.Unlock()

	a.outboxMu.Lock()
	a.outbox = nil
	a.outboxMu.Unlock()

	if adapterErr != nil {
		return adapterErr
	}
	return storageErr
}
