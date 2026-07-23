package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// validateRequestedBy checks raw against protocol's requested_by enum
// (semar|gareng|petruk|bagong) up front, returning a guiding error instead of
// letting an unrecognized value flow through as an ApprovalRecordRequestedBy
// the approval record would then carry verbatim (punokawan-mkm). It reuses the
// generated enum validator (protocol.ApprovalRecordRequestedBy.UnmarshalJSON)
// so the accepted set stays tied to the schema rather than duplicated here.
func validateRequestedBy(raw string) (protocol.ApprovalRecordRequestedBy, error) {
	data, _ := json.Marshal(raw)
	var v protocol.ApprovalRecordRequestedBy
	if err := v.UnmarshalJSON(data); err != nil {
		return "", fmt.Errorf("mcpserver: invalid requested_by %q: must be one of semar, gareng, petruk, bagong", raw)
	}
	return v, nil
}

// validateWorkflowName checks raw against protocol's workflow_name enum up
// front (punokawan-4ae), so create_workflow_run rejects an unknown name with a
// guiding message instead of persisting it unchecked (workflow.New stored it
// verbatim). Reuses the generated enum validator.
func validateWorkflowName(raw string) (protocol.WorkflowRunWorkflowName, error) {
	data, _ := json.Marshal(raw)
	var v protocol.WorkflowRunWorkflowName
	if err := v.UnmarshalJSON(data); err != nil {
		return "", fmt.Errorf("mcpserver: invalid workflow_name %q: must be one of feature-delivery, requirement-review, browser-flow-capture, implementation-only, final-review", raw)
	}
	return v, nil
}

// validateCapsuleRole checks raw against protocol's context-capsule role enum
// up front (punokawan-4ae), reusing the generated enum validator.
func validateCapsuleRole(raw string) (protocol.ContextCapsuleRole, error) {
	data, _ := json.Marshal(raw)
	var v protocol.ContextCapsuleRole
	if err := v.UnmarshalJSON(data); err != nil {
		return "", fmt.Errorf("mcpserver: invalid role %q: must be one of gareng, petruk, bagong", raw)
	}
	return v, nil
}

// recordPartialFailure decides how a sub-write failure in a multi-write tool
// (update_jira_task_progress, request_jira_clarification, submit_jira_assessment)
// is surfaced, per punokawan-4tw. If no earlier write in the same call
// succeeded, nothing was applied, so err is returned unchanged and the caller
// sees an ordinary failure. If an earlier write did succeed, this is a partial
// success: the failure is recorded on the output's failed_step/failed_error
// fields and nil is returned, so the handler returns a NON-error result the
// caller can read to see exactly what completed - and therefore not blindly
// re-run the whole tool, which would re-apply the already-applied non-dedup
// writes (addJiraComment/addWorklog) and duplicate them. The failed step is
// separately recorded in the adapter sync queue for out-of-band retry.
func recordPartialFailure(failedStep, failedError *string, anySucceeded bool, step string, err error) error {
	if !anySucceeded {
		return err
	}
	*failedStep = step
	*failedError = err.Error()
	return nil
}
