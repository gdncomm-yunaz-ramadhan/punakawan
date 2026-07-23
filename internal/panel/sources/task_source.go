package sources

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/panel/contract"
)

// staleTaskAfter is how long a task may go without an update before the
// panel flags it as stale, per §14.5's "show blocker reasons and stale
// tasks."
const staleTaskAfter = 14 * 24 * time.Hour

// TaskSource implements contract.TaskReader over bd, via internal/beads.
type TaskSource struct {
	App *app.App
}

func (t *TaskSource) checkWorkspace(workspaceID string) error {
	if workspaceID != t.App.Workspace.ID {
		return fmt.Errorf("sources: workspace %q is not available (only %q is)", workspaceID, t.App.Workspace.ID)
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

// boardStatus derives a §14.5 status-board column from an issue's stored
// status plus whether bd currently considers it ready. bd has no "review"
// or "failed" issue status, so this never yields those two of the plan's
// seven board columns - an honest gap in the underlying data model, not an
// oversight.
func boardStatus(issue beads.ReadyIssue, ready bool) string {
	switch issue.Status {
	case "open":
		if ready {
			return "ready"
		}
		return "blocked"
	case "in_progress":
		return "active"
	case "blocked":
		return "blocked"
	case "deferred":
		return "pending"
	case "closed":
		return "completed"
	default:
		return issue.Status
	}
}

func isStale(updatedAt string) bool {
	ts, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return false
	}
	return time.Since(ts) > staleTaskAfter
}

// blockingReasons reports which of a blocked issue's "blocks" dependencies
// are not yet closed - the actual reason it isn't ready. Only "blocks"
// edges count: verified empirically against bd 1.0.4's own `bd ready
// --explain` output that a parent-child (or tracks/related/...) edge to an
// open issue does NOT keep the child out of the ready set, only an unmet
// "blocks" edge does. byID indexes every issue in the same workspace
// fetch, so this needs no extra bd invocation per task.
func blockingReasons(issue beads.ReadyIssue, isBlocked bool, byID map[string]beads.ReadyIssue) []string {
	if !isBlocked {
		return nil
	}
	var reasons []string
	for _, dep := range issue.Dependencies {
		if dep.Type != "blocks" {
			continue
		}
		target, ok := byID[dep.DependsOnId]
		if !ok {
			reasons = append(reasons, "waiting on "+dep.DependsOnId+" (external reference, not in this workspace)")
			continue
		}
		if target.Status != "closed" {
			reasons = append(reasons, "waiting on "+target.ID+" \""+target.Title+"\" ("+target.Status+")")
		}
	}
	return reasons
}

func summarize(issues []beads.ReadyIssue, readyIDs map[string]bool) []contract.TaskSummary {
	byID := make(map[string]beads.ReadyIssue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	out := make([]contract.TaskSummary, 0, len(issues))
	for _, issue := range issues {
		board := boardStatus(issue, readyIDs[issue.ID])
		out = append(out, contract.TaskSummary{
			ReadyIssue:      issue,
			BoardStatus:     board,
			BlockingReasons: blockingReasons(issue, board == "blocked", byID),
			Stale:           isStale(issue.UpdatedAt),
		})
	}
	return out
}

// matchesPriority compares a stored priority (0-4) against a bd-style
// filter value ("2" or "P2"), tolerating either form. An unparseable
// filter value matches nothing, per §11.4's documented filter rather than
// silently ignoring a typo'd query parameter.
func matchesPriority(priority int, filter string) bool {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(filter), "P"))
	if err != nil {
		return false
	}
	return priority == n
}

func (t *TaskSource) List(ctx context.Context, workspaceID string, filter contract.TaskFilter) ([]contract.TaskSummary, error) {
	if err := t.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}

	// Fetched unfiltered and unbounded: BoardStatus/BlockingReasons need
	// every issue's real status to cross-reference dependencies and
	// readiness correctly, regardless of which subset filter narrows the
	// response to.
	issues, err := beads.List(ctx, t.App.Supervisor, t.App.Workspace.Root, beads.ListOptions{Limit: -1})
	if err != nil {
		return nil, fmt.Errorf("sources: list tasks: %w", err)
	}
	readyIDs, err := t.readySet(ctx)
	if err != nil {
		return nil, err
	}

	out := []contract.TaskSummary{}
	query := strings.ToLower(filter.Query)
	for _, s := range summarize(issues, readyIDs) {
		if filter.Status != "" && s.Status != filter.Status {
			continue
		}
		if filter.Priority != "" && !matchesPriority(s.Priority, filter.Priority) {
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
	issues, err := beads.List(ctx, t.App.Supervisor, t.App.Workspace.Root, beads.ListOptions{Limit: -1})
	if err != nil {
		return contract.TaskGraph{}, fmt.Errorf("sources: task dependencies: %w", err)
	}
	readyIDs, err := t.readySet(ctx)
	if err != nil {
		return contract.TaskGraph{}, err
	}

	graph := contract.TaskGraph{Nodes: summarize(issues, readyIDs), Edges: []contract.TaskEdge{}, Cycles: [][]string{}}
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			graph.Edges = append(graph.Edges, contract.TaskEdge{From: dep.IssueId, To: dep.DependsOnId, Type: dep.Type})
		}
	}
	graph.Cycles = detectCycles(graph.Edges)
	return graph, nil
}

// detectCycles finds every distinct cycle in the From->To dependency edges
// via DFS with a recursion-stack color map (white/gray/black), per the
// panel's exit criterion that dependency cycles be detected and displayed
// rather than left to confuse a tree-shaped rendering. Each returned cycle
// is the ordered walk from the point a gray (in-progress) node is
// re-encountered back to itself.
func detectCycles(edges []contract.TaskEdge) [][]string {
	adjacency := map[string][]string{}
	for _, e := range edges {
		adjacency[e.From] = append(adjacency[e.From], e.To)
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var stack []string
	var cycles [][]string

	var visit func(node string)
	visit = func(node string) {
		color[node] = gray
		stack = append(stack, node)
		for _, next := range adjacency[node] {
			switch color[next] {
			case white:
				visit(next)
			case gray:
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == next {
						cycle := append([]string{}, stack[i:]...)
						cycle = append(cycle, next)
						cycles = append(cycles, cycle)
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
	}

	for node := range adjacency {
		if color[node] == white {
			visit(node)
		}
	}
	return cycles
}
