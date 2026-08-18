// tools_startdelivery.go implements the five delivery-facade MCP tools:
// start_delivery, get_delivery, answer_delivery_question,
// approve_project_delivery, and cancel_delivery. Each one wraps the
// already-built, already-tested internal/delivery Store API
// (deliveryview.go's DeliveryView and StartDelivery, plus the
// manifest/orchestration/routing methods store.go, manifests.go, and
// parenttasks.go already expose) - none of the underlying persistence or
// validation logic is reimplemented here.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
)

// StartDeliveryInput is start_delivery's input: a batch of requirement
// references to bootstrap one new delivery orchestration from.
type StartDeliveryInput struct {
	References []string `json:"references" jsonschema:"one entry per requirement source - a Jira PROJECT-123 key, a GitHub owner/repo#123 reference, an absolute http(s) URL, or free text; a reference this call cannot confidently classify becomes a pending question (visible in the returned view's pending_questions) instead of erroring the whole call"`
	// IdempotencyKey is optional: repeating the same key on retry
	// resolves to the same orchestration instead of minting a second
	// one for the same request.
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// WorkflowDefinitionId is optional: when set, it must name an
	// existing, enabled workflow definition (rejected outright
	// otherwise), and the new orchestration's lane role-stage gate then
	// consults that definition's Roles map instead of always requiring
	// all four of semar/gareng/petruk/bagong.
	WorkflowDefinitionId string `json:"workflow_definition_id,omitempty"`
	// Projects is optional but is what turns a new orchestration into a
	// delivery that can actually run: without it the call only records
	// the requirements and the orchestration stays at pending with zero
	// lanes until register_project, create_parent_task, and create_lane
	// are each called separately.
	Projects []StartDeliveryProject `json:"projects,omitempty" jsonschema:"optional decomposition applied in the same call - pass it whenever you already know which repositories the work lands in. Omitting it leaves an inert shell: an orchestration with zero lanes that nothing can execute until register_project, create_parent_task, and create_lane are called by hand. Passing it registers each project and creates one parent task plus one lane per task, so the returned view already shows real lanes. A project or task that cannot be created is reported in decomposition[].skipped; the rest of the delivery still succeeds"`
}

// StartDeliveryProject is one repository the delivery lands in, plus the
// units of work to open there. Slug/RepositoryUrl/DefaultBranch mirror
// register_project's own input exactly.
type StartDeliveryProject struct {
	Slug          string              `json:"slug" jsonschema:"unique short identifier for this project; a slug already registered under a different id cannot be re-registered, and that project is reported as skipped"`
	RepositoryUrl string              `json:"repository_url"`
	DefaultBranch string              `json:"default_branch,omitempty"`
	Tasks         []StartDeliveryTask `json:"tasks,omitempty" jsonschema:"one entry per unit of work; each becomes a parent task and a lane in this project. A project with no tasks is registered but gets no lanes"`
}

// StartDeliveryTask is one unit of work: a parent task grouping some of
// the call's captured requirement sources, plus the lane that executes
// it.
type StartDeliveryTask struct {
	Title string `json:"title"`
	// References names which of the call's own references this task
	// covers, written exactly as they were passed in (a source id,
	// canonical key, or bare external id is accepted too). Omitting it
	// means the task covers every source this call captured.
	References []string `json:"references,omitempty" jsonschema:"subset of this call's references that this task covers, written the same way they were passed in; omit to cover every captured source. A task whose references match nothing captured is skipped, since a parent task must group at least one requirement source"`
}

// StartDeliveryProjectResult reports what one requested project actually
// produced. Skipped is empty when everything asked for was created, and
// otherwise says which project or task could not be and why - a failure
// on one project never rolls back or aborts the others.
type StartDeliveryProjectResult struct {
	Slug          string   `json:"slug"`
	ProjectId     string   `json:"project_id,omitempty"`
	ParentTaskIds []string `json:"parent_task_ids,omitempty"`
	LaneIds       []string `json:"lane_ids,omitempty"`
	Skipped       string   `json:"skipped,omitempty"`
}

