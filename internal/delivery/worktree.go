// worktree.go gives each delivery lane its own isolated git worktree and
// branch, created from its project's configured base branch, and lets a
// lane commit and push from that worktree.
//
// Every git invocation here either only reads or advances
// remote-tracking state in the project's base checkout (fetch --prune,
// rev-parse against refs/remotes/<remote>/<branch>, show-ref against
// refs/heads/<branch>) or targets the lane's own linked worktree
// directory once one exists (add, remove, add -A, commit, push). None
// of that ever touches the base checkout's own tracked working-tree
// files or moves its current branch, so the base checkout is never
// required to be clean, and it is safe for many lanes to share one base
// checkout at once.
package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// worktreesRoot returns the local directory lane worktrees are created
// under: a sibling "<db-file>-worktrees" directory next to whatever
// database file this Store's kernel was opened against, the same shape
// as artifacts.go's artifactsRoot - colocated with this Store's own db
// file rather than a single global directory, so each opened database
// (each test, each server instance) gets its own isolated worktree tree.
func (s *Store) worktreesRoot() (string, error) {
	root := s.db.Path() + "-worktrees"
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("delivery: create worktrees root: %w", err)
	}
	return root, nil
}

// laneWorktreeDir computes a lane's worktree directory: both projectID
// and laneID are ULIDs, which are filesystem-safe on every OS including
// Windows (no colons or slashes), so no further sanitizing is needed.
func laneWorktreeDir(root, projectID, laneID string) string {
	return filepath.Join(root, projectID, laneID)
}

var branchSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

const laneBranchSlugMaxLen = 40

// laneBranchName computes the deterministic branch name a lane's
// worktree checks out: a lowercased slug of the parent task's title
// (non-alphanumeric runs collapsed to a single hyphen, leading/trailing
// hyphens trimmed, truncated to a reasonable length) followed by the
// lane's own id's last 8 characters. ULIDs are fixed-width and
// lexicographically ordered, so those trailing characters are the most
// locally-unique part of the id - appending them keeps two lanes
// delivering similarly (or identically) titled tasks from computing the
// same branch name.
func laneBranchName(parentTaskTitle, laneID string) string {
	slug := strings.ToLower(parentTaskTitle)
	slug = branchSlugPattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > laneBranchSlugMaxLen {
		slug = strings.Trim(slug[:laneBranchSlugMaxLen], "-")
	}

	shortID := strings.ToLower(laneID)
	if len(shortID) > 8 {
		shortID = shortID[len(shortID)-8:]
	}

	if slug == "" {
		return "punakawan/" + shortID
	}
	return "punakawan/" + slug + "-" + shortID
}

