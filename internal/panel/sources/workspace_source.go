// Package sources implements internal/panel/contract's reader interfaces
// against an already-loaded *app.App, per
// punakawan-panel-implementation-plan.md §8. Each source wraps an existing
// store or CLI wrapper rather than introducing new persistence.
package sources

import (
	"context"
	"fmt"
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

	out := make([]contract.WorkspaceSummary, 0, len(entries))
	for _, e := range entries {
		out = append(out, w.summaryFor(ctx, e))
	}
	return out, nil
}

func (w *WorkspaceSource) Get(ctx context.Context, workspaceID string) (contract.WorkspaceDetail, error) {
	if workspaceID == w.App.Workspace.ID {
		return w.describe(ctx, w.App, nil)
	}
	if w.Registry == nil {
		return contract.WorkspaceDetail{}, fmt.Errorf("sources: workspace %q is not available (only %q is)", workspaceID, w.App.Workspace.ID)
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
	var health []protocol.PanelSourceHealth

	knowledgeCount := 0
	if store, err := a.OpenKnowledge(); err != nil {
		health = append(health, healthDown("knowledge", err, now))
	} else if recs, err := store.AllWithUpdatedAt(); err != nil {
		health = append(health, healthDown("knowledge", err, now))
	} else {
		knowledgeCount = len(recs)
		health = append(health, healthOK("knowledge", now))
	}

	openTasks, blockedTasks := 0, 0
	if !beads.Available(ctx, a.Supervisor, a.Workspace.Root) {
		msg := "bd binary not found or not initialized in this workspace"
		health = append(health, protocol.PanelSourceHealth{Source: "bd", Availability: protocol.PanelSourceHealthAvailabilityUnavailable, Message: &msg, CheckedAt: now})
	} else if issues, err := beads.List(ctx, a.Supervisor, a.Workspace.Root, beads.ListOptions{}); err != nil {
		health = append(health, healthDown("bd", err, now))
	} else {
		for _, issue := range issues {
			switch issue.Status {
			case "open":
				openTasks++
			case "blocked":
				blockedTasks++
			}
		}
		health = append(health, healthOK("bd", now))
	}

	activeSessions := 0
	if current, err := a.Workflow.Current(); err != nil {
		health = append(health, healthDown("workflow", err, now))
	} else {
		for _, run := range current {
			if activeStates[run.State] {
				activeSessions++
			}
		}
		health = append(health, healthOK("workflow", now))
	}

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
	}
	return contract.WorkspaceDetail{WorkspaceSummary: summary, Health: health}, nil
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
