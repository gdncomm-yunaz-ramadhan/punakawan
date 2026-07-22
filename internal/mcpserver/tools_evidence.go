package mcpserver

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
)

// newEvidenceBundle opens (creating if necessary) the evidence bundle for
// runID/taskID under the workspace root, per §17.2. Several M6 tools write
// into the same bundle across separate calls (task.yaml, diff.patch,
// tests.json, api-diff.json, ...), so each call re-derives the bundle from
// its deterministic path rather than requiring callers to thread a handle
// between tool calls.
func newEvidenceBundle(a *app.App, runID, taskID string) (*evidence.Bundle, error) {
	bundle, err := evidence.NewBundle(a.Workspace.Root, runID, taskID)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open evidence bundle for run %q task %q: %w", runID, taskID, err)
	}
	return bundle, nil
}

// newEvidenceLedger opens (creating if necessary) the structured evidence
// ledger for runID under the workspace root (punokawan-s12). Like
// newEvidenceBundle, it is re-derived per call from a deterministic path.
func newEvidenceLedger(a *app.App, runID string) (*evidence.Ledger, error) {
	ledger, err := evidence.OpenLedger(a.Workspace.Root, runID)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open evidence ledger for run %q: %w", runID, err)
	}
	return ledger, nil
}