// CreateWorktree gives laneID its own linked git worktree and branch,
// forked from its project's configured base branch. Calling it again
// for a lane that already has a worktree (still present on disk) is a
// no-op resume; calling it again for a lane whose worktree was removed
// but whose branch survives (see RemoveWorktree) checks that same
// branch back out rather than creating a new one.
func (s *Store) CreateWorktree(ctx context.Context, idempotencyKey, orchestrationID, laneID string, expectedRevision int) (*protocol.DeliveryLane, error) {
	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return nil, err
	}
	if lane.ParentTaskId == nil || *lane.ParentTaskId == "" {
		return nil, fmt.Errorf("delivery: lane %s has not been routed to a parent task yet; cannot create a worktree", laneID)
	}
	if lane.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}

	project, err := s.GetProject(ctx, lane.ProjectId)
	if err != nil {
		return nil, err
	}
	profile, err := s.GetDeliveryProfile(ctx, project.Id)
	if err != nil {
		return nil, err
	}
	if profile.LocalPath == nil || *profile.LocalPath == "" {
		return nil, fmt.Errorf("delivery: project %s has no local_path configured in its delivery profile", project.Id)
	}

	// Never consult a configured absolute git path from anywhere -
	// always resolve "git" by name only, so a stale absolute path
	// setting elsewhere can never be silently used instead of whatever
	// git the current environment actually provides.
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitUnavailable, err)
	}

	root, err := s.worktreesRoot()
	if err != nil {
		return nil, err
	}
	laneDir := laneWorktreeDir(root, project.Id, laneID)

	sup := tools.New(*profile.LocalPath, laneDir)
	inspector := gitops.NewInspector(sup)

	// Resume path: a worktree is already recorded for this lane. Verify
	// it is still what we expect and, if so, treat this as a harmless
	// no-op rather than touching anything.
	if lane.WorktreePath != nil && *lane.WorktreePath != "" {
		isWorktree, wtErr := inspector.IsWorktree(ctx, *lane.WorktreePath)
		if wtErr != nil || !isWorktree {
			return nil, fmt.Errorf("%w: %s is not a valid linked worktree (%v)", ErrWorktreeMismatch, *lane.WorktreePath, wtErr)
		}
		current, branchErr := inspector.CurrentBranch(ctx, *lane.WorktreePath)
		wantBranch := ""
		if lane.Branch != nil {
			wantBranch = *lane.Branch
		}
		if branchErr != nil || current != wantBranch {
			return nil, fmt.Errorf("%w: %s is checked out on %q (err=%v), want lane branch %q", ErrWorktreeMismatch, *lane.WorktreePath, current, branchErr, wantBranch)
		}
		return lane, nil
	}

	// Recreate path: the branch survived a prior RemoveWorktree; check
	// the same branch back out into a fresh worktree at the same path.
	if lane.Branch != nil && *lane.Branch != "" {
		branch := *lane.Branch
		baseSha := ""
		if lane.BaseSha != nil {
			baseSha = *lane.BaseSha
		}
		baseRemote := ""
		if lane.BaseRemote != nil {
			baseRemote = *lane.BaseRemote
		}
		if err := os.MkdirAll(filepath.Dir(laneDir), 0o700); err != nil {
			return nil, fmt.Errorf("delivery: create lane worktree parent directory: %w", err)
		}

		writeErr := s.db.Write(ctx, idempotencyKey, "create worktree "+laneID, func(tx *sql.Tx) error {
			events, err := loadEventsTx(ctx, tx, orchestrationID)
			if err != nil {
				return err
			}
			current, err := reduceLane(orchestrationID, laneID, events)
			if err != nil {
				return err
			}
			if current.Revision != expectedRevision {
				return ErrRevisionConflict
			}

			res, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"worktree", "add", laneDir, branch}, Dir: *profile.LocalPath})
			if err != nil {
				return fmt.Errorf("delivery: git worktree add: %w", err)
			}
			if res.ExitCode != 0 {
				return fmt.Errorf("delivery: git worktree add: %s", res.Stderr)
			}

			payload, err := json.Marshal(map[string]interface{}{
				"worktree_path": laneDir, "branch": branch, "base_sha": baseSha, "base_remote": baseRemote,
			})
			if err != nil {
				return err
			}
			return insertEvent(ctx, tx, eventRow{
				ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
				Type: string(protocol.DeliveryEventTypeLaneWorktreeCreated), Payload: string(payload),
				Sequence: len(events), OccurredAt: time.Now().UTC(),
			})
		})
		if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
			return nil, writeErr
		}
		return s.GetLane(ctx, orchestrationID, laneID)
	}

	// First-create path: neither a branch nor a worktree has ever been
	// recorded for this lane yet.
	remote := "origin"
	if profile.CanonicalRemote != nil && *profile.CanonicalRemote != "" {
		remote = *profile.CanonicalRemote
	}

	fetchRes, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"fetch", "--prune", remote}, Dir: *profile.LocalPath})
	if err != nil {
		return nil, fmt.Errorf("delivery: git fetch --prune %s: %w", remote, err)
	}
	if fetchRes.ExitCode != 0 {
		return nil, fmt.Errorf("delivery: git fetch --prune %s: %s", remote, fetchRes.Stderr)
	}

	baseRef := "refs/remotes/" + remote + "/" + profile.BaseBranch
	revRes, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"rev-parse", baseRef}, Dir: *profile.LocalPath})
	if err != nil {
		return nil, fmt.Errorf("delivery: git rev-parse %s: %w", baseRef, err)
	}
	if revRes.ExitCode != 0 {
		return nil, fmt.Errorf("delivery: could not resolve base branch %q on remote %q (missing ref %s): %s", profile.BaseBranch, remote, baseRef, revRes.Stderr)
	}
	baseSHA := strings.TrimSpace(string(revRes.Stdout))

	parentTask, err := s.GetParentTask(ctx, orchestrationID, *lane.ParentTaskId)
	if err != nil {
		return nil, err
	}
	branch := laneBranchName(parentTask.Title, laneID)

	showRefRes, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"show-ref", "--verify", "--quiet", "refs/heads/" + branch}, Dir: *profile.LocalPath})
	if err != nil {
		return nil, fmt.Errorf("delivery: git show-ref refs/heads/%s: %w", branch, err)
	}
	if showRefRes.ExitCode == 0 {
		return nil, fmt.Errorf("%w: branch %q already exists in %s", ErrWorktreeCollision, branch, *profile.LocalPath)
	}

	if err := os.MkdirAll(filepath.Dir(laneDir), 0o700); err != nil {
		return nil, fmt.Errorf("delivery: create lane worktree parent directory: %w", err)
	}

	writeErr := s.db.Write(ctx, idempotencyKey, "create worktree "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}

		res, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"worktree", "add", "-b", branch, laneDir, baseSHA}, Dir: *profile.LocalPath})
		if err != nil {
			return fmt.Errorf("delivery: git worktree add: %w", err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("delivery: git worktree add: %s", res.Stderr)
		}

		payload, err := json.Marshal(map[string]interface{}{
			"worktree_path": laneDir, "branch": branch, "base_sha": baseSHA, "base_remote": remote,
		})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneWorktreeCreated), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return nil, writeErr
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}

