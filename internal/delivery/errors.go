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
)
