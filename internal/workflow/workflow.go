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
// §18.1 enumerates the run's possible states but, per §9's workflow
// diagram, does not define a single linear transition graph: clarification
// can loop back to context-building, review can send work back to
// executing, and blocked/failed/cancelled can occur from several states.
// Rather than invent an unstated transition graph, Advance only rejects two
// things it can be confident about: an unrecognized state value, and any
// attempt to leave a terminal state (completed/failed/cancelled) — per
// §9's diagram, delivery is the end of the workflow.
func Advance(run protocol.WorkflowRun, next protocol.WorkflowRunState, note string, now time.Time) (protocol.WorkflowRun, error) {
	if !validStates[next] {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: unknown state %q", run.Id, next)
	}
	if IsTerminal(run.State) {
		return protocol.WorkflowRun{}, fmt.Errorf("workflow: %s: run is already %s and cannot advance further", run.Id, run.State)
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
