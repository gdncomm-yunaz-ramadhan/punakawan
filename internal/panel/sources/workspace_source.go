// Package sources implements internal/panel/contract's reader interfaces
// against an already-loaded *app.App, per
// punakawan-panel-implementation-plan.md §8. Each source wraps an existing
// store or CLI wrapper rather than introducing new persistence.
package sources

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// WorkspaceSource implements contract.WorkspaceReader over the workspace
// registry, describing the current *app.App directly (no re-load needed)
// and every other registered workspace by loading it fresh per call - per
// §9, there is no cache yet (a later phase's job), and per §30/§24's
// Phase 2 exit criterion "one broken workspace does not break the page":
// a workspace whose path is missing or invalid degrades to an
// Unavailable summary rather than failing the whole List/Get call.
//
// If Registry is nil, WorkspaceSource falls back to describing only the
// single workspace App was loaded for - the same behavior Phase 1 shipped
// before the registry was wired in here, kept so callers that construct
// this type directly (tests, or any future caller with no registry) still
// get a working single-workspace reader.
type WorkspaceSource struct {
	App      *app.App
	Registry *registry.Store
}

func healthOK(source string, at time.Time) protocol.PanelSourceHealth {
	return protocol.PanelSourceHealth{Source: source, Availability: protocol.PanelSourceHealthAvailabilityAvailable, CheckedAt: at}
}

func healthDown(source string, err error, at time.Time) protocol.PanelSourceHealth {
	msg := err.Error()
	return protocol.PanelSourceHealth{Source: source, Availability: protocol.PanelSourceHealthAvailabilityUnavailable, Message: &msg, CheckedAt: at}
}

// activeStates are the WorkflowRun states counted as an "active session"
// in the workspace summary. Blocked, terminal, and not-yet-started states
// are excluded: a blocked run belongs in "needs attention" (a later
// phase's overview page), not the active count.
var activeStates = map[protocol.WorkflowRunState]bool{
	protocol.WorkflowRunStateContextBuilding:       true,
	protocol.WorkflowRunStateAwaitingClarification: true,
	protocol.WorkflowRunStatePlanning:              true,
	protocol.WorkflowRunStateAwaitingApproval:      true,
	protocol.WorkflowRunStateExecuting:             true,
	protocol.WorkflowRunStateReviewing:             true,
}

func (w *WorkspaceSource) List(ctx context.Context) ([]contract.WorkspaceSummary, error) {
	if w.Registry == nil {
		detail, err := w.describe(ctx, w.App, nil)
		if err != nil {
			return nil, err
		}
		return []contract.WorkspaceSummary{detail.WorkspaceSummary}, nil
	}

	entries, err := w.Registry.List()
	if err != nil {
		return nil, fmt.Errorf("sources: list workspaces: %w", err)
	}
	if len(entries) == 0 {
		detail, err := w.describe(ctx, w.App, nil)
		if err != nil {
			return nil, err
		}
		return []contract.WorkspaceSummary{detail.WorkspaceSummary}, nil
	}

	// Each entry's summary is independent (a non-primary workspace is
	// app.Load'd fresh and its bd/dolt/git health probed in isolation), so
	// describing them sequentially made List O(workspaces) in wall-clock -
	// ~10s for a handful of workspaces, since every entry shells out to
	// `bd list` + `bd ready` and opens a Dolt store (punokawan-d9h). Fan the
	// per-entry work out with a bounded worker pool and reassemble in the
	// registry's original order.
	out := make([]contract.WorkspaceSummary, len(entries))
	const maxConcurrent = 4
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, e := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, e protocol.PanelWorkspaceRegistryEntry) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = w.summaryFor(ctx, e)
		}(i, e)
	}
	wg.Wait()
	return out, nil
}

func (w *WorkspaceSource) Get(ctx context.Context, workspaceID string) (contract.WorkspaceDetail, error) {
	if workspaceID == w.App.Workspace.ID {
		return w.describe(ctx, w.App, nil)
	}
	if w.Registry == nil {
		return contract.WorkspaceDetail{}, fmt.Errorf("sources: workspace %q is not available (only %q is): %w", workspaceID, w.App.Workspace.ID, contract.ErrWorkspaceUnavailable)
	}

	entry, err := w.Registry.Get(workspaceID)
	if err != nil {
		return contract.WorkspaceDetail{}, fmt.Errorf("sources: workspace %q: %w", workspaceID, err)
	}

	other, err := app.Load(entry.Path)
	if err != nil {
		return unavailableDetail(entry, err), nil
	}
	defer other.Close()
	return w.describe(ctx, other, &entry)
}

