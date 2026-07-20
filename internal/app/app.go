// Package app wires a discovered workspace to the services built from it
// (policy, tool supervisor, approvals, git inspection, worktree lifecycle),
// giving the CLI (and eventually the daemon, §3.1) a single bootstrap path
// instead of each entrypoint wiring these individually.
package app

import (
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/policy"
	"github.com/ygrip/punakawan/internal/tools"
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

	return &App{
		Workspace:  ws,
		Policy:     pol,
		Supervisor: sup,
		Approvals:  store,
		Inspector:  gitops.NewInspector(sup),
		Worktrees:  gitops.NewWorktreeManager(sup, store, pol),
	}, nil
}

// RepoPath resolves a repository id declared in the workspace to its
// absolute path.
func (a *App) RepoPath(repoID string) (string, error) {
	return a.Workspace.RepositoryPath(repoID)
}
