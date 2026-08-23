package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

// WorkflowJudgment marks a save as the calling agent's own judgment call
// (rather than a human/user dictating the definition directly). Setting it
// records a fingerprinted, deduplicated learning.Proposal alongside the save
// - the same audit trail propose_project_learning builds - except this one
// is recorded already-applied (Status: accepted, no review_id) instead of
// left pending for a panel accept, since the save already happened.
// Rationale is required: an agent-judgment save must state why, even though
// nothing blocks on a human reading it first.
type WorkflowJudgment struct {
	Rationale    string   `json:"rationale" jsonschema:"why this pattern is worth capturing as a reusable workflow definition; required"`
	EvidenceIds  []string `json:"evidence_ids,omitempty"`
	SourceRunIds []string `json:"source_run_ids,omitempty"`
}

// SaveWorkflowDefinitionInput carries a full workflow definition to persist.
// version defaults to workflowdef.SchemaVersion when omitted. revision must
// equal the current live definition's revision when updating an existing id
// (workflowdef.Store.Save's own optimistic-concurrency check); it is ignored
// for a brand-new id. Judgment is set only when the agent itself - not an
// explicit user instruction - is the one deciding this pattern is worth
// capturing.
type SaveWorkflowDefinitionInput struct {
	Definition workflowdef.Definition `json:"definition" jsonschema:"the full workflow definition to save: id/name/steps are required, plus whichever of selectors/required_metadata/allowed_capabilities/approval/output apply"`
	Judgment   *WorkflowJudgment      `json:"judgment,omitempty" jsonschema:"set this when the agent's own judgment - not a direct user instruction - is why this definition is being saved; records a fingerprinted learning proposal alongside the save"`
}

type SaveWorkflowDefinitionOutput struct {
	Id       string `json:"id"`
	Revision int    `json:"revision"`
	Action   string `json:"action"` // "created" | "updated"

	// ProposalId/SupportCount are set only when Judgment was supplied.
	// SupportCount > 1 means this same step pattern (by fingerprint) has now
	// been captured/reinforced by agent judgment more than once.
	ProposalId   string `json:"proposal_id,omitempty"`
	SupportCount int    `json:"support_count,omitempty"`
}

// saveWorkflowDefinitionHandler lets a caller author or revise a workflow
// definition directly - either because a user dictated the flow explicitly,
// or because the calling agent judged a pattern worth capturing - without
// routing through propose_project_learning's panel-review gate first.
// Validation and versioning are unchanged from every other definition write:
// workflowdef.Validate runs against the same capability set the panel API
// uses, and workflowdef.Store.Save produces an immutable, revertable
// revision exactly as SetEnabled/the panel editor already do.
func saveWorkflowDefinitionHandler(a *app.App, reg *toolIndex) func(context.Context, *mcp.CallToolRequest, SaveWorkflowDefinitionInput) (*mcp.CallToolResult, SaveWorkflowDefinitionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SaveWorkflowDefinitionInput) (*mcp.CallToolResult, SaveWorkflowDefinitionOutput, error) {
		if in.Judgment != nil && in.Judgment.Rationale == "" {
			return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow_definition: judgment.rationale is required when judgment is set")
		}

		def := in.Definition
		if def.Version == "" {
			def.Version = workflowdef.SchemaVersion
		}
		canonicalizeWorkflowCapabilities(&def)

		caps := workflowdef.NewCapabilitySet(reg.Names(), nil)
		if err := workflowdef.Validate(def, caps); err != nil {
			return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow_definition: %w", err)
		}

		store, err := workflowdef.Open(a.Workspace.Root)
		if err != nil {
			return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow_definition: open store: %w", err)
		}

		action := "updated"
		if _, err := store.Get(def.ID); err != nil {
			if !errors.Is(err, workflowdef.ErrNotFound) {
				return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow_definition: %w", err)
			}
			action = "created"
		}

		saved, err := store.Save(def)
		if err != nil {
			return nil, SaveWorkflowDefinitionOutput{}, fmt.Errorf("mcpserver: save_workflow_definition: %w", err)
		}

		out := SaveWorkflowDefinitionOutput{Id: saved.ID, Revision: saved.Revision, Action: action}
		if in.Judgment != nil {
			proposalID, supportCount, err := recordWorkflowJudgment(a, saved, *in.Judgment)
			if err != nil {
				return nil, SaveWorkflowDefinitionOutput{}, err
			}
			out.ProposalId = proposalID
			out.SupportCount = supportCount
		}
		return nil, out, nil
	}
}

