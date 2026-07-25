package sources

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/tasksnapshot"
)

// TaskSource implements contract.TaskReader over bd, via internal/beads.
//
// Snapshot is optional. When set, List and Dependencies read from ONE shared
// ProjectTaskSnapshot (a single `bd list` + `bd ready` refresh reused across
// the board, table, graph, and count widgets), refreshing it when absent or
// stale. When nil, TaskSource falls back to the pre-snapshot behavior
// exactly: each call fetches from bd itself and builds the derivation inline
// via tasksnapshot.BuildSnapshot. This keeps the existing
// `&TaskSource{App: a}` construction (in deps.go and tests) compiling and
// behaving identically.
type TaskSource struct {
	App      *app.App
	Snapshot *tasksnapshot.Service
}

func (t *TaskSource) checkWorkspace(workspaceID string) error {
	if workspaceID != t.App.Workspace.ID {
		return fmt.Errorf("sources: workspace %q is not available (only %q is): %w", workspaceID, t.App.Workspace.ID, contract.ErrWorkspaceUnavailable)
	}
	return nil
}

// readySet reports which issue IDs bd currently considers ready to claim
// (its GetReadyWork semantics: open, with no active blockers). This is the
// only reliable way to tell a truly-blocked "open" issue from a truly-
// ready one - bd does not flip an issue's stored Status to "blocked" just
// because one of its "blocks" dependencies is still open (verified
// empirically against bd 1.0.4).
func (t *TaskSource) readySet(ctx context.Context) (map[string]bool, error) {
	ready, err := beads.Ready(ctx, t.App.Supervisor, t.App.Workspace.Root, beads.ReadyOptions{})
	if err != nil {
		return nil, fmt.Errorf("sources: ready set: %w", err)
	}
	set := make(map[string]bool, len(ready))
	for _, r := range ready {
		set[r.ID] = true
	}
	return set, nil
}

// snapshot returns the project's task snapshot: from the shared Snapshot
// service when one is wired (refreshing on a miss or a stale entry), or
// built inline from a fresh bd fetch when Snapshot is nil. Both paths run
// the same tasksnapshot.BuildSnapshot derivation, so List/Dependencies stay
// byte-for-byte identical whether or not the shared cache is in play.
func (t *TaskSource) snapshot(ctx context.Context) (*tasksnapshot.ProjectTaskSnapshot, error) {
	projectID := t.App.Workspace.ID

	// Beads-less project: read from Punakawan's fallback task store instead of
	// bd. It emits the same (issues, readyIDs) BuildSnapshot consumes, so the
	// derived board/graph is identical in shape to the bd-backed path. Gated
	// ahead of the shared-snapshot branch so it applies to every reader.
	if !beads.ProjectInitialized(t.App.Workspace.Root) {
		store, err := t.App.OpenTaskStore()
		if err != nil {
			return nil, fmt.Errorf("sources: open task store: %w", err)
		}
		issues, readyIDs, err := store.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("sources: list tasks (fallback store): %w", err)
		}
		return tasksnapshot.BuildSnapshot(projectID, issues, readyIDs), nil
	}

	if t.Snapshot != nil {
		if snap, ok := t.Snapshot.Get(projectID); ok && snap != nil && !snap.Stale {
			return snap, nil
		}
		return t.Snapshot.Refresh(ctx, projectID)
	}

	// Fallback (no shared service): fetch unfiltered and unbounded, exactly
	// as before. BoardStatus/BlockingReasons need every issue's real status
	// to cross-reference dependencies and readiness correctly, regardless of
	// which subset a filter narrows the response to.
	issues, err := beads.List(ctx, t.App.Supervisor, t.App.Workspace.Root, beads.ListOptions{Limit: -1})
	if err != nil {
		return nil, fmt.Errorf("sources: list tasks: %w", err)
	}
	readyIDs, err := t.readySet(ctx)
	if err != nil {
		return nil, err
	}
	return tasksnapshot.BuildSnapshot(projectID, issues, readyIDs), nil
}

func (t *TaskSource) List(ctx context.Context, workspaceID string, filter contract.TaskFilter) ([]contract.TaskSummary, error) {
	if err := t.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}

	snap, err := t.snapshot(ctx)
	if err != nil {
		return nil, err
	}

	out := []contract.TaskSummary{}
	query := strings.ToLower(filter.Query)
	for _, s := range snap.Tasks {
		if filter.Status != "" && s.Status != filter.Status {
			continue
		}
		if filter.Priority != "" && !tasksnapshot.MatchesPriority(s.Priority, filter.Priority) {
			continue
		}
		if filter.Type != "" && s.IssueType != filter.Type {
			continue
		}
		if filter.Assignee != "" && s.Assignee != filter.Assignee {
			continue
		}
		if filter.ExternalIssue != "" && s.ExternalRef != filter.ExternalIssue {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(s.Title), query) && !strings.Contains(strings.ToLower(s.Description), query) {
			continue
		}
		out = append(out, s)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func (t *TaskSource) Get(ctx context.Context, workspaceID, taskID string) (beads.Issue, error) {
	if err := t.checkWorkspace(workspaceID); err != nil {
		return beads.Issue{}, err
	}
	if !beads.ProjectInitialized(t.App.Workspace.Root) {
		store, err := t.App.OpenTaskStore()
		if err != nil {
			return beads.Issue{}, fmt.Errorf("sources: open task store: %w", err)
		}
		issue, err := store.Get(ctx, taskID)
		if err != nil {
			return beads.Issue{}, fmt.Errorf("sources: get task %q (fallback store): %w", taskID, err)
		}
		return issue, nil
	}
	issue, err := beads.Show(ctx, t.App.Supervisor, t.App.Workspace.Root, taskID)
	if err != nil {
		return beads.Issue{}, fmt.Errorf("sources: get task %q: %w", taskID, err)
	}
	return issue, nil
}

func (t *TaskSource) Dependencies(ctx context.Context, workspaceID string) (contract.TaskGraph, error) {
	if err := t.checkWorkspace(workspaceID); err != nil {
		return contract.TaskGraph{}, err
	}
	snap, err := t.snapshot(ctx)
	if err != nil {
		return contract.TaskGraph{}, err
	}
	return snap.Graph, nil
}
