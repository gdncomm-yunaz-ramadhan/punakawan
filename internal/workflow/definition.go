package workflow

import (
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Missing is one piece of context a definition-aware run could not resolve at
// invocation time (agent-context plan §4.1). Kind is "metadata", "knowledge",
// or "input"; Key names the specific item when applicable. A non-empty set
// drives the run into awaiting-clarification.
type Missing struct {
	Kind string
	Key  string
}

// BindDefinition stamps a freshly-created run (from New) with the workflow
// definition it was invoked from (agent-context plan §4.1), replacing the old
// convention of encoding the definition id inside Objective:
//
//   - records the immutable definition_ref (id, revision, content hash);
//   - stores the resolved inputs;
//   - initializes step_progress to "ready" for every step id, in order.
//
// When missing is non-empty it records those entries in the run's
// context_snapshot and advances the run to awaiting-clarification (via the
// valid created -> context-building -> awaiting-clarification path) so a human
// or agent resolves the gap before the work executes. The caller persists the
// returned run via Store.Append.
func BindDefinition(run protocol.WorkflowRun, id string, revision int, contentHash string, inputs map[string]any, stepIDs []string, missing []Missing, now time.Time) (protocol.WorkflowRun, error) {
	run.DefinitionRef = &protocol.WorkflowRunDefinitionRef{Id: id, Revision: revision, ContentHash: contentHash}
	if len(inputs) > 0 {
		run.Inputs = protocol.WorkflowRunInputs(inputs)
	}

	sp := make([]protocol.WorkflowRunStepProgressElem, 0, len(stepIDs))
	for _, sid := range stepIDs {
		sp = append(sp, protocol.WorkflowRunStepProgressElem{
			StepId: sid,
			State:  protocol.WorkflowRunStepProgressElemStateReady,
		})
	}
	if len(sp) > 0 {
		run.StepProgress = sp
	}

	if len(missing) == 0 {
		return run, nil
	}

	elems := make([]protocol.WorkflowRunContextSnapshotMissingElem, 0, len(missing))
	for _, m := range missing {
		elem := protocol.WorkflowRunContextSnapshotMissingElem{Kind: m.Kind}
		if m.Key != "" {
			key := m.Key
			elem.Key = &key
		}
		elems = append(elems, elem)
	}
	run.ContextSnapshot = &protocol.WorkflowRunContextSnapshot{Missing: elems}

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
