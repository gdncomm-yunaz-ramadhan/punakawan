// Package tasksnapshot builds and caches one reusable view of a project's bd
// work graph, so the panel's board, table, dependency graph, and count
// widgets share a SINGLE `bd list` + `bd ready` refresh rather than each
// re-shelling into bd independently (punakawan-panel-project-performance-
// improvement-plan.md §8/§12, Phase 5).
//
// The derivation logic (board status, blocker reasons, dependency cycles,
// staleness) lives here rather than in internal/panel/sources so both the
// snapshot service AND the sources.TaskSource fallback path can call the
// exact same builder without an import cycle (sources depends on
// tasksnapshot, never the reverse).
package tasksnapshot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/tools"
)

// staleTaskAfter is how long a task may go without an update before the
// panel flags it as stale, per §14.5's "show blocker reasons and stale
// tasks." Moved here (from sources) alongside isStale so the shared builder
// owns it.
const staleTaskAfter = 14 * 24 * time.Hour

// ProjectTaskSnapshot is one immutable view of a project's bd work graph at
// a point in time: every task (unfiltered), the dependency graph (with
// cycles), and the board-column counts, all derived from a single
// list+ready refresh. Stale is true when this snapshot is a retained copy
// of a previous successful refresh that a later refresh failed to replace,
// per §12's "keep old snapshot when refresh fails and mark stale."
type ProjectTaskSnapshot struct {
	ProjectID      string
	UpdatedAt      time.Time
	Tasks          []contract.TaskSummary
	Graph          contract.TaskGraph
	OpenCount      int
	ReadyCount     int
	ActiveCount    int
	BlockedCount   int
	CompletedCount int
	Stale          bool
}

// Runner fetches the raw bd data one Refresh needs: every issue (unbounded,
// unfiltered) plus the set of issue IDs bd currently considers ready. It is
// the single injection point that keeps the service testable without a real
// bd binary - service_test.go supplies a fake, production supplies
// BeadsRunner.
type Runner func(ctx context.Context) (issues []beads.ReadyIssue, ready map[string]bool, err error)

// BeadsRunner builds a Runner backed by a real bd binary, running `bd list
// --limit=0` (every issue regardless of state) and `bd ready` once each in
// root via sup. This mirrors the exact two calls sources.TaskSource made
// per request before the snapshot existed.
func BeadsRunner(sup *tools.Supervisor, root string) Runner {
	return func(ctx context.Context) ([]beads.ReadyIssue, map[string]bool, error) {
		issues, err := beads.List(ctx, sup, root, beads.ListOptions{Limit: -1})
		if err != nil {
			return nil, nil, fmt.Errorf("tasksnapshot: list tasks: %w", err)
		}
		ready, err := beads.Ready(ctx, sup, root, beads.ReadyOptions{})
		if err != nil {
			return nil, nil, fmt.Errorf("tasksnapshot: ready set: %w", err)
		}
		set := make(map[string]bool, len(ready))
		for _, r := range ready {
			set[r.ID] = true
		}
		return issues, set, nil
	}
}

// inflightCall is one in-progress Refresh other callers for the same
// projectID coalesce onto, so a burst of board/table/graph/counts requests
// triggers a single bd refresh rather than N. This is a deliberately
// minimal, inline singleflight (§12) rather than a golang.org/x/sync
// dependency.
type inflightCall struct {
	done chan struct{}
	snap *ProjectTaskSnapshot
	err  error
}

// Service caches one ProjectTaskSnapshot per projectID and deduplicates
// concurrent refreshes. The zero value is not usable; construct with
// NewService.
type Service struct {
	runner Runner

	mu       sync.Mutex
	cache    map[string]*ProjectTaskSnapshot
	inflight map[string]*inflightCall
}

// NewService returns a Service that refreshes via runner.
func NewService(runner Runner) *Service {
	return &Service{
		runner:   runner,
		cache:    map[string]*ProjectTaskSnapshot{},
		inflight: map[string]*inflightCall{},
	}
}

// Get returns the cached snapshot for projectID, if any, without triggering
// a refresh. The bool reports presence.
func (s *Service) Get(projectID string) (*ProjectTaskSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap, ok := s.cache[projectID]
	return snap, ok
}

// Invalidate drops the cached snapshot for projectID, forcing the next
// Refresh (or Get-then-Refresh consumer) to rebuild from bd.
func (s *Service) Invalidate(projectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, projectID)
}