// StartDeliveryOutput is start_delivery's output: the new
// orchestration's id plus its DeliveryView, so a caller sees what to do
// next (e.g. resolve a pending question, or create and route parent
// tasks) without a second round trip. When projects were supplied, View
// is rebuilt after the decomposition so it shows the lanes just created
// rather than the empty pre-decomposition snapshot.
type StartDeliveryOutput struct {
	OrchestrationId string                       `json:"orchestration_id"`
	View            delivery.DeliveryView        `json:"view"`
	Decomposition   []StartDeliveryProjectResult `json:"decomposition,omitempty"`
}

func startDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, StartDeliveryInput) (*mcp.CallToolResult, StartDeliveryOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in StartDeliveryInput) (*mcp.CallToolResult, StartDeliveryOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, StartDeliveryOutput{}, err
		}
		view, err := store.StartDeliveryWithDefinition(ctx, in.IdempotencyKey, in.References, in.WorkflowDefinitionId)
		if err != nil {
			return nil, StartDeliveryOutput{}, fmt.Errorf("mcpserver: start delivery: %w", err)
		}
		out := StartDeliveryOutput{OrchestrationId: view.Orchestration.Id, View: *view}
		if len(in.Projects) == 0 {
			return nil, out, nil
		}

		out.Decomposition = decomposeStartDelivery(ctx, store, view.Orchestration.Id, in.References, in.Projects)
		// The view built inside StartDeliveryWithDefinition predates every
		// project, task, and lane just created, so it is rebuilt here rather
		// than returned stale - the whole point of accepting projects is that
		// the caller sees the live delivery in this one response.
		rebuilt, err := store.BuildDeliveryView(ctx, view.Orchestration.Id)
		if err != nil {
			return nil, StartDeliveryOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		out.View = *rebuilt
		return nil, out, nil
	}
}

// decomposeStartDelivery registers each requested project and opens one
// parent task plus one lane per task under it, reporting per project what
// was created and what was not. It never returns an error: the
// orchestration and its captured requirements already exist by this
// point, so throwing away a delivery that is nine tenths correct because
// one project's repository url was wrong would be worse than saying so
// and continuing.
func decomposeStartDelivery(ctx context.Context, store *delivery.Store, orchestrationID string, references []string, projects []StartDeliveryProject) []StartDeliveryProjectResult {
	sourceIDsByRef, allSourceIDs, indexErr := startDeliverySourceIndex(ctx, store, orchestrationID, references)

	results := make([]StartDeliveryProjectResult, 0, len(projects))
	for _, p := range projects {
		res := StartDeliveryProjectResult{Slug: p.Slug}
		if indexErr != nil {
			res.Skipped = fmt.Sprintf("list requirement sources: %v", indexErr)
			results = append(results, res)
			continue
		}
		project, err := store.RegisterProject(ctx, delivery.NewID(), delivery.NewID(), p.Slug, p.RepositoryUrl, p.DefaultBranch)
		if err != nil {
			res.Skipped = fmt.Sprintf("register project: %v", err)
			results = append(results, res)
			continue
		}
		res.ProjectId = project.Id

		var skipped []string
		for _, task := range p.Tasks {
			sourceIDs, unmatched := startDeliveryTaskSourceIDs(task, sourceIDsByRef, allSourceIDs)
			if len(sourceIDs) == 0 {
				if len(unmatched) > 0 {
					skipped = append(skipped, fmt.Sprintf("task %q: none of its references matched a captured requirement source (%s)", task.Title, strings.Join(unmatched, ", ")))
				} else {
					skipped = append(skipped, fmt.Sprintf("task %q: this delivery captured no requirement sources to group", task.Title))
				}
				continue
			}
			parentTask, err := store.CreateParentTask(ctx, delivery.NewID(), delivery.NewID(), orchestrationID, task.Title, sourceIDs)
			if err != nil {
				skipped = append(skipped, fmt.Sprintf("task %q: create parent task: %v", task.Title, err))
				continue
			}
			res.ParentTaskIds = append(res.ParentTaskIds, parentTask.Id)
			lane, err := store.CreateLane(ctx, delivery.NewID(), delivery.NewID(), orchestrationID, project.Id, parentTask.Id)
			if err != nil {
				skipped = append(skipped, fmt.Sprintf("task %q: create lane: %v", task.Title, err))
				continue
			}
			res.LaneIds = append(res.LaneIds, lane.Id)
		}
		res.Skipped = strings.Join(skipped, "; ")
		results = append(results, res)
	}
	return results
}

