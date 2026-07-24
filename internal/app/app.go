// Package app wires a discovered workspace to the services built from it
// (policy, tool supervisor, approvals, git inspection, worktree lifecycle),
// giving the CLI (and eventually the daemon, §3.1) a single bootstrap path
// instead of each entrypoint wiring these individually.
package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/capsule"
	"github.com/ygrip/punakawan/internal/contextrequest"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/prreview"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/syncqueue"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workspace"
)

// App bundles a loaded workspace and the services built from it.
type App struct {
	Workspace       *workspace.Workspace
	Policy          *policy.Policy
	Supervisor      *tools.Supervisor
	Approvals       *approvals.Store
	Capsules        *capsule.Store
	Inspector       *gitops.Inspector
	Worktrees       *gitops.WorktreeManager
	Workflow        *workflow.Store
	AdapterRegistry *adapters.Registry
	SyncQueue       *syncqueue.Queue
	PrReviews       *prreview.Store
	ContextRequests *contextrequest.Store

	knowledgeMu    sync.Mutex
	knowledgeStore *knowledge.Store

	searchIndexMu sync.Mutex
	searchIndex   *search.Index

	// closed is set by Close, under closedMu, so that a lazy-open call
	// (OpenKnowledge, OpenSearchIndex) racing with or arriving after Close
	// - e.g. a background goroutine a caller started but did not fully
	// join before calling Close, per punokawan-q9r.6.1 - fails loudly
	// instead of silently starting a brand new, untracked external
	// process (Dolt's sql-server) that Close will never get a chance to
	// stop.
	closedMu sync.Mutex
	closed   bool

	jiraWorkflowMu     sync.Mutex
	jiraWorkflowConfig *jiraworkflow.Config
}