// canonicalizeWorkflowCapabilities accepts the two Jira-create identifiers
// agents most often infer from the adapter operation name and stores the
// canonical native MCP capability. This keeps workflow definitions portable:
// create_jira_issue is always registered, while raw adapter operations depend
// on which adapters happened to load for the current project.
func canonicalizeWorkflowCapabilities(def *workflowdef.Definition) {
	canonical := func(name string) string {
		switch name {
		case "createJiraIssue", "atlassian.createJiraIssue":
			return "create_jira_issue"
		default:
			return name
		}
	}
	for i := range def.AllowedCapabilities {
		def.AllowedCapabilities[i] = canonical(def.AllowedCapabilities[i])
	}
	for i := range def.Steps {
		def.Steps[i].Capability = canonical(def.Steps[i].Capability)
	}
}

// recordWorkflowJudgment records (or reinforces) a learning.Proposal for a
// workflow definition saved from the agent's own judgment. It reuses
// propose_project_learning's fingerprint/dedup shape exactly, except the
// proposal is written already Status: accepted with no review_id - the save
// already happened, so there is nothing left for a panel review to accept.
func recordWorkflowJudgment(a *app.App, def workflowdef.Definition, judgment WorkflowJudgment) (proposalID string, supportCount int, err error) {
	graph := make([]string, 0, len(def.Steps))
	for _, s := range def.Steps {
		graph = append(graph, s.Capability+":"+s.Intent)
	}
	fp := learning.WorkflowFingerprint(a.Workspace.ID, graph)

	store, err := a.OpenLearning()
	if err != nil {
		return "", 0, fmt.Errorf("mcpserver: save_workflow_definition: open learning store: %w", err)
	}
	now := time.Now().UTC()

	existing, ok, err := findLearningProposalByFingerprint(store, fp)
	if err != nil {
		return "", 0, fmt.Errorf("mcpserver: save_workflow_definition: find learning proposal: %w", err)
	}
	if ok {
		existing.EvidenceIds = mergeUnique(existing.EvidenceIds, judgment.EvidenceIds)
		existing.SourceRunIds = mergeUnique(existing.SourceRunIds, judgment.SourceRunIds)
		existing.SupportCount++
		existing.UpdatedAt = now
		if err := store.Append(existing); err != nil {
			return "", 0, fmt.Errorf("mcpserver: save_workflow_definition: update learning proposal: %w", err)
		}
		return existing.Id, existing.SupportCount, nil
	}

	lp := learning.Proposal{
		Id:           randomLocalID("learn"),
		ArtifactType: learning.TypeWorkflow,
		TargetId:     def.ID,
		Fingerprint:  fp,
		Rationale:    judgment.Rationale,
		EvidenceIds:  judgment.EvidenceIds,
		SourceRunIds: judgment.SourceRunIds,
		SupportCount: 1,
		Status:       learning.StatusAccepted,
		CreatedBy:    "agent_judgment",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.Append(lp); err != nil {
		return "", 0, fmt.Errorf("mcpserver: save_workflow_definition: record learning proposal: %w", err)
	}
	return lp.Id, lp.SupportCount, nil
}

// findLearningProposalByFingerprint looks past learning.Store's own
// FindPendingByFingerprint (which only matches Status: pending): an
// agent-judgment save is recorded already-accepted, so a later save of the
// same pattern must still find and reinforce it rather than creating a
// duplicate.
func findLearningProposalByFingerprint(store *learning.Store, fp string) (learning.Proposal, bool, error) {
	all, err := store.List()
	if err != nil {
		return learning.Proposal{}, false, err
	}
	for _, p := range all {
		if p.Fingerprint == fp {
			return p, true, nil
		}
	}
	return learning.Proposal{}, false, nil
}
