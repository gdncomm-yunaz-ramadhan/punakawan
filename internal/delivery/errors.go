package delivery

import "errors"

var (
	// ErrNotFound is returned when a referenced project, orchestration,
	// or lane id does not exist.
	ErrNotFound = errors.New("delivery: not found")

	// ErrScopeMismatch is returned when a lane id is provided together
	// with an orchestration or project id it does not actually belong
	// to. Calls never fall back to guessing the right scope.
	ErrScopeMismatch = errors.New("delivery: scope mismatch")

	// ErrRevisionConflict is returned when the caller's expected
	// revision no longer matches the current derived revision.
	ErrRevisionConflict = errors.New("delivery: revision conflict")

	// ErrInvalidState is returned when the requested transition is not
	// valid from the entity's current derived status (e.g. cancelling
	// an already-completed orchestration).
	ErrInvalidState = errors.New("delivery: invalid state transition")

	// ErrProjectInactive is returned when a lane is requested against a
	// disabled project.
	ErrProjectInactive = errors.New("delivery: project is not active")

	// ErrCycle is returned when adding a dependency edge would create a
	// cycle in the orchestration's task graph.
	ErrCycle = errors.New("delivery: edge would create a cycle")

	// ErrEvidenceRequired is returned when removing a dependency edge
	// from a task that has already been routed without supplying
	// removal evidence.
	ErrEvidenceRequired = errors.New("delivery: removal evidence is required once the task is routed")

	// ErrUnsafeCrossProjectEdge is returned when a dependency edge
	// between two tasks already routed to different projects is
	// inferred (repository-fact or model-inference origin) rather than
	// explicitly declared (explicit-source or user origin).
	ErrUnsafeCrossProjectEdge = errors.New("delivery: cross-project edge requires an explicit-source or user origin")

	// ErrLaneNotRunnable is returned when a lease is requested against a
	// lane that is not currently runnable (already leased, still
	// waiting/blocked, or terminal).
	ErrLaneNotRunnable = errors.New("delivery: lane is not runnable")

	// ErrProjectAtConcurrencyLimit is returned when granting a lease
	// would exceed one mutating lane per project within an orchestration.
	ErrProjectAtConcurrencyLimit = errors.New("delivery: project already has a mutating lane leased or running")

	// ErrGlobalConcurrencyLimit is returned when granting a lease would
	// bring a new project into mutating work while the maximum number
	// of distinct projects already have a mutating lane in flight,
	// across every orchestration.
	ErrGlobalConcurrencyLimit = errors.New("delivery: maximum number of projects already have mutating work in flight")

	// ErrLeaseTokenMismatch is returned when a heartbeat/complete/reject
	// call presents a lease token that does not match the lane's current
	// lease - it belongs to an expired or already-superseded lease.
	ErrLeaseTokenMismatch = errors.New("delivery: lease token does not match the lane's current lease")

	// ErrLeaseNotExpired is returned when a caller attempts to reclaim a
	// lease that has not actually passed its expiry deadline.
	ErrLeaseNotExpired = errors.New("delivery: lease has not expired")

	// ErrRoleStageOutOfOrder is returned when a role stage is submitted
	// before the stage that must precede it (semar before gareng before
	// petruk before bagong).
	ErrRoleStageOutOfOrder = errors.New("delivery: role stage submitted out of order")

	// ErrRoleStagesIncomplete is returned when CompleteLease is called
	// before Bagong's review has been recorded for the current attempt.
	ErrRoleStagesIncomplete = errors.New("delivery: bagong review has not been recorded yet")

	// ErrGitUnavailable is returned when git cannot be found on PATH.
	ErrGitUnavailable = errors.New("delivery: git is not available on PATH")

	// ErrWorktreeCollision is returned when a lane's deterministic branch
	// name already exists as a local branch in the project's checkout,
	// but it is not this lane's own recorded branch (the lane has not
	// created a worktree yet). An unrelated stale branch of that name is
	// never reused, deleted, or forced past.
	ErrWorktreeCollision = errors.New("delivery: branch name already exists and is not this lane's own")

	// ErrWorktreeDirty is returned when RemoveWorktree is asked to remove
	// a worktree that still has uncommitted changes.
	ErrWorktreeDirty = errors.New("delivery: worktree has uncommitted changes")

	// ErrWorktreeMismatch is returned when a lane's recorded
	// worktree_path no longer looks like a valid linked worktree checked
	// out on the lane's own branch - resume validation failed, so
	// nothing is recreated or deleted automatically.
	ErrWorktreeMismatch = errors.New("delivery: lane's recorded worktree no longer matches its recorded branch")

	// ErrLaneTerminal is returned when a verification dimension, CI
	// check, or review conclusion is recorded against a lane that has
	// already reached a terminal status (accepted or failed) - the
	// attempt it would apply to is over, so recording it would only
	// misattribute new evidence to a closed attempt.
	ErrLaneTerminal = errors.New("delivery: lane has already reached a terminal status")

	// ErrIndependenceRequired is returned when a review conclusion's
	// reviewer_session_id matches the session that implemented the
	// attempt it reviews, and no independence_override_reason was given
	// to explicitly acknowledge that. Independent review is the default
	// requirement; only a stated reason lets a lane's own implementer
	// record its review conclusion.
	ErrIndependenceRequired = errors.New("delivery: review conclusion requires an independent reviewer or an override reason")

	// ErrRepairCyclesExhausted is returned when StartRepairCycle is called
	// against a lane that has already used up its repair-cycle budget. The
	// lane is still escalated (lane.escalated is emitted and the reloaded
	// lane is returned alongside this error) - this is a distinguishable,
	// expected outcome for a caller to report honestly, not a normal
	// repair-cycle start.
	ErrRepairCyclesExhausted = errors.New("delivery: lane has exhausted its repair-cycle budget and was escalated")
)