// Load discovers the workspace containing startDir and wires up its services.
func Load(startDir string) (*App, error) {
	ws, err := workspace.Discover(startDir)
	if err != nil {
		return nil, err
	}

	pol, err := policy.Load(ws.PolicyPath())
	if err != nil {
		return nil, err
	}

	roots := make([]string, 0, len(ws.Repositories)+1)
	roots = append(roots, ws.Root)
	for _, r := range ws.Repositories {
		path, err := ws.RepositoryPath(r.ID)
		if err != nil {
			return nil, err
		}
		roots = append(roots, path)
	}
	sup := tools.New(roots...)

	store, err := approvals.Open(ws.Root)
	if err != nil {
		return nil, err
	}

	capsules, err := capsule.OpenStore(ws.Root)
	if err != nil {
		return nil, err
	}

	wf, err := workflow.Open(ws.Root)
	if err != nil {
		return nil, err
	}

	global, err := workspace.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	mergedAdapters := ws.MergeAdapters(global)
	specs := make(map[string]adapters.AdapterSpec, len(mergedAdapters))
	for id, cfg := range mergedAdapters {
		specs[id] = adapters.AdapterSpec{
			Command:        cfg.Command,
			Args:           cfg.Args,
			Env:            []string{"PUNAKAWAN_WORKSPACE_ROOT=" + ws.Root},
			EnvPassthrough: cfg.EnvPassthrough,
		}
	}

	syncQueue, err := syncqueue.Open(ws.Root)
	if err != nil {
		return nil, err
	}

	prReviews, err := prreview.OpenStore(ws.Root)
	if err != nil {
		return nil, err
	}

	contextRequests, err := contextrequest.OpenStore(ws.Root)
	if err != nil {
		return nil, err
	}

	registry := adapters.NewRegistry(specs, store)
	registry.SetApprovalScope(pol.Approvals.Scope)
	registry.SetSyncQueue(syncQueue)

	return &App{
		Workspace:       ws,
		Policy:          pol,
		Supervisor:      sup,
		Approvals:       store,
		Capsules:        capsules,
		Inspector:       gitops.NewInspector(sup),
		Worktrees:       gitops.NewWorktreeManager(sup, store, pol),
		Workflow:        wf,
		AdapterRegistry: registry,
		SyncQueue:       syncQueue,
		PrReviews:       prReviews,
		ContextRequests: contextRequests,
	}, nil
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

// OpenKnowledge lazily starts the Dolt-backed knowledge store rooted at
// .punakawan/knowledge under the workspace, memoizing the result. This is
// deferred rather than wired eagerly in Load because it starts an external
// Dolt server process: most commands (workspace show, git status, worktree
// lifecycle, doctor) never touch durable knowledge and should not pay that
// startup cost on every invocation.
func (a *App) OpenKnowledge() (*knowledge.Store, error) {
	if a.isClosed() {
		return nil, errAppClosed
	}
	a.knowledgeMu.Lock()
	defer a.knowledgeMu.Unlock()

	if a.knowledgeStore != nil {
		return a.knowledgeStore, nil
	}
	// Re-check under knowledgeMu: Close acquires knowledgeMu too, so this
	// closes the window between the isClosed check above and this lock
	// being acquired.
	if a.isClosed() {
		return nil, errAppClosed
	}
	store, err := knowledge.Open(a.Supervisor, filepath.Join(a.Workspace.Root, ".punakawan", "knowledge"))
	if err != nil {
		return nil, err
	}
	a.knowledgeStore = store
	return store, nil
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

// OpenSearchIndex lazily opens the Bleve BM25F index rooted at
// .punakawan/index/bm25 under the workspace (§10.2), memoizing the result.
// Per §11.11 the index is disposable and always rebuildable from
// OpenKnowledge's Store, so callers searching it should call search.Rebuild
// first rather than assume it is already current.
func (a *App) OpenSearchIndex() (*search.Index, error) {
	if a.isClosed() {
		return nil, errAppClosed
	}
	a.searchIndexMu.Lock()
	defer a.searchIndexMu.Unlock()

	if a.searchIndex != nil {
		return a.searchIndex, nil
	}
	if a.isClosed() {
		return nil, errAppClosed
	}
	ix, err := search.OpenIndex(filepath.Join(a.Workspace.Root, ".punakawan", "index", "bm25"))
	if err != nil {
		return nil, err
	}
	a.searchIndex = ix
	return ix, nil
}

// SearchKnowledge synchronizes the search index to the knowledge store and
// runs req against it, holding searchIndexMu across both. search.Rebuild is a
// read-modify-write over the shared index, so two concurrent search_knowledge
// calls must not interleave a rebuild with each other's read (punokawan-hzp).
// Rebuild is watermark-gated, so in steady state (no knowledge mutations
// between searches) it is a cheap no-op and this lock is held only briefly
// (punokawan-77q).
func (a *App) SearchKnowledge(store *knowledge.Store, ix *search.Index, req search.Request) ([]search.Result, error) {
	a.searchIndexMu.Lock()
	defer a.searchIndexMu.Unlock()

	if err := search.Rebuild(store, ix); err != nil {
		return nil, err
	}
	return search.Search(store, ix, req)
}

// Close releases resources opened on demand (the knowledge store's Dolt
// server and the BM25 search index, if OpenKnowledge/OpenSearchIndex were
// ever called) and shuts down any adapter processes the AdapterRegistry has
// started.
func (a *App) Close() error {
	a.closedMu.Lock()
	a.closed = true
	a.closedMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adapterErr := a.AdapterRegistry.Close(ctx)

	a.searchIndexMu.Lock()
	var searchErr error
	if a.searchIndex != nil {
		searchErr = a.searchIndex.Close()
		a.searchIndex = nil
	}
	a.searchIndexMu.Unlock()

	a.knowledgeMu.Lock()
	defer a.knowledgeMu.Unlock()

	if a.knowledgeStore == nil {
		if adapterErr != nil {
			return adapterErr
		}
		return searchErr
	}
	knowledgeErr := a.knowledgeStore.Close()
	a.knowledgeStore = nil
	if adapterErr != nil {
		return adapterErr
	}
	if searchErr != nil {
		return searchErr
	}
	return knowledgeErr
}
