package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/reconcile"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// IngestJiraRequirementInput is ingest_jira_requirement's input.
type IngestJiraRequirementInput struct {
	RunId        string `json:"run_id"`
	IssueIdOrKey string `json:"issue_id_or_key" jsonschema:"the Jira issue key or id to ingest as a requirement knowledge record, e.g. PAY-1842"`
	RequestedBy  string `json:"requested_by" jsonschema:"one of semar|gareng|petruk|bagong; who is requesting this operation"`
}

// IngestJiraRequirementOutput is ingest_jira_requirement's output.
type IngestJiraRequirementOutput struct {
	RequirementId string `json:"requirement_id" jsonschema:"pass this as requirement_id to build_task_context and submit_task_graph"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	Created       bool   `json:"created" jsonschema:"true if this created a new requirement record; false if it refreshed an existing one for the same issue"`
}

// ingestJiraRequirementHandler implements the missing half of the
// requirement pipeline: build_task_context and submit_task_graph both
// hard-require store.Get(requirement_id) to already succeed (§9.1/§10.1),
// but until this tool, nothing created that record - the requirement
// KnowledgeRecordType existed in the schema with zero writers anywhere in
// the codebase. This fetches the issue via the same adapter operation
// internal/reconcile later re-fetches for staleness checks, and persists a
// minimal requirement record (id, title, provenance) via store.Put; it
// does not decompose the issue into acceptance criteria/claims/constraints,
// since the schema has no body field on the base record for that (§7.1)
// and that decomposition is the calling role's job (§9), not this tool's.
func ingestJiraRequirementHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, IngestJiraRequirementInput) (*mcp.CallToolResult, IngestJiraRequirementOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in IngestJiraRequirementInput) (*mcp.CallToolResult, IngestJiraRequirementOutput, error) {
		gate, err := a.AdapterRegistry.Gate(ctx, "atlassian")
		if err != nil {
			return nil, IngestJiraRequirementOutput{}, fmt.Errorf("mcpserver: ingest_jira_requirement: %w", err)
		}
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, IngestJiraRequirementOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		out, err := ingestJiraRequirement(ctx, req, gate, store, a.Workspace.ID, in)
		return nil, out, err
	}
}

// ingestJiraRequirement is ingestJiraRequirementHandler's core logic, split
// out so it can be tested against a Gate built from a fake caller
// (mirroring internal/adapters/gate_test.go's pattern) instead of a real
// spawned adapter process, which would require live Jira credentials.
func ingestJiraRequirement(ctx context.Context, req *mcp.CallToolRequest, gate *adapters.Gate, store *knowledge.Store, workspaceID string, in IngestJiraRequirementInput) (IngestJiraRequirementOutput, error) {
	var out IngestJiraRequirementOutput
	requestedBy := protocol.ApprovalRecordRequestedBy(in.RequestedBy)

	// fields: ["*all"] and includeRaw: true mirror internal/reconcile's own
	// fetchOpForSource params exactly, so the content_hash computed below
	// from the same raw envelope is the correct baseline for that package's
	// later CheckSourceStale calls against this record.
	raw, err := invokeAdapterOperation(ctx, req, gate, in.RunId, "atlassian.getJiraIssue", map[string]any{
		"issueIdOrKey": in.IssueIdOrKey,
		"fields":       []string{"*all"},
		"includeRaw":   true,
	}, requestedBy)
	if err != nil {
		return out, fmt.Errorf("mcpserver: fetch jira issue %q: %w", in.IssueIdOrKey, err)
	}

	var result struct {
		Normalized struct {
			Source  protocol.KnowledgeRecordSource `json:"source"`
			Key     string                         `json:"key"`
			Summary string                         `json:"summary"`
			Status  string                         `json:"status"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return out, fmt.Errorf("mcpserver: decode jira issue %q: %w", in.IssueIdOrKey, err)
	}
	if result.Normalized.Key == "" {
		return out, fmt.Errorf("mcpserver: jira issue %q: adapter response had no normalized.key", in.IssueIdOrKey)
	}
	if result.Normalized.Summary == "" {
		return out, fmt.Errorf("mcpserver: jira issue %q has no summary; a requirement record requires a non-empty title", in.IssueIdOrKey)
	}

	hash := knowledge.ContentHash(reconcile.StableSourcePayload(raw))
	source := result.Normalized.Source
	source.ContentHash = &hash

	id := fmt.Sprintf("pkw:req/%s/%s", workspaceID, result.Normalized.Key)
	_, err = store.Get(id)
	created := errors.Is(err, knowledge.ErrNotFound)
	if err != nil && !created {
		return out, fmt.Errorf("mcpserver: check existing requirement %q: %w", id, err)
	}

	// Status is the raw Jira status name, verbatim, matching
	// check_jira_skippable's expectation that a Jira-sourced requirement's
	// Status field holds normalizeJiraIssue's fields.status.name - not a
	// generic "active" placeholder.
	rec := protocol.KnowledgeRecord{
		Id:     id,
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: result.Normalized.Status,
		Title:  result.Normalized.Summary,
		Source: source,
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodImported,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	}
	if err := store.Put(rec); err != nil {
		return out, fmt.Errorf("mcpserver: persist requirement %q: %w", id, err)
	}

	out.RequirementId = id
	out.Title = rec.Title
	out.Status = rec.Status
	out.Created = created
	return out, nil
}
