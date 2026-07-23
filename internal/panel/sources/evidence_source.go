package sources

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// EvidenceSource implements contract.EvidenceReader over *app.App's
// per-run evidence.Ledger (the run's evidence manifest).
type EvidenceSource struct {
	App *app.App
}

func (e *EvidenceSource) checkWorkspace(workspaceID string) error {
	if workspaceID != e.App.Workspace.ID {
		return fmt.Errorf("sources: workspace %q is not available (only %q is)", workspaceID, e.App.Workspace.ID)
	}
	return nil
}

func (e *EvidenceSource) List(ctx context.Context, workspaceID, sessionID string) ([]protocol.EvidenceRecord, error) {
	if err := e.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	ledger, err := evidence.OpenLedger(e.App.Workspace.Root, sessionID)
	if err != nil {
		return nil, fmt.Errorf("sources: list evidence for %q: %w", sessionID, err)
	}
	return ledger.List()
}

// Get scans every known run's ledger for evidenceID, since there is no
// global evidence index yet - only a per-run one. This is an O(runs)
// linear search; a later phase should add a workspace-wide evidence index
// if run counts make this too slow (§18's "Read evidence lazily" still
// holds, since each ledger read here is a bounded records.jsonl read).
func (e *EvidenceSource) Get(ctx context.Context, workspaceID, evidenceID string) (protocol.EvidenceRecord, error) {
	if err := e.checkWorkspace(workspaceID); err != nil {
		return protocol.EvidenceRecord{}, err
	}

	runs, err := e.App.Workflow.List()
	if err != nil {
		return protocol.EvidenceRecord{}, fmt.Errorf("sources: get evidence %q: %w", evidenceID, err)
	}

	seen := map[string]bool{}
	for _, run := range runs {
		if seen[run.Id] {
			continue
		}
		seen[run.Id] = true

		ledger, err := evidence.OpenLedger(e.App.Workspace.Root, run.Id)
		if err != nil {
			continue
		}
		recs, err := ledger.List()
		if err != nil {
			continue
		}
		for _, rec := range recs {
			if rec.Id == evidenceID {
				return rec, nil
			}
		}
	}
	return protocol.EvidenceRecord{}, fmt.Errorf("sources: evidence %q not found in any run", evidenceID)
}