// RemoveWorktree removes laneID's current linked worktree from disk,
// leaving its branch/base_sha/base_remote recorded so a later
// CreateWorktree call checks the same branch back out instead of
// creating a new one. It refuses to remove anything that is not
// independently confirmed to be both a recognized linked worktree and
// clean - git's own dirty-check on `worktree remove` is a second line
// of defense here, not the only one, since --force is never passed.
func (s *Store) RemoveWorktree(ctx context.Context, idempotencyKey, orchestrationID, laneID string, expectedRevision int) (*protocol.DeliveryLane, error) {
	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return nil, err
	}
	if lane.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	if lane.WorktreePath == nil || *lane.WorktreePath == "" {
		return lane, nil
	}

	project, err := s.GetProject(ctx, lane.ProjectId)
	if err != nil {
		return nil, err
	}
	profile, err := s.GetDeliveryProfile(ctx, project.Id)
	if err != nil {
		return nil, err
	}
	if profile.LocalPath == nil || *profile.LocalPath == "" {
		return nil, fmt.Errorf("delivery: project %s has no local_path configured in its delivery profile", project.Id)
	}

	worktreePath := *lane.WorktreePath
	sup := tools.New(*profile.LocalPath, worktreePath)
	inspector := gitops.NewInspector(sup)

	isWorktree, err := inspector.IsWorktree(ctx, worktreePath)
	if err != nil || !isWorktree {
		return nil, fmt.Errorf("%w: %s is not a valid linked worktree (%v)", ErrWorktreeMismatch, worktreePath, err)
	}

	status, err := inspector.Status(ctx, worktreePath)
	if err != nil {
		return nil, fmt.Errorf("delivery: check worktree status: %w", err)
	}
	if !status.Clean {
		return nil, ErrWorktreeDirty
	}

	writeErr := s.db.Write(ctx, idempotencyKey, "remove worktree "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}

		res, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"worktree", "remove", worktreePath}, Dir: *profile.LocalPath})
		if err != nil {
			return fmt.Errorf("delivery: git worktree remove: %w", err)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("delivery: git worktree remove: %s", res.Stderr)
		}

		pruneRes, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"worktree", "prune"}, Dir: *profile.LocalPath})
		if err != nil {
			return fmt.Errorf("delivery: git worktree prune: %w", err)
		}
		if pruneRes.ExitCode != 0 {
			return fmt.Errorf("delivery: git worktree prune: %s", pruneRes.Stderr)
		}

		payload, err := json.Marshal(map[string]interface{}{"removed_path": worktreePath})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneWorktreeRemoved), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return nil, writeErr
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}

