package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/tasks"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// TaskGraphDependencyInput is one dependency edge within a
// SubmitTaskGraphInput, referencing a sibling item by LocalKey.
type TaskGraphDependencyInput struct {
	LocalKey string `json:"local_key" jsonschema:"local_key of another item in the same call"`
	Type     string `json:"type" jsonschema:"one of blocks|discovered-from|requires"`
}

// TaskGraphItemInput is one task to create as part of a submit_task_graph
// call, mirroring protocol.TaskContract's fields (§10.3) plus a LocalKey
// used only to express DependsOn edges within this call.
type TaskGraphItemInput struct {
	LocalKey                  string                     `json:"local_key" jsonschema:"identifies this item within this call only, for depends_on references"`
	RequirementId             string                     `json:"requirement_id" jsonschema:"id of the parent requirement knowledge record"`
	TaskId                    string                     `json:"task_id" jsonschema:"stable task id"`
	Repository                string                     `json:"repository"`
	Scope                     string                     `json:"scope"`
	ExpectedFilesOrComponents []string                   `json:"expected_files_or_components,omitempty"`
	AcceptanceCriteria        []string                   `json:"acceptance_criteria" jsonschema:"at least one entry is required"`
	TestRequirements          []string                   `json:"test_requirements,omitempty"`
	RequiredEvidence          []string                   `json:"required_evidence,omitempty"`
	RiskClassification        string                     `json:"risk_classification,omitempty" jsonschema:"one of low|medium|high"`
	ApprovalRequired          *bool                      `json:"approval_required,omitempty"`
	DefinitionOfDone          string                     `json:"definition_of_done"`
	BeadsParent               string                     `json:"beads_parent,omitempty"`
	BeadsType                 string                     `json:"beads_type,omitempty"`
	BeadsLabels               []string                   `json:"beads_labels,omitempty"`
	DependsOn                 []TaskGraphDependencyInput `json:"depends_on,omitempty"`
}

// SubmitTaskGraphInput is submit_task_graph's input.
type SubmitTaskGraphInput struct {
	Items []TaskGraphItemInput `json:"items" jsonschema:"the decomposed task graph; the calling role does the decomposition, this tool only creates and wires it"`
}

// TaskGraphResultOutput pairs a created TaskContract with the LocalKey it
// was created from.
type TaskGraphResultOutput struct {
	LocalKey string                `json:"local_key"`
	Contract protocol.TaskContract `json:"contract"`
}

// SubmitTaskGraphOutput is submit_task_graph's output.
type SubmitTaskGraphOutput struct {
	Results []TaskGraphResultOutput `json:"results"`
}

func submitTaskGraphHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitTaskGraphInput) (*mcp.CallToolResult, SubmitTaskGraphOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitTaskGraphInput) (*mcp.CallToolResult, SubmitTaskGraphOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, SubmitTaskGraphOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		items := make([]tasks.GraphItem, len(in.Items))
		for i, it := range in.Items {
			dependsOn := make([]tasks.GraphDependency, len(it.DependsOn))
			for j, dep := range it.DependsOn {
				dependsOn[j] = tasks.GraphDependency{
					LocalKey: dep.LocalKey,
					Type:     protocol.TaskContractDependenciesElemType(dep.Type),
				}
			}

			var riskClassification protocol.TaskContractRiskClassification
			if it.RiskClassification != "" {
				riskClassification = protocol.TaskContractRiskClassification(it.RiskClassification)
			}

			items[i] = tasks.GraphItem{
				LocalKey:      it.LocalKey,
				RequirementID: it.RequirementId,
				Input: tasks.NewTaskContractInput{
					TaskID:                    it.TaskId,
					Repository:                it.Repository,
					Scope:                     it.Scope,
					ExpectedFilesOrComponents: it.ExpectedFilesOrComponents,
					AcceptanceCriteria:        it.AcceptanceCriteria,
					TestRequirements:          it.TestRequirements,
					RequiredEvidence:          it.RequiredEvidence,
					RiskClassification:        riskClassification,
					ApprovalRequired:          it.ApprovalRequired,
					DefinitionOfDone:          it.DefinitionOfDone,
					BeadsParent:               it.BeadsParent,
					BeadsType:                 it.BeadsType,
					BeadsLabels:               it.BeadsLabels,
				},
				DependsOn: dependsOn,
			}
		}

		results, err := tasks.GenerateGraph(ctx, a.Supervisor, a.Workspace.Root, store, items)
		if err != nil {
			return nil, SubmitTaskGraphOutput{}, fmt.Errorf("mcpserver: generate task graph: %w", err)
		}

		out := SubmitTaskGraphOutput{Results: make([]TaskGraphResultOutput, len(results))}
		for i, r := range results {
			out.Results[i] = TaskGraphResultOutput{LocalKey: r.LocalKey, Contract: r.Contract}
		}
		return nil, out, nil
	}
}
