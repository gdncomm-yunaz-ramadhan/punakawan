// Package app wires a discovered workspace to the services built from it
// (policy, tool supervisor, approvals, git inspection, worktree lifecycle),
// giving the CLI (and eventually the daemon, §3.1) a single bootstrap path
// instead of each entrypoint wiring these individually.
package app

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/policy"
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
	Inspector       *gitops.Inspector
	Worktrees       *gitops.WorktreeManager
	Workflow        *workflow.Store
	AdapterRegistry *adapters.Registry

	knowledgeMu    sync.Mutex
	knowledgeStore *knowledge.Store

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

	return &App{
		Workspace:       ws,
		Policy:          pol,
		Supervisor:      sup,
		Approvals:       store,
		Inspector:       gitops.NewInspector(sup),
		Worktrees:       gitops.NewWorktreeManager(sup, store, pol),
		Workflow:        wf,
		AdapterRegistry: adapters.NewRegistry(specs, store),
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

// Close releases resources opened on demand (the knowledge store's Dolt
// server, if OpenKnowledge was ever called) and shuts down any adapter
// processes the AdapterRegistry has started.
func (a *App) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	adapterErr := a.AdapterRegistry.Close(ctx)

	a.knowledgeMu.Lock()
	defer a.knowledgeMu.Unlock()

	if a.knowledgeStore == nil {
		return adapterErr
	}
	knowledgeErr := a.knowledgeStore.Close()
	a.knowledgeStore = nil
	if adapterErr != nil {
		return adapterErr
	}
	return knowledgeErr
}
