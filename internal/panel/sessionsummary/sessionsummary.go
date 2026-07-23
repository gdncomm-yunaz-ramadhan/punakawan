// Package sessionsummary computes and persists the compact per-run
// summary.yaml Punakawan writes as part of normal run checkpointing, per
// punakawan-panel-implementation-plan.md §8.3. This is not panel-specific
// persistence: the same file backs CLI recovery inspection and audit, with
// the panel simply being another reader of it.
package sessionsummary

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Counts holds the derived counters Build folds into the summary.
// TaskCounts is intentionally workspace-wide rather than run-scoped: bd
// issues carry no run_id today, so a caller cannot honestly attribute a
// count of tasks to one run.
type Counts struct {
	TaskCounts    *protocol.PanelSessionSummaryTaskCounts
	EvidenceCount int
	WarningCount  int
	ErrorCount    int
}

// Build derives a PanelSessionSummary from a WorkflowRun and its counts.
// It performs no I/O: callers gather counts (from the beads, evidence, and
// event-journal readers) and pass them in, keeping this function pure and
// trivially testable.
func Build(run protocol.WorkflowRun, counts Counts) protocol.PanelSessionSummary {
	summary := protocol.PanelSessionSummary{
		Id:          run.Id,
		WorkspaceId: run.Workspace,
		Workflow:    string(run.WorkflowName),
		Status:      string(run.State),
		StartedAt:   run.CreatedAt,
		UpdatedAt:   run.UpdatedAt,
		Initiator:   run.Initiator,
		Objective:   run.Objective,
		TaskCounts:  counts.TaskCounts,
	}
	if run.ActiveRole != nil {
		role := protocol.PanelSessionSummaryActiveRole(*run.ActiveRole)
		summary.ActiveRole = &role
	}
	summary.EvidenceCount = &counts.EvidenceCount
	summary.WarningCount = &counts.WarningCount
	summary.ErrorCount = &counts.ErrorCount
	return summary
}

// dir returns .punakawan/runs/<runID> under workspaceRoot, matching
// internal/evidence.OpenJournal's layout.
func dir(workspaceRoot, runID string) string {
	return filepath.Join(workspaceRoot, ".punakawan", "runs", runID)
}

// WriteYAML persists summary to .punakawan/runs/<summary.Id>/summary.yaml
// under workspaceRoot, creating the run directory if needed. Each call
// overwrites the previous summary: unlike the append-only Journal and
// Ledger, summary.yaml holds only the latest checkpoint's derived state.
func WriteYAML(workspaceRoot string, summary protocol.PanelSessionSummary) error {
	d := dir(workspaceRoot, summary.Id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return fmt.Errorf("sessionsummary: create %s: %w", d, err)
	}

	data, err := yaml.Marshal(summary)
	if err != nil {
		return fmt.Errorf("sessionsummary: encode summary for %s: %w", summary.Id, err)
	}

	path := filepath.Join(d, "summary.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("sessionsummary: write %s: %w", path, err)
	}
	return nil
}

// ReadYAML reads back the summary.yaml written by WriteYAML for runID
// under workspaceRoot.
func ReadYAML(workspaceRoot, runID string) (protocol.PanelSessionSummary, error) {
	path := filepath.Join(dir(workspaceRoot, runID), "summary.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return protocol.PanelSessionSummary{}, fmt.Errorf("sessionsummary: read %s: %w", path, err)
	}

	var summary protocol.PanelSessionSummary
	if err := yaml.Unmarshal(data, &summary); err != nil {
		return protocol.PanelSessionSummary{}, fmt.Errorf("sessionsummary: decode %s: %w", path, err)
	}
	return summary, nil
}