// summaryFor degrades to an Unavailable summary instead of returning an
// error, since List must isolate one broken workspace from the rest.
func (w *WorkspaceSource) summaryFor(ctx context.Context, entry protocol.PanelWorkspaceRegistryEntry) contract.WorkspaceSummary {
	if entry.Id == w.App.Workspace.ID {
		detail, err := w.describe(ctx, w.App, &entry)
		if err != nil {
			return unavailableDetail(entry, err).WorkspaceSummary
		}
		return detail.WorkspaceSummary
	}

	other, err := app.Load(entry.Path)
	if err != nil {
		return unavailableDetail(entry, err).WorkspaceSummary
	}
	defer other.Close()

	detail, err := w.describe(ctx, other, &entry)
	if err != nil {
		return unavailableDetail(entry, err).WorkspaceSummary
	}
	return detail.WorkspaceSummary
}

func unavailableDetail(entry protocol.PanelWorkspaceRegistryEntry, err error) contract.WorkspaceDetail {
	displayName := entry.Id
	if entry.DisplayName != nil && *entry.DisplayName != "" {
		displayName = *entry.DisplayName
	}
	pinned := entry.Pinned != nil && *entry.Pinned
	msg := err.Error()
	return contract.WorkspaceDetail{
		WorkspaceSummary: contract.WorkspaceSummary{
			ID:           entry.Id,
			Path:         entry.Path,
			DisplayName:  displayName,
			Availability: protocol.PanelSourceHealthAvailabilityUnavailable,
			Pinned:       pinned,
		},
		Health: []protocol.PanelSourceHealth{
			{Source: "workspace", Availability: protocol.PanelSourceHealthAvailabilityUnavailable, Message: &msg, CheckedAt: time.Now().UTC()},
		},
	}
}