// Refresh rebuilds projectID's snapshot from bd, caches it, and returns it.
// Concurrent Refresh calls for the same projectID share a single underlying
// bd refresh (inline singleflight). On failure it returns the previously
// cached snapshot marked Stale=true alongside the error (§12); if there is
// no previous snapshot it returns a nil snapshot and the error.
func (s *Service) Refresh(ctx context.Context, projectID string) (*ProjectTaskSnapshot, error) {
	s.mu.Lock()
	if call, ok := s.inflight[projectID]; ok {
		s.mu.Unlock()
		<-call.done
		return call.snap, call.err
	}
	call := &inflightCall{done: make(chan struct{})}
	s.inflight[projectID] = call
	s.mu.Unlock()

	snap, err := s.doRefresh(ctx, projectID)

	s.mu.Lock()
	delete(s.inflight, projectID)
	// Cache both a fresh snapshot and a retained-stale one, so a subsequent
	// Get sees the stale copy rather than the pre-failure fresh flag.
	if snap != nil {
		s.cache[projectID] = snap
	}
	call.snap = snap
	call.err = err
	s.mu.Unlock()
	close(call.done)

	return snap, err
}

// doRefresh runs the injected Runner once and builds a snapshot, or - on
// Runner failure - clones the previous cached snapshot with Stale=true.
func (s *Service) doRefresh(ctx context.Context, projectID string) (*ProjectTaskSnapshot, error) {
	issues, ready, err := s.runner(ctx)
	if err != nil {
		wrapped := fmt.Errorf("tasksnapshot: refresh %q: %w", projectID, err)
		s.mu.Lock()
		prev, ok := s.cache[projectID]
		s.mu.Unlock()
		if !ok || prev == nil {
			return nil, wrapped
		}
		stale := *prev
		stale.Stale = true
		return &stale, wrapped
	}
	return BuildSnapshot(projectID, issues, ready), nil
}

// BuildSnapshot derives a ProjectTaskSnapshot from a single list+ready
// fetch: the TaskSummary list (board status, blocker reasons, staleness),
// the dependency graph (+cycles), and the board-column counts. It is
// exported so sources.TaskSource's no-service fallback path reuses the exact
// same derivation. Counts follow §12: Open=stored status open; Ready=in the
// ready set; Active=in_progress; Blocked=open-not-ready plus stored blocked
// (i.e. board status "blocked"); Completed=closed.
func BuildSnapshot(projectID string, issues []beads.ReadyIssue, readyIDs map[string]bool) *ProjectTaskSnapshot {
	tasks := summarize(issues, readyIDs)
	snap := &ProjectTaskSnapshot{
		ProjectID: projectID,
		UpdatedAt: time.Now(),
		Tasks:     tasks,
		Graph:     buildGraph(tasks, issues),
	}
	for _, t := range tasks {
		switch t.Status {
		case "open":
			snap.OpenCount++
		case "in_progress":
			snap.ActiveCount++
		case "closed":
			snap.CompletedCount++
		}
		if readyIDs[t.ID] {
			snap.ReadyCount++
		}
		if t.BoardStatus == "blocked" {
			snap.BlockedCount++
		}
	}
	return snap
}

func buildGraph(nodes []contract.TaskSummary, issues []beads.ReadyIssue) contract.TaskGraph {
	if nodes == nil {
		nodes = []contract.TaskSummary{}
	}
	graph := contract.TaskGraph{Nodes: nodes, Edges: []contract.TaskEdge{}, Cycles: [][]string{}}
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			graph.Edges = append(graph.Edges, contract.TaskEdge{From: dep.IssueId, To: dep.DependsOnId, Type: dep.Type})
		}
	}
	graph.Cycles = detectCycles(graph.Edges)
	return graph
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

// MatchesPriority compares a stored priority (0-4) against a bd-style filter
// value ("2" or "P2"), tolerating either form. An unparseable filter value
// matches nothing, per §11.4's documented filter rather than silently
// ignoring a typo'd query parameter. Exported here so sources.TaskSource's
// List filtering shares the same parsing.
func MatchesPriority(priority int, filter string) bool {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(filter), "P"))
	if err != nil {
		return false
	}
	return priority == n
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
	// Non-nil so the JSON encoder emits [] not null; the panel reads
	// graph.cycles.length unconditionally.
	cycles := [][]string{}

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
