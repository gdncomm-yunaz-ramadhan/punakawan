package delivery

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ReconcileWorktrees is Punakawan's startup janitor for lane worktrees
// (PR1 §3.6): it removes only worktrees that are clean, belong to
// Punakawan's own central worktrees root, and whose lane has already
// reached a terminal status (accepted or failed) but still carries a
// linked worktree - i.e. an orphan left behind by an interrupted
// RemoveWorktree call. It never touches a dirty or unrecognized worktree.
// Every step is best-effort and logged: one lane's failure never stops
// reconciliation of the rest, and this method never returns an error a
// caller would need to treat as fatal to startup.
//
// gitops's CLI/task-execution worktrees are intentionally not reconciled
// here: that system persists no task-status store a worktree can be
// checked against for "terminal and recorded", so there is nothing safe
// to automate for it yet.
func (s *Store) ReconcileWorktrees(ctx context.Context) {
	root, err := s.worktreesRoot()
	if err != nil {
		slog.Warn("delivery: reconcile worktrees: resolve worktrees root", "error", err)
		return
	}
	prefix := root + string(filepath.Separator)

	orchestrations, err := s.ListOrchestrations(ctx)
	if err != nil {
		slog.Warn("delivery: reconcile worktrees: list orchestrations", "error", err)
		return
	}

	for _, orch := range orchestrations {
		lanes, err := s.ListLanes(ctx, orch.Id)
		if err != nil {
			slog.Warn("delivery: reconcile worktrees: list lanes", "orchestration_id", orch.Id, "error", err)
			continue
		}

		for _, lane := range lanes {
			if lane.WorktreePath == nil || *lane.WorktreePath == "" {
				continue
			}
			if !strings.HasPrefix(*lane.WorktreePath, prefix) {
				// Never touch a worktree outside Punakawan's own central
				// directory - it is not ours to manage.
				continue
			}
			if lane.Status != protocol.DeliveryLaneStatusAccepted && lane.Status != protocol.DeliveryLaneStatusFailed {
				continue
			}

			if _, err := s.RemoveWorktree(ctx, newID(), orch.Id, lane.Id, lane.Revision); err != nil {
				if errors.Is(err, ErrWorktreeDirty) {
					slog.Info("delivery: reconcile worktrees: leaving dirty orphaned worktree in place",
						"lane_id", lane.Id, "path", *lane.WorktreePath)
					continue
				}
				slog.Warn("delivery: reconcile worktrees: remove orphaned worktree failed",
					"lane_id", lane.Id, "path", *lane.WorktreePath, "error", err)
				continue
			}
			slog.Info("delivery: reconcile worktrees: removed orphaned worktree for terminal lane",
				"lane_id", lane.Id, "path", *lane.WorktreePath)
		}
	}
}
