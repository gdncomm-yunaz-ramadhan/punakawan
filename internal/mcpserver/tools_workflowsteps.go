package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/deliverysummary"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// stepDefsForRun loads the workflow definition a run was created from and
// projects its steps into the engine's StepDef shape, plus the allowed-check
// that combines the definition's capability allowlist with the server's
// capability registry. Returns ok=false for an ad hoc run (no definition_ref).
func stepDefsForRun(a *app.App, run protocol.WorkflowRun) (steps []workflow.StepDef, allowed workflow.AllowedFunc, ok bool, err error) {
	if run.DefinitionRef == nil {
		return nil, nil, false, nil
	}
	store, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		return nil, nil, false, err
	}
	def, err := store.Get(run.DefinitionRef.Id)
	if err != nil {
		return nil, nil, false, fmt.Errorf("load definition %q for run: %w", run.DefinitionRef.Id, err)
	}
	for _, s := range def.Steps {
		steps = append(steps, workflow.StepDef{ID: s.ID, Capability: s.Capability, Intent: s.Intent, InputFrom: s.InputFrom})
	}

	allowSet := make(map[string]bool, len(def.AllowedCapabilities))
	for _, c := range def.AllowedCapabilities {
		allowSet[c] = true
	}
	reg := CapabilityRegistry(a)
	allowed = func(capability string) bool {
		// An explicit allowlist is authoritative (its entries were verified as
		// registered when the definition was saved). Without an allowlist, any
		// registered capability is allowed.
		if allowSet[capability] {
			return true
		}
		if len(def.AllowedCapabilities) > 0 {
			return false
		}
		return reg.Has(capability)
	}
	return steps, allowed, true, nil
}

// GetNextWorkflowStepInput is get_next_workflow_step's input.
type GetNextWorkflowStepInput struct {
	RunId string `json:"run_id"`
}

// GetNextWorkflowStepOutput reports the ready and blocked steps of a run.
type GetNextWorkflowStepOutput struct {
	RunId       string              `json:"run_id"`
	AdHoc       bool                `json:"ad_hoc"`
	AllComplete bool                `json:"all_complete"`
	Ready       []workflow.StepView `json:"ready,omitempty"`
	Blocked     []workflow.StepView `json:"blocked,omitempty"`
	Note        string              `json:"note,omitempty"`
}

func getNextWorkflowStepHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetNextWorkflowStepInput) (*mcp.CallToolResult, GetNextWorkflowStepOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetNextWorkflowStepInput) (*mcp.CallToolResult, GetNextWorkflowStepOutput, error) {
		run, err := a.Workflow.Get(in.RunId)
		if err != nil {
			return nil, GetNextWorkflowStepOutput{}, fmt.Errorf("mcpserver: get_next_workflow_step: %w", err)
		}
		steps, allowed, ok, err := stepDefsForRun(a, run)
		if err != nil {
			return nil, GetNextWorkflowStepOutput{}, err
		}
		if !ok {
			return nil, GetNextWorkflowStepOutput{RunId: run.Id, AdHoc: true, Note: "ad hoc run: no workflow steps; record the actual path and call record_work_outcome when done"}, nil
		}
		ready, blocked := workflow.NextSteps(run, steps, allowed)
		out := GetNextWorkflowStepOutput{RunId: run.Id, Ready: ready, Blocked: blocked}
		out.AllComplete = len(ready) == 0 && len(blocked) == 0
		if out.AllComplete {
			out.Note = "all steps complete; call record_work_outcome, then advance_workflow to completed"
		}
		return nil, out, nil
	}
}

// CompleteWorkflowStepInput is complete_workflow_step's input.
type CompleteWorkflowStepInput struct {
	RunId           string   `json:"run_id"`
	StepId          string   `json:"step_id"`
	EvidenceIds     []string `json:"evidence_ids,omitempty" jsonschema:"evidence record ids proving the step was done"`
	DeviationReason string   `json:"deviation_reason,omitempty" jsonschema:"why this step deviated from the definition; required if no evidence is attached"`
	Role            string   `json:"role,omitempty"`
}

func completeWorkflowStepHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CompleteWorkflowStepInput) (*mcp.CallToolResult, GetNextWorkflowStepOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CompleteWorkflowStepInput) (*mcp.CallToolResult, GetNextWorkflowStepOutput, error) {
		run, err := a.Workflow.Get(in.RunId)
		if err != nil {
			return nil, GetNextWorkflowStepOutput{}, fmt.Errorf("mcpserver: complete_workflow_step: %w", err)
		}
		steps, allowed, ok, err := stepDefsForRun(a, run)
		if err != nil {
			return nil, GetNextWorkflowStepOutput{}, err
		}
		if !ok {
			return nil, GetNextWorkflowStepOutput{}, fmt.Errorf("mcpserver: complete_workflow_step: run %q is ad hoc (no workflow steps)", in.RunId)
		}
		now := time.Now().UTC()
		run, err = workflow.CompleteStep(run, in.StepId, in.EvidenceIds, in.DeviationReason, steps, allowed, now)
		if err != nil {
			return nil, GetNextWorkflowStepOutput{}, err
		}
		if err := a.Workflow.Append(run); err != nil {
			return nil, GetNextWorkflowStepOutput{}, fmt.Errorf("mcpserver: persist step completion: %w", err)
		}

		// Emit a run-scoped capability event so the run retains a structured
		// trace (agent-context plan §4.3/§6). Best-effort: a logging failure
		// must not fail the completion that already persisted.
		if events, evErr := workflow.OpenEvents(a.Workspace.Root); evErr == nil {
			result := "completed"
			if in.DeviationReason != "" {
				result = "deviation"
			}
			var capability string
			for _, s := range steps {
				if s.ID == in.StepId {
					capability = s.Capability
					break
				}
			}
			_ = events.Append(workflow.CapabilityEvent{
				RunId: run.Id, StepId: in.StepId, Capability: capability, Role: in.Role, Result: result, At: now,
			})
		}

		ready, blocked := workflow.NextSteps(run, steps, allowed)
		out := GetNextWorkflowStepOutput{RunId: run.Id, Ready: ready, Blocked: blocked}
		out.AllComplete = len(ready) == 0 && len(blocked) == 0
		if out.AllComplete {
			out.Note = "all steps complete; call record_work_outcome, then advance_workflow to completed"
		}
		return nil, out, nil
	}
}

