package revision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/ygrip/punakawan/internal/tools"
)

// childTasks is §16's fixed 7-step checklist every revision run's parent
// task decomposes into, in order (each depends on the one before it).
var childTasks = []string{
	"Load base artifact and review snapshot",
	"Resolve related knowledge and evidence",
	"Apply review comments",
	"Validate proposed artifact",
	"Generate diff and resolution report",
	"Await user acceptance",
	"Commit accepted canonical version",
}

// BDDispatcher implements Dispatcher by creating a BD parent task ("Revise
// <artifact> from review <review-id>") plus its 7 sequential children,
// per §16. The parent's BD id is request.RequestID itself - the caller's
// idempotency key doubles as the BD task id, so a repeated Dispatch call
// for the same request is idempotent at the BD layer too: Dispatch checks
// whether the parent already exists before creating anything.
type BDDispatcher struct {
	Supervisor    *tools.Supervisor
	WorkspaceRoot string
}

func (d *BDDispatcher) taskExists(ctx context.Context, id string) (bool, error) {
	res, err := d.Supervisor.Run(ctx, tools.Spec{Name: "bd", Args: []string{"show", id}, Dir: d.WorkspaceRoot})
	if err != nil {
		return false, fmt.Errorf("revision: bd show %s: %w", id, err)
	}
	return res.ExitCode == 0, nil
}

func (d *BDDispatcher) runBD(ctx context.Context, args ...string) error {
	res, err := d.Supervisor.Run(ctx, tools.Spec{Name: "bd", Args: args, Dir: d.WorkspaceRoot})
	if err != nil {
		return fmt.Errorf("revision: bd %v: %w", args, err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("revision: bd %v exited %d: %s", args, res.ExitCode, res.Stderr)
	}
	return nil
}

// Dispatch creates request's durable BD task graph, or, if request.RequestID
// already names an existing parent task, returns the existing run without
// creating anything - the idempotent case per §8.
func (d *BDDispatcher) Dispatch(ctx context.Context, request Request) (RunReference, error) {
	exists, err := d.taskExists(ctx, request.RequestID)
	if err != nil {
		return RunReference{}, err
	}
	if exists {
		return RunReference{RunID: request.RequestID, ParentTaskID: request.RequestID}, nil
	}

	// --force: request.RequestID is a deterministic hash-derived id (the
	// idempotency key), not one shaped like this workspace's configured bd
	// prefix - bd's prefix check exists to keep a database's own IDs
	// self-consistent, which doesn't apply to a caller-supplied
	// idempotency token.
	title := fmt.Sprintf("Revise %s %s from review %s", request.ArtifactType, request.ArtifactID, request.ReviewID)
	if err := d.runBD(ctx, "create", "--force", "--id", request.RequestID, "--title", title, "--type", "task", "--description", agentContractYAML(request)); err != nil {
		return RunReference{}, err
	}

	// bd disallows combining --id with --parent in one create call, so
	// each child is created first, then reparented under the run's
	// parent task via `bd update --parent`.
	prevID := ""
	for i, taskTitle := range childTasks {
		childID := fmt.Sprintf("%s.%d", request.RequestID, i+1)
		if err := d.runBD(ctx, "create", "--force", "--id", childID, "--title", taskTitle, "--type", "task"); err != nil {
			return RunReference{}, err
		}
		if err := d.runBD(ctx, "update", childID, "--parent", request.RequestID); err != nil {
			return RunReference{}, err
		}
		if prevID != "" {
			if err := d.runBD(ctx, "dep", "add", childID, prevID); err != nil {
				return RunReference{}, err
			}
		}
		prevID = childID
	}

	return RunReference{RunID: request.RequestID, ParentTaskID: request.RequestID}, nil
}

// agentContractYAML renders §9's Agent Revision Contract input document
// for the parent task's description - whichever agent claims this task
// reads it to know what to do. review.comments themselves aren't embedded
// here (the task description is meant to be a compact pointer, not a
// duplicate copy of the review) - the agent reads the live review/comments
// via the existing GET /api/v1/reviews/{id} and .../comments endpoints
// using ReviewId below.
func agentContractYAML(request Request) string {
	return fmt.Sprintf(`objective: Revise the artifact using the submitted review.
base_artifact:
  type: %s
  id: %s
  version: %d
  revision_hash: %s
review:
  review_id: %s
  title: %q
  instruction: %q
  comment_count: %d
constraints:
  preserve_unaffected_content: true
  complete_artifact_required: true
  direct_canonical_write_forbidden: true
  unresolved_comments_must_be_reported: true
`, request.ArtifactType, request.ArtifactID, request.BaseVersion, request.BaseRevisionHash,
		request.ReviewID, request.ReviewTitle, request.ReviewInstruction, request.CommentCount)
}

// IdempotencyKey derives §8's submission key - review ID + base revision
// hash + comment snapshot hash + submission sequence - as a stable,
// BD-id-safe token. The same four inputs always produce the same key, so
// a retried submission (network retry, double-click) resolves to the same
// BD task graph instead of creating a competing one.
func IdempotencyKey(reviewID, baseRevisionHash, commentSnapshotHash string, sequence int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", reviewID, baseRevisionHash, commentSnapshotHash, sequence)))
	return "revision-" + hex.EncodeToString(sum[:])[:16]
}
