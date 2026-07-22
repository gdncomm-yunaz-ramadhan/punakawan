package workflow

import (
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// validStates is every state §18.1's run state machine enumerates (matching
// protocol/workflow.schema.json's "state" enum exactly).
var validStates = map[protocol.WorkflowRunState]bool{
	protocol.WorkflowRunStateCreated:               true,
	protocol.WorkflowRunStateContextBuilding:       true,
	protocol.WorkflowRunStateAwaitingClarification: true,
	protocol.WorkflowRunStatePlanning:              true,
	protocol.WorkflowRunStateAwaitingApproval:      true,
	protocol.WorkflowRunStateExecuting:             true,
	protocol.WorkflowRunStateReviewing:             true,
	protocol.WorkflowRunStateBlocked:               true,
	protocol.WorkflowRunStateCompleted:             true,
	protocol.WorkflowRunStateFailed:                true,
	protocol.WorkflowRunStateCancelled:             true,
}

// terminalStates are the states §9's workflow ends in. Once a run reaches
// one of these, Advance refuses to move it further.
var terminalStates = map[protocol.WorkflowRunState]bool{
	protocol.WorkflowRunStateCompleted: true,
	protocol.WorkflowRunStateFailed:    true,
	protocol.WorkflowRunStateCancelled: true,
}

// IsTerminal reports whether state is one a run does not leave.
func IsTerminal(state protocol.WorkflowRunState) bool {
	return terminalStates[state]
}

// forwardTransitions is the non-escape-hatch edges of §9's workflow
// diagram, mapped onto the state names protocol/workflow.schema.json
// enumerates: intake (created) -> context dossier (context-building) ->
// either the clarification loop (awaiting-clarification, which loops back
// to context-building per §9.2) or straight to planning -> the delivery
// approval gate (awaiting-approval) -> task execution (executing) ->
// Bagong's independent review (reviewing), which either sends work back to
// executing ("changes required", §9's diagram) or reaches completed.
//
// This is deliberately not a single linear sequence — §9's diagram has two
// loops (clarification back to context-building, review back to
// executing) — but it is still a fixed graph: e.g. created cannot reach
// completed without passing through it, which is the concrete gap this
// closes (see punokawan-e8x). blocked/failed/cancelled are handled
// separately by isValidTransition below rather than listed as a
// destination here, since §9's diagram shows them reachable from
// multiple stages without depicting a single source state for each.
var forwardTransitions = map[protocol.WorkflowRunState][]protocol.WorkflowRunState{
	protocol.WorkflowRunStateCreated:               {protocol.WorkflowRunStateContextBuilding},
	protocol.WorkflowRunStateContextBuilding:       {protocol.WorkflowRunStateAwaitingClarification, protocol.WorkflowRunStatePlanning},
	protocol.WorkflowRunStateAwaitingClarification: {protocol.WorkflowRunStateContextBuilding},
	protocol.WorkflowRunStatePlanning:              {protocol.WorkflowRunStateAwaitingApproval},
	protocol.WorkflowRunStateAwaitingApproval:      {protocol.WorkflowRunStateExecuting, protocol.WorkflowRunStatePlanning},
	protocol.WorkflowRunStateExecuting:             {protocol.WorkflowRunStateReviewing},
	protocol.WorkflowRunStateReviewing:             {protocol.WorkflowRunStateExecuting, protocol.WorkflowRunStateCompleted},
	// blocked has no fixed source stage (§9's diagram shows it reachable
	// from execution's mechanical checks and, in practice, any stage that
	// can stall on an external dependency), so resuming from it can return
	// to any active stage rather than one fixed successor.
	protocol.WorkflowRunStateBlocked: {
		protocol.WorkflowRunStateContextBuilding,
		protocol.WorkflowRunStateAwaitingClarification,
		protocol.WorkflowRunStatePlanning,
		protocol.WorkflowRunStateAwaitingApproval,
		protocol.WorkflowRunStateExecuting,
		protocol.WorkflowRunStateReviewing,
	},
}

// isValidTransition reports whether next is reachable from from: either a
// listed forward edge, or one of the escape hatches (blocked, failed,
// cancelled) that §9's diagram shows reachable from multiple stages
// without a single depicted source.
func isValidTransition(from, next protocol.WorkflowRunState) bool {
	if next == protocol.WorkflowRunStateBlocked ||
		next == protocol.WorkflowRunStateFailed ||
		next == protocol.WorkflowRunStateCancelled {
		return true
	}
	for _, allowed := range forwardTransitions[from] {
		if allowed == next {
			return true
		}
	}
	return false
}

// New builds the initial state of a run: state "created" with a single
// checkpoint recording its creation, per §18.1.
func New(id, workspaceID string, workflowName protocol.WorkflowRunWorkflowName, now time.Time) protocol.WorkflowRun {
	return protocol.WorkflowRun{
		Id:           id,
		Workspace:    workspaceID,
		WorkflowName: workflowName,
		State:        protocol.WorkflowRunStateCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
		Checkpoints: []protocol.WorkflowRunCheckpointsElem{
			{State: string(protocol.WorkflowRunStateCreated), At: now},
		},
	}
}

// Advance moves run to next, appending a checkpoint and updating
// UpdatedAt. It returns the updated run; the caller is responsible for
// persisting it via Store.Append.
//
// §18.1 enumerates the run's possible states, and §9's workflow diagram
// (mapped onto those states by forwardTransitions above) is not a single
// linear sequence: clarification can loop back to context-building,
// review can send work back to executing, and blocked/failed/cancelled
// can occur from several states. Advance still enforces that graph rather
// than accepting any non-terminal state as a valid next step — e.g.
// created cannot jump straight to completed, skipping every stage in
// between (punokawan-e8x).
func Advance(run protocol.WorkflowRun, next protocol.WorkflowRunState, note string, now time.Time) (protocol.WorkflowRun, error) {
	if !validStates[next] {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: unknown state %q", run.Id, next)
	}
	if IsTerminal(run.State) {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: run is already %s and cannot advance further", run.Id, run.State)
	}
	if !isValidTransition(run.State, next) {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: cannot advance from %s to %s; valid next states from %s are %v (or blocked/failed/cancelled at any point)",
			run.Id, run.State, next, run.State, forwardTransitions[run.State])
	}

	checkpoint := protocol.WorkflowRunCheckpointsElem{State: string(next), At: now}
	if note != "" {
		checkpoint.Note = &note
	}

	run.State = next
	run.UpdatedAt = now
	run.Checkpoints = append(run.Checkpoints, checkpoint)
	return run, nil
}
