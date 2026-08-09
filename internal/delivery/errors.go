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

	// ErrLeaseTokenMismatch is returned when a heartbeat/complete/reject
	// call presents a lease token that does not match the lane's current
	// lease - it belongs to an expired or already-superseded lease.
	ErrLeaseTokenMismatch = errors.New("delivery: lease token does not match the lane's current lease")

	// ErrLeaseNotExpired is returned when a caller attempts to reclaim a
	// lease that has not actually passed its expiry deadline.
	ErrLeaseNotExpired = errors.New("delivery: lease has not expired")
)
