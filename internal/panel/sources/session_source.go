package sources

import (
	"context"
	"fmt"
	"sort"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/deliverysummary"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/panel/contract"
	"github.com/ygrip/punakawan/internal/panel/sessionsummary"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SessionSource implements contract.SessionReader over *app.App's
// workflow.Store, evidence journal/ledger, and (for task_counts) bd.
type SessionSource struct {
	App *app.App
}

func (s *SessionSource) checkWorkspace(workspaceID string) error {
	if workspaceID != s.App.Workspace.ID {
		return fmt.Errorf("sources: workspace %q is not available (only %q is): %w", workspaceID, s.App.Workspace.ID, contract.ErrWorkspaceUnavailable)
	}
	return nil
}

// counts derives sessionsummary.Counts for run from its own evidence
// ledger, PR review findings, and event journal.
//
// EvidenceCount/ErrorCount/WarningCount are seeded from deliverysummary.Build
// - the same canonical evidence ledger and PR review reads a PR body, Jira
// comment, and record_work_outcome
// summary for this run draw from - so a run whose canonical data shows a
// failing command or an open risk cannot show a clean session summary here
// while contradicting itself elsewhere. The event journal then adds its own
// execution-level failure/cancellation signal on top, since that is not
// something deliverysummary observes at all.
//
// This deliberately does NOT populate TaskCounts: bd issues carry no run_id,
// so the only bd data available is a workspace-wide snapshot that is
// identical for every run. Attaching that same total to every session
// summary misrepresented workspace-wide counts as per-session ones, so the
// field is now omitted (the protocol keeps it optional) rather than filled
// with a misleading value.
func (s *SessionSource) counts(ctx context.Context, run protocol.WorkflowRun) sessionsummary.Counts {
	var counts sessionsummary.Counts

	var risks []protocol.ReviewFinding
	if s.App.PrReviews != nil {
		if records, err := s.App.PrReviews.ForRun(run.Id); err == nil {
			for _, rec := range records {
				risks = append(risks, rec.Findings...)
			}
		}
	}
	if in, err := deliverysummary.Gather(ctx, deliverysummary.GatherInput{
		WorkspaceRoot: s.App.Workspace.Root,
		RunId:         run.Id,
		Risks:         risks,
	}); err == nil {
		summary := deliverysummary.Build(in)
		counts.EvidenceCount = summary.EvidenceCount
		counts.ErrorCount = summary.CommandsFailed
		counts.WarningCount = len(summary.Risks)
	}

	if journal, err := evidence.OpenJournal(s.App.Workspace.Root, run.Id); err == nil {
		if events, err := journal.List(); err == nil {
			for _, e := range events {
				switch e.Result {
				case protocol.EventResultFailure, protocol.EventResultTimeout:
					counts.ErrorCount++
				case protocol.EventResultCancelled:
					counts.WarningCount++
				}
			}
		}
	}

	return counts
}

func (s *SessionSource) List(ctx context.Context, workspaceID string, filter contract.SessionFilter) ([]protocol.PanelSessionSummary, error) {
	if err := s.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}

	current, err := s.App.Workflow.Current()
	if err != nil {
		return nil, fmt.Errorf("sources: list sessions: %w", err)
	}

	// Workflow.Current returns a map, whose iteration order Go randomizes;
	// collect the filtered runs and sort them newest-first (by UpdatedAt,
	// falling back to CreatedAt) so that both this list and the Overview's
	// derived "recent sessions" are deterministic and actually recent,
	// rather than an arbitrary sample of the map cut off by Limit.
	runs := make([]protocol.WorkflowRun, 0, len(current))
	for _, run := range current {
		if filter.Status != "" && string(run.State) != filter.Status {
			continue
		}
		if filter.Workflow != "" && string(run.WorkflowName) != filter.Workflow {
			continue
		}
		if filter.Role != "" && (run.ActiveRole == nil || string(*run.ActiveRole) != filter.Role) {
			continue
		}
		runs = append(runs, run)
	}
	sort.SliceStable(runs, func(i, j int) bool {
		ti := runs[i].UpdatedAt
		if ti.IsZero() {
			ti = runs[i].CreatedAt
		}
		tj := runs[j].UpdatedAt
		if tj.IsZero() {
			tj = runs[j].CreatedAt
		}
		return ti.After(tj)
	})

	out := []protocol.PanelSessionSummary{}
	for _, run := range runs {
		counts := sessionsummary.Counts{}
		if !filter.SkipCounts {
			counts = s.counts(ctx, run)
		}
		out = append(out, sessionsummary.Build(run, counts))
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func (s *SessionSource) Get(ctx context.Context, workspaceID, sessionID string) (contract.SessionDetail, error) {
	if err := s.checkWorkspace(workspaceID); err != nil {
		return contract.SessionDetail{}, err
	}

	run, err := s.App.Workflow.Get(sessionID)
	if err != nil {
		return contract.SessionDetail{}, fmt.Errorf("sources: get session %q: %w", sessionID, err)
	}

	var timeline []protocol.Event
	if journal, err := evidence.OpenJournal(s.App.Workspace.Root, sessionID); err == nil {
		timeline, _ = journal.List()
	}

	return contract.SessionDetail{
		PanelSessionSummary: sessionsummary.Build(run, s.counts(ctx, run)),
		Timeline:            timeline,
	}, nil
}

// WriteSessionSummary computes run's PanelSessionSummary and persists it
// to .punakawan/runs/<run-id>/summary.yaml, per
// punakawan-panel-implementation-plan.md §8.3 ("the core runtime should
// write summary.yaml as part of normal run checkpointing"). Callers invoke
// this right after appending a WorkflowRun checkpoint (create_workflow_run,
// advance_workflow); it is not panel-specific persistence.
func WriteSessionSummary(ctx context.Context, a *app.App, run protocol.WorkflowRun) error {
	s := &SessionSource{App: a}
	return sessionsummary.WriteYAML(a.Workspace.Root, sessionsummary.Build(run, s.counts(ctx, run)))
}
