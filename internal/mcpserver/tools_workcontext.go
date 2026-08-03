package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/workcontext"
	"github.com/ygrip/punakawan/internal/workflow"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// PrepareWorkContextInput drives the single context-preparation entry point
// (agent-context plan §4.4/§5.2). Provide either an explicit workflow_id or a
// capability(+intent) selector to resolve a definition; omit both for an ad
// hoc run. A retrieval_query turns on scoped, lifecycle-filtered knowledge
// retrieval.
type PrepareWorkContextInput struct {
	RunId string `json:"run_id,omitempty" jsonschema:"resume/refresh this run's context; omit to create a new run"`

	WorkflowId string         `json:"workflow_id,omitempty" jsonschema:"explicit workflow definition id to run"`
	Capability string         `json:"capability,omitempty" jsonschema:"resolve a workflow implicitly by exact capability selector"`
	Intent     string         `json:"intent,omitempty" jsonschema:"intent paired with capability for selector resolution"`
	Inputs     map[string]any `json:"inputs,omitempty" jsonschema:"workflow input values; required inputs without a value or default are rejected"`
	Objective  string         `json:"objective,omitempty" jsonschema:"human-readable goal for this run"`

	RetrievalQuery        string   `json:"retrieval_query,omitempty" jsonschema:"scoped knowledge search query; omit to skip knowledge retrieval"`
	RequestedMetadataKeys []string `json:"requested_metadata_keys,omitempty"`
	KnowledgeTypes        []string `json:"knowledge_types,omitempty"`
	KnowledgeLimit        int      `json:"knowledge_limit,omitempty"`
	IncludeAssumed        bool     `json:"include_assumed,omitempty" jsonschema:"also include 'assumed' knowledge as accepted context"`

	Project    string `json:"project,omitempty"`
	Repository string `json:"repository,omitempty"`
	Module     string `json:"module,omitempty"`
	Path       string `json:"path,omitempty"`
}

type PrepareWorkContextMetadataItem struct {
	Key    string `json:"key"`
	Value  any    `json:"value,omitempty"`
	Reason string `json:"reason"`
}