// RecordWorkOutcomeInput is record_work_outcome's input (agent-context plan
// §6.1). An observation here is a traceable input to a learning proposal, not
// canonical knowledge.
type RecordWorkOutcomeInput struct {
	RunId       string   `json:"run_id"`
	Status      string   `json:"status" jsonschema:"one of success|partial|failed"`
	Summary     string   `json:"summary,omitempty"`
	EvidenceIds []string `json:"evidence_ids,omitempty"`
	OutputRefs  []string `json:"output_refs,omitempty"`
	Deviations  []struct {
		StepId           string `json:"step_id"`
		Reason           string `json:"reason"`
		ActualCapability string `json:"actual_capability,omitempty"`
	} `json:"deviations,omitempty"`
	MissingContext []struct {
		Kind string `json:"kind"`
		Key  string `json:"key,omitempty"`
	} `json:"missing_context,omitempty"`
	Observations []struct {
		Kind        string   `json:"kind" jsonschema:"workflow|metadata|knowledge|contradiction|workflow-revision"`
		Summary     string   `json:"summary"`
		EvidenceIds []string `json:"evidence_ids,omitempty"`
	} `json:"observations,omitempty"`
}

func recordWorkOutcomeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RecordWorkOutcomeInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RecordWorkOutcomeInput) (*mcp.CallToolResult, protocol.WorkflowRun, error) {
		status, err := validateOutcomeStatus(in.Status)
		if err != nil {
			return nil, protocol.WorkflowRun{}, err
		}
		run, err := a.Workflow.Get(in.RunId)
		if err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: record_work_outcome: %w", err)
		}

		outcome := protocol.WorkflowRunOutcome{Status: status, EvidenceIds: in.EvidenceIds, OutputRefs: in.OutputRefs}
		// The canonical block is appended to (not instead of) the caller's own
		// summary prose, so test counts/commits/risks/links come from this
		// run's actual records rather than the caller restating them
		// (punokawan-xu7m). prURL/jiraURL are recovered from OutputRefs
		// rather than a new field: a caller recording an outcome already
		// names its PR/Jira URLs there for an unrelated reason.
		prURL, jiraURL := deliverysummary.URLsFromRefs(in.OutputRefs)
		summary := buildDeliverySummary(ctx, a, in.RunId, "", "", "", prURL, jiraURL)
		outcomeSummary := in.Summary
		if section := summary.Section("###"); section != "" {
			if outcomeSummary != "" {
				outcomeSummary += "\n\n" + section
			} else {
				outcomeSummary = section
			}
		}
		if outcomeSummary != "" {
			outcome.Summary = &outcomeSummary
		}
		for _, d := range in.Deviations {
			elem := protocol.WorkflowRunOutcomeDeviationsElem{StepId: d.StepId, Reason: d.Reason}
			if d.ActualCapability != "" {
				ac := d.ActualCapability
				elem.ActualCapability = &ac
			}
			outcome.Deviations = append(outcome.Deviations, elem)
		}
		for _, m := range in.MissingContext {
			elem := protocol.WorkflowRunOutcomeMissingContextElem{Kind: m.Kind}
			if m.Key != "" {
				k := m.Key
				elem.Key = &k
			}
			outcome.MissingContext = append(outcome.MissingContext, elem)
		}
		for _, o := range in.Observations {
			outcome.Observations = append(outcome.Observations, protocol.WorkflowRunOutcomeObservationsElem{Kind: o.Kind, Summary: o.Summary, EvidenceIds: o.EvidenceIds})
		}

		run = workflow.RecordOutcome(run, outcome, time.Now().UTC())
		if err := a.Workflow.Append(run); err != nil {
			return nil, protocol.WorkflowRun{}, fmt.Errorf("mcpserver: persist work outcome: %w", err)
		}
		return nil, run, nil
	}
}

func validateOutcomeStatus(s string) (protocol.WorkflowRunOutcomeStatus, error) {
	switch protocol.WorkflowRunOutcomeStatus(s) {
	case protocol.WorkflowRunOutcomeStatusSuccess,
		protocol.WorkflowRunOutcomeStatusPartial,
		protocol.WorkflowRunOutcomeStatusFailed:
		return protocol.WorkflowRunOutcomeStatus(s), nil
	default:
		return "", errors.New("record_work_outcome: status must be one of success|partial|failed")
	}
}
