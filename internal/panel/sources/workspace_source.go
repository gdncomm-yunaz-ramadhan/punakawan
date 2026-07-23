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
	"github.com/ygrip/punakawan/pkg/protocol"
)

// WorkspaceSource implements contract.WorkspaceReader over the single
// workspace a *app.App was loaded for. Until the workspace registry
// (Phase 1) lands, List and Get can only describe that one workspace -
// this is not a stub, it is the honest scope of "a workspace before a
// registry exists."
type WorkspaceSource struct {
	App *app.App
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
	detail, err := w.Get(ctx, w.App.Workspace.ID)
	if err != nil {
		return nil, err
	}
	return []contract.WorkspaceSummary{detail.WorkspaceSummary}, nil
}

func (w *WorkspaceSource) Get(ctx context.Context, workspaceID string) (contract.WorkspaceDetail, error) {
	if workspaceID != w.App.Workspace.ID {
		return contract.WorkspaceDetail{}, fmt.Errorf("sources: workspace %q is not available (only %q is, until the workspace registry lands)", workspaceID, w.App.Workspace.ID)
	}

	now := time.Now().UTC()
	var health []protocol.PanelSourceHealth

	knowledgeCount := 0
	if store, err := w.App.OpenKnowledge(); err != nil {
		health = append(health, healthDown("knowledge", err, now))
	} else if recs, err := store.AllWithUpdatedAt(); err != nil {
		health = append(health, healthDown("knowledge", err, now))
	} else {
		knowledgeCount = len(recs)
		health = append(health, healthOK("knowledge", now))
	}

	openTasks, blockedTasks := 0, 0
	if !beads.Available(ctx, w.App.Supervisor, w.App.Workspace.Root) {
		msg := "bd binary not found or not initialized in this workspace"
		health = append(health, protocol.PanelSourceHealth{Source: "bd", Availability: protocol.PanelSourceHealthAvailabilityUnavailable, Message: &msg, CheckedAt: now})
	} else if issues, err := beads.List(ctx, w.App.Supervisor, w.App.Workspace.Root, beads.ListOptions{}); err != nil {
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
	if current, err := w.App.Workflow.Current(); err != nil {
		health = append(health, healthDown("workflow", err, now))
	} else {
		for _, run := range current {
			if activeStates[run.State] {
				activeSessions++
			}
		}
		health = append(health, healthOK("workflow", now))
	}

	summary := contract.WorkspaceSummary{
		ID:                 w.App.Workspace.ID,
		Path:               w.App.Workspace.Root,
		DisplayName:        w.App.Workspace.Name,
		Availability:       overallAvailability(health),
		RepositoryCount:    len(w.App.Workspace.Repositories),
		ActiveSessionCount: activeSessions,
		OpenTaskCount:      openTasks,
		BlockedTaskCount:   blockedTasks,
		KnowledgeCount:     knowledgeCount,
		LastActivityAt:     now,
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