// startDeliverySourceIndex maps the strings a caller can plausibly use to
// name a captured requirement source onto that source's id, which it has
// no way of knowing before this call returns. The call's own reference
// strings are resolved through the same ClassifyReference/CanonicalKey
// pair the capture itself used, so the mapping is the exact-identifier
// one, never a fuzzy text match; a source id, canonical key, or bare
// external id is accepted as well for a caller that already has one. The
// second return is every captured source id, used by a task that names no
// references at all.
func startDeliverySourceIndex(ctx context.Context, store *delivery.Store, orchestrationID string, references []string) (map[string]string, []string, error) {
	sources, err := store.ListRequirementSources(ctx, orchestrationID)
	if err != nil {
		return nil, nil, err
	}

	byCanonicalKey := make(map[string]string, len(sources))
	index := make(map[string]string, len(sources)*2)
	all := make([]string, 0, len(sources))
	for _, src := range sources {
		byCanonicalKey[src.CanonicalKey] = src.Id
		index[src.Id] = src.Id
		index[src.CanonicalKey] = src.Id
		if src.ExternalId != nil && *src.ExternalId != "" {
			index[*src.ExternalId] = src.Id
		}
		all = append(all, src.Id)
	}
	for _, ref := range references {
		in, ok := delivery.ClassifyReference(ref)
		if !ok {
			// An unclassifiable reference became a pending question rather
			// than a source, so there is nothing to point this string at.
			continue
		}
		key, err := delivery.CanonicalKey(in)
		if err != nil {
			continue
		}
		if id, ok := byCanonicalKey[key]; ok {
			index[ref] = id
		}
	}
	return index, all, nil
}

// startDeliveryTaskSourceIDs resolves one task's references to source
// ids, also returning the references that matched nothing so the caller
// is told which string was wrong instead of just that something was.
func startDeliveryTaskSourceIDs(task StartDeliveryTask, index map[string]string, all []string) (sourceIDs []string, unmatched []string) {
	if len(task.References) == 0 {
		return all, nil
	}
	seen := make(map[string]bool, len(task.References))
	for _, ref := range task.References {
		id, ok := index[ref]
		if !ok {
			unmatched = append(unmatched, ref)
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		sourceIDs = append(sourceIDs, id)
	}
	return sourceIDs, unmatched
}

// GetDeliveryInput is get_delivery's input.
type GetDeliveryInput struct {
	OrchestrationId string `json:"orchestration_id"`
	// SinceSeq is optional: pass a prior response's view.latest_seq to
	// learn which lanes became runnable since that point, reported back
	// as view.newly_runnable_lane_ids. Omitted (or 0) reports every lane
	// currently runnable, since there is then no prior checkpoint to
	// diff against.
	SinceSeq int `json:"since_seq,omitempty"`
}

// DeliveryViewOutput wraps one DeliveryView, used by every tool in this
// file that returns the orchestration's refreshed state after a call.
type DeliveryViewOutput struct {
	View delivery.DeliveryView `json:"view"`
}

func getDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliveryViewOutput{}, err
		}
		view, err := store.BuildDeliveryViewSince(ctx, in.OrchestrationId, in.SinceSeq)
		if err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, DeliveryViewOutput{View: *view}, nil
	}
}

// AnswerDeliveryQuestionInput is answer_delivery_question's input. It
// supports two distinct cases, chosen by which fields are set:
//
//   - Resolved-requirement case: the caller now has real content for a
//     pending reference (one the initial classification could not
//     place). Set provider (and whichever of external_id/url/title/
//     summary that provider needs) plus expected_revision; this
//     captures it as a requirement source and clears it from the
//     orchestration's pending questions via ResolveInput.
//   - Ambiguous-routing case: the "question" was actually about which
//     project an already-created parent task belongs to. Set
//     parent_task_id and project_id; this calls RouteParentTask instead.
//     expected_revision is not used for this case - RouteParentTask has
//     no revision parameter.
type AnswerDeliveryQuestionInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	Reference        string `json:"reference" jsonschema:"the pending question's reference, from get_delivery's pending_questions; required for the resolved-requirement case, ignored for the routing case"`
	ExpectedRevision int    `json:"expected_revision,omitempty" jsonschema:"resolved-requirement case only: the orchestration's current revision from get_delivery, so answering an already-superseded question is never silently accepted"`

	Provider   string `json:"provider,omitempty" jsonschema:"resolved-requirement case: jira | confluence | github | url | freetext"`
	ExternalId string `json:"external_id,omitempty" jsonschema:"resolved-requirement case: issue key, page id, or owner/repo#number; not used for url/freetext"`
	Url        string `json:"url,omitempty" jsonschema:"resolved-requirement case: canonical source url"`
	Title      string `json:"title,omitempty" jsonschema:"resolved-requirement case: human-readable title"`
	Summary    string `json:"summary,omitempty" jsonschema:"resolved-requirement case: short summary; freetext requires title or summary"`

	ParentTaskId string `json:"parent_task_id,omitempty" jsonschema:"ambiguous-routing case: the parent task this question was actually about"`
	ProjectId    string `json:"project_id,omitempty" jsonschema:"ambiguous-routing case: the project to route parent_task_id to"`
}

func answerDeliveryQuestionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AnswerDeliveryQuestionInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in AnswerDeliveryQuestionInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliveryViewOutput{}, err
		}

		switch {
		case in.ParentTaskId != "" && in.ProjectId != "":
			if _, err := store.RouteParentTask(ctx, delivery.NewID(), in.OrchestrationId, in.ParentTaskId, in.ProjectId); err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: route parent task: %w", err)
			}
		case in.Provider != "":
			src := delivery.SourceInput{
				Provider: in.Provider, ExternalID: in.ExternalId, URL: in.Url,
				Title: in.Title, Summary: in.Summary,
			}
			if _, err := store.CaptureRequirement(ctx, delivery.NewID(), in.OrchestrationId, src); err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: capture requirement: %w", err)
			}
			if _, err := store.ResolveInput(ctx, delivery.NewID(), in.OrchestrationId, in.ExpectedRevision, in.Reference); err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: resolve input: %w", err)
			}
		default:
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: answer_delivery_question requires either provider (resolved-requirement case) or both parent_task_id and project_id (routing case)")
		}

		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, DeliveryViewOutput{View: *view}, nil
	}
}

// ApproveProjectDeliveryInput is approve_project_delivery's input.
// Setting reject decides the manifest rejected instead of approved,
// rather than adding a seventh dedicated tool for that one-bit
// difference.
type ApproveProjectDeliveryInput struct {
	OrchestrationId string `json:"orchestration_id"`
	ManifestId      string `json:"manifest_id"`
	ApprovedBy      string `json:"approved_by" jsonschema:"identifies the human deciding this manifest; never one of the agent role names (semar/gareng/petruk/bagong) - Store rejects self-approval"`
	Reject          bool   `json:"reject,omitempty" jsonschema:"set true to reject the manifest instead of approving it"`
}

func approveProjectDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ApproveProjectDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ApproveProjectDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliveryViewOutput{}, err
		}

		if in.Reject {
			if _, err := store.RejectManifest(ctx, delivery.NewID(), in.OrchestrationId, in.ManifestId, in.ApprovedBy); err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: reject manifest: %w", err)
			}
		} else {
			if _, err := store.ApproveManifest(ctx, delivery.NewID(), in.OrchestrationId, in.ManifestId, in.ApprovedBy); err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: approve manifest: %w", err)
			}
		}

		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, DeliveryViewOutput{View: *view}, nil
	}
}

// CancelDeliveryInput is cancel_delivery's input. reason is accepted
// for a caller's own audit trail but is not persisted anywhere:
// CancelOrchestration's signature has no reason parameter to store it
// against, and inventing a side channel for it is out of scope here.
type CancelDeliveryInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	ExpectedRevision int    `json:"expected_revision" jsonschema:"the orchestration's current revision from get_delivery, so cancelling an already-superseded view is never silently accepted"`
	Reason           string `json:"reason,omitempty" jsonschema:"informational only - not persisted, since CancelOrchestration has no field to store it in"`
}

func cancelDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CancelDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CancelDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliveryViewOutput{}, err
		}
		if _, err := store.CancelOrchestration(ctx, delivery.NewID(), in.OrchestrationId, in.ExpectedRevision); err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: cancel orchestration: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, DeliveryViewOutput{View: *view}, nil
	}
}
