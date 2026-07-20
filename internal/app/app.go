// Package app wires a discovered workspace to the services built from it
// (policy, tool supervisor, approvals, git inspection, worktree lifecycle),
// giving the CLI (and eventually the daemon, §3.1) a single bootstrap path
// instead of each entrypoint wiring these individually.
package app

import (
	"path/filepath"
	"sync"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workspace"
)

// App bundles a loaded workspace and the services built from it.
type App struct {
	Workspace  *workspace.Workspace
	Policy     *policy.Policy
	Supervisor *tools.Supervisor
	Approvals  *approvals.Store
	Inspector  *gitops.Inspector
	Worktrees  *gitops.WorktreeManager
	Workflow   *workflow.Store

	knowledgeMu    sync.Mutex
	knowledgeStore *knowledge.Store
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

	wf, err := workflow.Open(ws.Root)
	if err != nil {
		return nil, err
	}

	return &App{
		Workspace:  ws,
		Policy:     pol,
		Supervisor: sup,
		Approvals:  store,
		Inspector:  gitops.NewInspector(sup),
		Worktrees:  gitops.NewWorktreeManager(sup, store, pol),
		Workflow:   wf,
	}, nil
}

// RepoPath resolves a repository id declared in the workspace to its
// absolute path.
func (a *App) RepoPath(repoID string) (string, error) {
	return a.Workspace.RepositoryPath(repoID)
}

// OpenKnowledge lazily starts the Dolt-backed knowledge store rooted at
// .punakawan/knowledge under the workspace, memoizing the result. This is
// deferred rather than wired eagerly in Load because it starts an external
// Dolt server process: most commands (workspace show, git status, worktree
// lifecycle, doctor) never touch durable knowledge and should not pay that
// startup cost on every invocation.
func (a *App) OpenKnowledge() (*knowledge.Store, error) {
	a.knowledgeMu.Lock()
	defer a.knowledgeMu.Unlock()

	if a.knowledgeStore != nil {
		return a.knowledgeStore, nil
	}
	store, err := knowledge.Open(a.Supervisor, filepath.Join(a.Workspace.Root, ".punakawan", "knowledge"))
	if err != nil {
		return nil, err
	}
	a.knowledgeStore = store
	return store, nil
}

// Close releases resources opened on demand (currently, the knowledge
// store's Dolt server, if OpenKnowledge was ever called).
func (a *App) Close() error {
	a.knowledgeMu.Lock()
	defer a.knowledgeMu.Unlock()

	if a.knowledgeStore == nil {
		return nil
	}
	err := a.knowledgeStore.Close()
	a.knowledgeStore = nil
	return err
}
