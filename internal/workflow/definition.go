package workflow

import (
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// StampContext binds a freshly-created run (from New) to the workflow
// definition and prepared context it was invoked with (agent-context plan
// §4.1), replacing the old convention of encoding the definition id inside
// Objective. It is the single run-stamping path shared by the panel invoke
// route and the prepare_work_context MCP tool, so the two cannot diverge
// (plan §5.2):
//
//   - defRef, when non-nil, records the immutable definition reference
//     (id, revision, content hash);
//   - inputs stores the resolved workflow inputs;
//   - stepIDs initialize step_progress to "ready", in order;
//   - snapshot, when non-nil, is the bounded context snapshot to attach.
//
// When the snapshot reports missing context, the run is advanced to
// awaiting-clarification (via the valid created -> context-building ->
// awaiting-clarification path) so a human or agent resolves the gap before the
// work executes. The caller persists the returned run via Store.Append.
func StampContext(
	run protocol.WorkflowRun,
	defRef *protocol.WorkflowRunDefinitionRef,
	inputs map[string]any,
	stepIDs []string,
	snapshot *protocol.WorkflowRunContextSnapshot,
	now time.Time,
) (protocol.WorkflowRun, error) {
	if defRef != nil {
		run.DefinitionRef = defRef
	}
	if len(inputs) > 0 {
		run.Inputs = protocol.WorkflowRunInputs(inputs)
	}

	if len(stepIDs) > 0 {
		sp := make([]protocol.WorkflowRunStepProgressElem, 0, len(stepIDs))
		for _, sid := range stepIDs {
			sp = append(sp, protocol.WorkflowRunStepProgressElem{
				StepId: sid,
				State:  protocol.WorkflowRunStepProgressElemStateReady,
			})
		}
		run.StepProgress = sp
	}

	if snapshot != nil {
		run.ContextSnapshot = snapshot
	}

	if snapshot == nil || len(snapshot.Missing) == 0 {
		return run, nil
	}

	// Missing context => awaiting-clarification. The state graph has no direct
	// created -> awaiting-clarification edge, so walk the two valid hops,
	// leaving an honest checkpoint trail.
	run, err := Advance(run, protocol.WorkflowRunStateContextBuilding, "definition-aware run: preparing context", now)
	if err != nil {
		return protocol.WorkflowRun{}, err
	}
	run, err = Advance(run, protocol.WorkflowRunStateAwaitingClarification, "missing required context at invocation", now)
	if err != nil {
		return protocol.WorkflowRun{}, err
	}
	return run, nil
}