// describe builds a WorkspaceDetail for a. entry, when non-nil, supplies
// registry metadata (display name override, pinned) that a itself does
// not know about.
func (w *WorkspaceSource) describe(ctx context.Context, a *app.App, entry *protocol.PanelWorkspaceRegistryEntry) (contract.WorkspaceDetail, error) {
	now := time.Now().UTC()

	// The four heavy probe groups below are mutually independent - knowledge
	// opens a Dolt store, beads shells out to `bd list` + `bd ready`, and git
	// shells out to `git status` per repo - so running them sequentially made a
	// single describe() the sum of all three's latency (~6s for the overview,
	// which describes only the primary workspace). Fan them out and assemble
	// their health slices back in a fixed order afterwards so the output stays
	// deterministic (tests and the health board depend on ordering). adapter
	// health is a cheap PATH lookup, left inline.
	var (
		knowledgeCount  int
		knowledgeHealth []protocol.PanelSourceHealth
		openTasks       int
		blockedTasks    int
		beadsHealth     []protocol.PanelSourceHealth
		activeSessions  int
		workflowHealth  []protocol.PanelSourceHealth
		gitH            []protocol.PanelSourceHealth
		wg              sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if store, err := a.OpenKnowledge(); err != nil {
			knowledgeHealth = append(knowledgeHealth, healthDown("knowledge", err, now))
		} else if recs, err := store.AllWithUpdatedAt(); err != nil {
			knowledgeHealth = append(knowledgeHealth, healthDown("knowledge", err, now))
		} else {
			knowledgeCount = len(recs)
			knowledgeHealth = append(knowledgeHealth, healthOK("knowledge", now))
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if !beads.Available(ctx, a.Supervisor, a.Workspace.Root) {
			msg := "bd binary not found or not initialized in this workspace"
			beadsHealth = append(beadsHealth, protocol.PanelSourceHealth{Source: "bd", Availability: protocol.PanelSourceHealthAvailabilityUnavailable, Message: &msg, CheckedAt: now})
			return
		}
		issues, err := beads.List(ctx, a.Supervisor, a.Workspace.Root, beads.ListOptions{Limit: -1})
		if err != nil {
			beadsHealth = append(beadsHealth, healthDown("bd", err, now))
			return
		}
		// The blocked count must match the status board's boardStatus,
		// which treats an "open" issue bd does not currently consider ready
		// (an unmet "blocks" dependency) as blocked - bd does not flip such
		// an issue's stored Status to "blocked". Counting only
		// Status=="blocked" under-reports against the board and overview, so
		// fold in the same readiness set boardStatus uses (bd ready).
		ready := map[string]bool{}
		if readyIssues, err := beads.Ready(ctx, a.Supervisor, a.Workspace.Root, beads.ReadyOptions{}); err == nil {
			for _, ri := range readyIssues {
				ready[ri.ID] = true
			}
		}
		for _, issue := range issues {
			switch issue.Status {
			case "open":
				openTasks++
				if !ready[issue.ID] {
					blockedTasks++
				}
			case "blocked":
				blockedTasks++
			}
		}
		beadsHealth = append(beadsHealth, healthOK("bd", now))
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if current, err := a.Workflow.Current(); err != nil {
			workflowHealth = append(workflowHealth, healthDown("workflow", err, now))
		} else {
			for _, run := range current {
				if activeStates[run.State] {
					activeSessions++
				}
			}
			workflowHealth = append(workflowHealth, healthOK("workflow", now))
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		gitH = gitHealth(ctx, a, now)
	}()

	wg.Wait()

	// Reassemble in the original sequential order: knowledge, bd, workflow,
	// git, adapters.
	health := make([]protocol.PanelSourceHealth, 0, len(knowledgeHealth)+len(beadsHealth)+len(workflowHealth)+len(gitH)+len(a.AdapterRegistry.Specs()))
	health = append(health, knowledgeHealth...)
	health = append(health, beadsHealth...)
	health = append(health, workflowHealth...)
	health = append(health, gitH...)
	health = append(health, adapterHealth(a, now)...)

	displayName := a.Workspace.Name
	pinned := false
	if entry != nil {
		if entry.DisplayName != nil && *entry.DisplayName != "" {
			displayName = *entry.DisplayName
		}
		pinned = entry.Pinned != nil && *entry.Pinned
	}

	summary := contract.WorkspaceSummary{
		ID:                 a.Workspace.ID,
		Path:               a.Workspace.Root,
		DisplayName:        displayName,
		Availability:       overallAvailability(health),
		RepositoryCount:    len(a.Workspace.Repositories),
		ActiveSessionCount: activeSessions,
		OpenTaskCount:      openTasks,
		BlockedTaskCount:   blockedTasks,
		KnowledgeCount:     knowledgeCount,
		LastActivityAt:     now,
		Pinned:             pinned,
		Primary:            a.Workspace.ID == w.App.Workspace.ID,
	}
	return contract.WorkspaceDetail{WorkspaceSummary: summary, Health: health}, nil
}

// gitHealth reports one protocol.PanelSourceHealth per repository a
// declares, via `git status` through a.Inspector (read-only, per §14.3
// "Git health"). A repository whose path cannot be resolved or whose
// status command fails is reported unavailable rather than failing the
// whole workspace describe call, matching every other per-source health
// check here.
func gitHealth(ctx context.Context, a *app.App, now time.Time) []protocol.PanelSourceHealth {
	health := make([]protocol.PanelSourceHealth, 0, len(a.Workspace.Repositories))
	for _, repo := range a.Workspace.Repositories {
		source := "git:" + repo.ID
		path, err := a.RepoPath(repo.ID)
		if err != nil {
			health = append(health, healthDown(source, err, now))
			continue
		}
		status, err := a.Inspector.Status(ctx, path)
		if err != nil {
			health = append(health, healthDown(source, err, now))
			continue
		}
		branch := status.Branch
		if branch == "" {
			branch = "(detached)"
		}
		msg := fmt.Sprintf("branch=%s clean=%t changed_files=%d", branch, status.Clean, len(status.ChangedFiles))
		health = append(health, protocol.PanelSourceHealth{
			Source:       source,
			Availability: protocol.PanelSourceHealthAvailabilityAvailable,
			Message:      &msg,
			CheckedAt:    now,
		})
	}
	return health
}

// adapterHealth reports one protocol.PanelSourceHealth per configured
// external adapter (Jira, Confluence, ...), per §14.3/§18's "source
// adapter health". It deliberately does not start the adapter process
// (a.AdapterRegistry.Gate would spawn it and perform a live handshake,
// which is neither cheap nor side-effect-free to do on every workspace
// page load): it only checks that the adapter's configured command
// resolves on PATH, an honest but shallower signal than "the adapter is
// actually reachable" - a missing/misconfigured binary is a real and
// common failure this still catches.
func adapterHealth(a *app.App, now time.Time) []protocol.PanelSourceHealth {
	specs := a.AdapterRegistry.Specs()
	ids := make([]string, 0, len(specs))
	for id := range specs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	health := make([]protocol.PanelSourceHealth, 0, len(ids))
	for _, id := range ids {
		source := "adapter:" + id
		if _, err := exec.LookPath(specs[id].Command); err != nil {
			health = append(health, healthDown(source, fmt.Errorf("command %q not found on PATH", specs[id].Command), now))
			continue
		}
		msg := "command resolves on PATH; not started this session, so live connectivity is not verified"
		health = append(health, protocol.PanelSourceHealth{
			Source:       source,
			Availability: protocol.PanelSourceHealthAvailabilityAvailable,
			Message:      &msg,
			CheckedAt:    now,
		})
	}
	return health
}

// overallAvailability rolls per-source health up into one
// WorkspaceAvailability value: available only if every source is; fully
// unavailable only if every source is; otherwise partially_available.
func overallAvailability(health []protocol.PanelSourceHealth) protocol.PanelSourceHealthAvailability {
	if len(health) == 0 {
		return protocol.PanelSourceHealthAvailabilityInvalid
	}
	allUp, allDown := true, true
	for _, h := range health {
		if h.Availability != protocol.PanelSourceHealthAvailabilityAvailable {
			allUp = false
		}
		if h.Availability != protocol.PanelSourceHealthAvailabilityUnavailable {
			allDown = false
		}
	}
	switch {
	case allUp:
		return protocol.PanelSourceHealthAvailabilityAvailable
	case allDown:
		return protocol.PanelSourceHealthAvailabilityUnavailable
	default:
		return protocol.PanelSourceHealthAvailabilityPartiallyAvailable
	}
}