type PrepareWorkContextKnowledgeItem struct {
	Id          string `json:"id"`
	Validity    string `json:"validity,omitempty"`
	Reason      string `json:"reason"`
	Summary     string `json:"summary,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

type PrepareWorkContextMissingItem struct {
	Kind string `json:"kind"`
	Key  string `json:"key,omitempty"`
}

// PrepareWorkContextOutput returns the run and its bounded, immutable context
// so the agent has required metadata and selected knowledge without any
// further discovery calls (plan §4.4 exit criterion).
type PrepareWorkContextOutput struct {
	RunId       string `json:"run_id"`
	State       string `json:"state"`
	Digest      string `json:"digest"`
	WorkflowId  string `json:"workflow_id,omitempty"`
	AdHoc       bool   `json:"ad_hoc"`
	NextAction  string `json:"next_action"`
	MetadataRev int    `json:"project_metadata_revision"`

	ResolvedInputs map[string]any                    `json:"resolved_inputs,omitempty"`
	Metadata       []PrepareWorkContextMetadataItem  `json:"metadata,omitempty"`
	Knowledge      []PrepareWorkContextKnowledgeItem `json:"knowledge,omitempty"`
	Caution        []PrepareWorkContextKnowledgeItem `json:"caution,omitempty"`
	Missing        []PrepareWorkContextMissingItem   `json:"missing,omitempty"`

	// Candidates is populated only when a selector matched more than one
	// workflow; the caller must disambiguate rather than the tool guessing.
	Candidates []string `json:"candidates,omitempty"`
}

func prepareWorkContextHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PrepareWorkContextInput) (*mcp.CallToolResult, PrepareWorkContextOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PrepareWorkContextInput) (*mcp.CallToolResult, PrepareWorkContextOutput, error) {
		root := a.Workspace.Root

		defs, err := loadWorkflowDefinitions(root)
		if err != nil {
			return nil, PrepareWorkContextOutput{}, fmt.Errorf("mcpserver: load workflow definitions: %w", err)
		}

		// Knowledge retrieval and recipe resolution only open the (expensive)
		// Dolt-backed store when a retrieval query is actually supplied.
		var searchFn workcontext.SearchFunc
		var recipes *recipe.Repository
		if in.RetrievalQuery != "" {
			store, err := a.OpenKnowledge()
			if err != nil {
				return nil, PrepareWorkContextOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
			}
			ix, err := a.OpenSearchIndex()
			if err != nil {
				return nil, PrepareWorkContextOutput{}, fmt.Errorf("mcpserver: open search index: %w", err)
			}
			searchFn = func(r search.Request) ([]search.Result, error) { return a.SearchKnowledge(store, ix, r) }
			recipes = &recipe.Repository{Store: store}
		}

		now := time.Now().UTC()
		res, err := workcontext.Prepare(workcontext.Request{
			WorkspaceRoot:         root,
			Definitions:           defs,
			WorkflowID:            in.WorkflowId,
			Capability:            in.Capability,
			Intent:                in.Intent,
			Inputs:                in.Inputs,
			RetrievalQuery:        in.RetrievalQuery,
			RequestedMetadataKeys: in.RequestedMetadataKeys,
			KnowledgeTypes:        in.KnowledgeTypes,
			KnowledgeLimit:        in.KnowledgeLimit,
			IncludeAssumed:        in.IncludeAssumed,
			Scope: search.Scope{
				Project:    in.Project,
				Repository: in.Repository,
				Module:     in.Module,
				Path:       in.Path,
			},
			Now: now,
		}, searchFn, recipes)
		if err != nil {
			// An ambiguous selector is a caller-resolvable condition, not a
			// server error: return the candidates so the agent picks one.
			if errors.Is(err, workflowdef.ErrAmbiguousSelector) {
				ids := make([]string, 0, len(res.Candidates))
				for _, c := range res.Candidates {
					ids = append(ids, c.ID)
				}
				return nil, PrepareWorkContextOutput{
					NextAction: "disambiguate: more than one workflow matched; re-call with an explicit workflow_id",
					Candidates: ids,
				}, nil
			}
			return nil, PrepareWorkContextOutput{}, err
		}

		run, err := createOrResumeRun(a, in, res, now)
		if err != nil {
			return nil, PrepareWorkContextOutput{}, err
		}

		return nil, buildPrepareOutput(run, res), nil
	}
}

// createOrResumeRun creates a fresh definition-aware/ad hoc run, or refreshes
// an existing run's context snapshot when run_id names one that already exists.
func createOrResumeRun(a *app.App, in PrepareWorkContextInput, res workcontext.Result, now time.Time) (protocol.WorkflowRun, error) {
	snapshot := res.Snapshot

	if in.RunId != "" {
		if existing, err := a.Workflow.Get(in.RunId); err == nil {
			existing.ContextSnapshot = &snapshot
			if len(res.ResolvedInputs) > 0 {
				existing.Inputs = protocol.WorkflowRunInputs(res.ResolvedInputs)
			}
			existing.UpdatedAt = now
			if err := a.Workflow.Append(existing); err != nil {
				return protocol.WorkflowRun{}, fmt.Errorf("mcpserver: refresh run context: %w", err)
			}
			return existing, nil
		}
	}

	runID := in.RunId
	if runID == "" {
		suffix := "adhoc"
		if res.Definition != nil {
			suffix = res.Definition.ID
		}
		runID = fmt.Sprintf("pkw:run/%s/%s-%d", a.Workspace.ID, suffix, now.UnixNano())
	}
	run := workflow.New(runID, a.Workspace.ID, protocol.WorkflowRunWorkflowNameImplementationOnly, now)
	if in.Objective != "" {
		obj := in.Objective
		run.Objective = &obj
	} else if res.Definition != nil {
		obj := res.Definition.Name
		run.Objective = &obj
	}

	var defRef *protocol.WorkflowRunDefinitionRef
	if res.Definition != nil {
		defRef = &protocol.WorkflowRunDefinitionRef{Id: res.Definition.ID, Revision: res.Definition.Revision, ContentHash: res.Definition.ContentHash()}
	}
	run, err := workflow.StampContext(run, defRef, res.ResolvedInputs, res.StepProgress, &snapshot, now)
	if err != nil {
		return protocol.WorkflowRun{}, fmt.Errorf("mcpserver: stamp run context: %w", err)
	}
	if err := a.Workflow.Append(run); err != nil {
		return protocol.WorkflowRun{}, fmt.Errorf("mcpserver: append run: %w", err)
	}
	return run, nil
}

func buildPrepareOutput(run protocol.WorkflowRun, res workcontext.Result) PrepareWorkContextOutput {
	out := PrepareWorkContextOutput{
		RunId:          run.Id,
		State:          string(run.State),
		Digest:         res.Digest,
		AdHoc:          res.Definition == nil,
		MetadataRev:    res.MetadataRevision,
		ResolvedInputs: res.ResolvedInputs,
	}
	if res.Definition != nil {
		out.WorkflowId = res.Definition.ID
	}
	for _, m := range res.Metadata {
		out.Metadata = append(out.Metadata, PrepareWorkContextMetadataItem{Key: m.Key, Value: m.Value, Reason: m.Reason})
	}
	for _, k := range res.Knowledge {
		out.Knowledge = append(out.Knowledge, PrepareWorkContextKnowledgeItem{Id: k.Id, Validity: k.Validity, Reason: k.Reason, Summary: k.Summary, ContentHash: k.ContentHash})
	}
	for _, k := range res.Caution {
		out.Caution = append(out.Caution, PrepareWorkContextKnowledgeItem{Id: k.Id, Validity: k.Validity, Reason: k.Reason, Summary: k.Summary, ContentHash: k.ContentHash})
	}
	for _, m := range res.Missing {
		out.Missing = append(out.Missing, PrepareWorkContextMissingItem{Kind: m.Kind, Key: m.Key})
	}
	switch {
	case len(res.Missing) > 0:
		out.NextAction = "resolve missing context (see missing[]), then re-call prepare_work_context"
	case out.AdHoc:
		out.NextAction = "no matching workflow; proceed with an ad hoc run and record the actual path"
	default:
		out.NextAction = "proceed with the workflow; pass this run_id to run-scoped calls"
	}
	return out
}

// loadWorkflowDefinitions lists the enabled+disabled definitions under a
// workspace root so workcontext can resolve one. A workspace with no
// definitions directory yields an empty list, not an error.
func loadWorkflowDefinitions(root string) ([]workflowdef.Definition, error) {
	store, err := workflowdef.Open(root)
	if err != nil {
		return nil, err
	}
	return store.List()
}

// GetKnowledgeRecordsInput batch-reads complete typed knowledge records by id
// (agent-context plan §5.2), so an agent that has ids from prepare_work_context
// can fetch full detail in one call rather than N searches.
type GetKnowledgeRecordsInput struct {
	Ids []string `json:"ids" jsonschema:"knowledge record ids to fetch"`

	// ProjectId is ADR-0020's hub project filter: which project's knowledge
	// store to read ids from. Always pass your own calling project's id
	// explicitly; omitting it also defaults to it, so cross-project access
	// only happens when a caller deliberately names a different project. Only
	// works when that project shares this one's hub.
	ProjectId string `json:"project_id,omitempty" jsonschema:"which project's knowledge store to read from (ADR-0020) - defaults to the calling project; name another project's id to deliberately read from it"`
}

// GetKnowledgeRecordsOutput returns the found records and the ids that did not
// resolve, so a partial batch is not an error.
type GetKnowledgeRecordsOutput struct {
	Records  []protocol.KnowledgeRecord `json:"records"`
	NotFound []string                   `json:"not_found,omitempty"`
}

func getKnowledgeRecordsHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetKnowledgeRecordsInput) (*mcp.CallToolResult, GetKnowledgeRecordsOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetKnowledgeRecordsInput) (*mcp.CallToolResult, GetKnowledgeRecordsOutput, error) {
		store, err := a.OpenKnowledge()
		if err != nil {
			return nil, GetKnowledgeRecordsOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}
		out := GetKnowledgeRecordsOutput{Records: []protocol.KnowledgeRecord{}}
		for _, id := range in.Ids {
			rec, err := store.GetInProject(in.ProjectId, id)
			if errors.Is(err, knowledge.ErrNotFound) {
				out.NotFound = append(out.NotFound, id)
				continue
			}
			if err != nil {
				return nil, GetKnowledgeRecordsOutput{}, fmt.Errorf("mcpserver: get knowledge record %q: %w", id, err)
			}
			out.Records = append(out.Records, rec)
		}
		return nil, out, nil
	}
}