// CommitLane stages and commits every pending change in laneID's own
// worktree. It refuses to run unless that worktree is actually checked
// out on the lane's own recorded branch, so a lane never commits onto
// whatever branch happens to be checked out for some other reason.
func (s *Store) CommitLane(ctx context.Context, orchestrationID, laneID, message string) (string, error) {
	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return "", err
	}
	if lane.WorktreePath == nil || *lane.WorktreePath == "" || lane.Branch == nil || *lane.Branch == "" {
		return "", fmt.Errorf("delivery: lane %s has no active worktree to commit in", laneID)
	}

	sup := tools.New(*lane.WorktreePath)
	inspector := gitops.NewInspector(sup)

	current, err := inspector.CurrentBranch(ctx, *lane.WorktreePath)
	if err != nil {
		return "", fmt.Errorf("delivery: determine current branch of %s: %w", *lane.WorktreePath, err)
	}
	if current != *lane.Branch {
		return "", fmt.Errorf("delivery: worktree %s is checked out on %q, expected lane branch %q; refusing to commit", *lane.WorktreePath, current, *lane.Branch)
	}

	addRes, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"add", "-A"}, Dir: *lane.WorktreePath})
	if err != nil {
		return "", fmt.Errorf("delivery: git add -A: %w", err)
	}
	if addRes.ExitCode != 0 {
		return "", fmt.Errorf("delivery: git add -A: %s", addRes.Stderr)
	}

	commitRes, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"commit", "-m", message}, Dir: *lane.WorktreePath})
	if err != nil {
		return "", fmt.Errorf("delivery: git commit: %w", err)
	}
	if commitRes.ExitCode != 0 {
		return "", fmt.Errorf("delivery: git commit: %s", commitRes.Stderr)
	}

	sha, err := inspector.HeadSHA(ctx, *lane.WorktreePath)
	if err != nil {
		return "", fmt.Errorf("delivery: resolve resulting commit sha: %w", err)
	}
	return sha, nil
}

// PushLane pushes laneID's own branch from its worktree to its
// project's canonical remote (or "origin" if none is configured). This
// never passes --force and never accepts a way for a caller to request
// it: a lane's branch is meant to be a durable, append-only history that
// a reviewer or another process may already be building on top of, and
// force-pushing over that is not something this method needs to recover
// from within this task's scope.
func (s *Store) PushLane(ctx context.Context, orchestrationID, laneID string) (string, error) {
	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return "", err
	}
	if lane.WorktreePath == nil || *lane.WorktreePath == "" || lane.Branch == nil || *lane.Branch == "" {
		return "", fmt.Errorf("delivery: lane %s has no active worktree to push from", laneID)
	}

	project, err := s.GetProject(ctx, lane.ProjectId)
	if err != nil {
		return "", err
	}
	profile, err := s.GetDeliveryProfile(ctx, project.Id)
	if err != nil {
		return "", err
	}
	remote := "origin"
	if profile.CanonicalRemote != nil && *profile.CanonicalRemote != "" {
		remote = *profile.CanonicalRemote
	}

	sup := tools.New(*lane.WorktreePath)
	inspector := gitops.NewInspector(sup)

	current, err := inspector.CurrentBranch(ctx, *lane.WorktreePath)
	if err != nil {
		return "", fmt.Errorf("delivery: determine current branch of %s: %w", *lane.WorktreePath, err)
	}
	if current != *lane.Branch {
		return "", fmt.Errorf("delivery: worktree %s is checked out on %q, expected lane branch %q; refusing to push", *lane.WorktreePath, current, *lane.Branch)
	}

	res, err := sup.Run(ctx, tools.Spec{Name: "git", Args: []string{"push", remote, *lane.Branch}, Dir: *lane.WorktreePath})
	if err != nil {
		return "", fmt.Errorf("delivery: git push %s %s: %w", remote, *lane.Branch, err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("delivery: git push %s %s: %s", remote, *lane.Branch, strings.TrimSpace(string(res.Stderr)))
	}
	return *lane.Branch, nil
}

// RunInLane runs one command scoped strictly to laneID's own worktree:
// the supervisor's only allowed working directory is that worktree, so
// the command can never read, write, or execute anything outside its
// own lease's scope, regardless of what arguments it is given. Requires
// leaseToken to match the lane's current lease, the same ownership
// check heartbeat/complete/reject already make - only the worker that
// actually holds this lane's lease can run anything in it.
func (s *Store) RunInLane(ctx context.Context, orchestrationID, laneID, leaseToken, name string, args []string, timeout time.Duration) (*tools.Result, error) {
	lane, err := s.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return nil, err
	}
	if lane.LeaseToken == nil || *lane.LeaseToken != leaseToken {
		return nil, ErrLeaseTokenMismatch
	}
	if lane.WorktreePath == nil || *lane.WorktreePath == "" {
		return nil, fmt.Errorf("delivery: lane %s has no active worktree to run in", laneID)
	}

	sup := tools.New(*lane.WorktreePath)
	res, err := sup.Run(ctx, tools.Spec{Name: name, Args: args, Dir: *lane.WorktreePath, Timeout: timeout})
	if err != nil {
		return nil, fmt.Errorf("delivery: run %s in lane %s: %w", name, laneID, err)
	}
	return res, nil
}
