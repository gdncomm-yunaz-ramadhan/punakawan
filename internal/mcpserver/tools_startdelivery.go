// tools_startdelivery.go implements the delivery-facade MCP tools:
// start_delivery, get_delivery, answer_delivery_question,
// cancel_delivery, and complete_delivery.
//
// start_delivery has exactly one path. It translates its wire shape into
// a deliveryservice.StartRequest and hands the whole thing to
// Service.StartOrResolve, which resolves the delivery's lifetime and
// reconciles its projects, plans, tasks, and lanes idempotently. Nothing
// here decides what to create; the two jobs this file does own are
// expanding a project's task list into one draft per unit of work, and
// filling in a session the caller did not fully describe - without which
// the delivery records no usage at all.
package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryservice"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// StartDeliverySource names the exact provider-neutral source
// start_delivery resolves identity against: SourceOrchestration and its
// lifetime reuse/continuation rules live in internal/deliveryservice.
type StartDeliverySource struct {
	Kind   string `json:"kind" jsonschema:"jira | adhoc"`
	Tenant string `json:"tenant,omitempty" jsonschema:"required when kind is jira: a stable identity for the connected Jira adapter instance"`
	Key    string `json:"key,omitempty" jsonschema:"required when kind is jira: the exact issue key, e.g. ABC-123"`
}

// StartDeliveryInput is start_delivery's input: one delivery source, the
// repositories the work lands in, and the session it is worked under.
type StartDeliveryInput struct {
	// Source is the delivery's identity. It is required: a delivery with
	// no source has nothing to reuse a lifetime by and nothing to hang a
	// requirement on.
	Source *StartDeliverySource `json:"source,omitempty" jsonschema:"provider-neutral delivery identity - jira reuses its non-cancelled lifetime by tenant+key and starts the next execution ordinal after completion; adhoc always starts a new lifetime"`
	// Title is optional: the label humans and agents see in place of the
	// orchestration's opaque id. Omitted, one is derived from the source.
	Title string `json:"title,omitempty" jsonschema:"short human-readable summary of what this delivery delivers, written for whoever reads it later instead of the orchestration's opaque id - e.g. \"migrate checkout to the new payments API\". Omitting it derives one from the source key, so supply it whenever the key alone would not say what the work is"`
	// Description is optional: prose about what the delivery is for.
	// Omitted, the orchestration simply carries none - nothing invents
	// prose the way a missing title is derived.
	Description string `json:"description,omitempty" jsonschema:"longer prose about what this delivery is for and why it exists, for whoever reads the run later. Omitting it leaves the delivery with no description at all; unlike title, nothing is derived in its place"`
	// PlanID and PlanRevision name the already-saved cross-project plan
	// for the delivery. Project-specific detailed plans belong on the
	// matching project entries below.
	PlanID       string `json:"plan_id,omitempty" jsonschema:"id of a plan already saved with plan_save; pass plan_revision with it"`
	PlanRevision int    `json:"plan_revision,omitempty"`
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
	// Projects is what turns a new orchestration into a delivery that can
	// actually run: without it the call records the source and the
	// orchestration stays at pending with zero lanes.
	Projects []StartDeliveryProject `json:"projects,omitempty" jsonschema:"the repositories this delivery lands in and the units of work to open in each. Each project is registered and attached, and each task becomes one parent task plus one lane, so the returned view already shows real lanes. Anything that could not be created is named in reconciliation.skipped and the rest of the delivery still succeeds"`
	// Session is the durable agent session opened alongside the delivery.
	Session *StartDeliverySessionStart `json:"session,omitempty" jsonschema:"the working session this delivery is executed under; omit it and one is opened anyway, named after the connected client and rooted at the server's working directory"`
}

// StartDeliverySessionStart opens the durable agent session the delivery
// is worked under. It is what makes usage tracking possible at all: a
// turn's tokens, cost, and tool calls attach to a session, and the
// lifecycle hooks that report them find it through the marker file
// writeSessionMarker drops under WorktreePath.
type StartDeliverySessionStart struct {
	Participant       string `json:"participant,omitempty" jsonschema:"who is doing the work - a role id (semar | gareng | petruk | bagong) or any label; omitted, the connected client's own name is used"`
	WorktreePath      string `json:"worktree_path,omitempty" jsonschema:"absolute path of the checkout this delivery is worked in; a session marker is written there so lifecycle hooks can attribute token and cost usage back to this delivery. Omitted, the server's own working directory is used"`
	Provider          string `json:"provider,omitempty" jsonschema:"the coding agent's provider, when known"`
	ResumedFromID     string `json:"resumed_from_id,omitempty" jsonschema:"the prior session this one continues, when resuming"`
	ExternalSessionID string `json:"external_session_id,omitempty" jsonschema:"the client's own session or thread id, so a later lifecycle hook for the same session resumes under the client-native identity rather than a punakawan-minted one"`
}

// StartDeliveryProject is one repository the delivery lands in, plus the
// units of work to open there.
type StartDeliveryProject struct {
	Slug          string `json:"slug" jsonschema:"unique short identifier for this project; registering the same slug again updates it rather than failing"`
	RepositoryUrl string `json:"repository_url"`
	DefaultBranch string `json:"default_branch,omitempty"`
	// PlanID and PlanRevision name this project's already-saved detailed
	// plan. They must be supplied together, and the plan is linked to the
	// delivery after this project has been registered.
	PlanID       string              `json:"plan_id,omitempty" jsonschema:"id of a plan already saved with plan_save for this project; pass plan_revision with it"`
	PlanRevision int                 `json:"plan_revision,omitempty"`
	Tasks        []StartDeliveryTask `json:"tasks,omitempty" jsonschema:"one entry per unit of work; each becomes a parent task and a lane in this project. A project with no tasks is registered and attached but gets no lanes"`
}

// StartDeliveryTask is one unit of work: a parent task grouping the
// requirement source it covers, plus the lane that executes it.
type StartDeliveryTask struct {
	Title string `json:"title"`
	// References names the requirement source this task covers. Only the
	// first entry is used - a parent task groups one source - and omitting
	// it means the task covers the delivery's own source key, which is the
	// ordinary case for a single-issue delivery.
	References []string `json:"references,omitempty" jsonschema:"the requirement source this task covers, normally a Jira issue key such as a subtask of the delivery's parent; omit to cover the delivery's own source key. A key matching nothing captured leaves the task without a lane and is named in reconciliation.skipped"`
}

// StartDeliveryRequirementSource is one captured requirement source,
// reported so a caller can pass its id straight to map_delivery_work_item
// without a second round trip - get_delivery does not expose source ids.
type StartDeliveryRequirementSource struct {
	Id           string `json:"id"`
	Provider     string `json:"provider"`
	ExternalId   string `json:"external_id,omitempty"`
	CanonicalKey string `json:"canonical_key"`
}

// StartDeliveryResultDelivery is the compact delivery identity
// start_delivery reports.
type StartDeliveryResultDelivery struct {
	ID                 string `json:"id"`
	ProjectionRevision int    `json:"projection_revision"`
}

// StartDeliveryResultSession is the compact session identity
// start_delivery reports for the session it opened.
type StartDeliveryResultSession struct {
	ID string `json:"id"`
}

// StartDeliveryOutput is start_delivery's output: the delivery's identity
// plus its rebuilt DeliveryView, so a caller sees the lanes this same call
// created rather than the empty pre-reconciliation snapshot.
type StartDeliveryOutput struct {
	Status     string                       `json:"status,omitempty"`
	NeedsInput *protocol.NeedUserInput      `json:"needs_input,omitempty"`
	Delivery   *StartDeliveryResultDelivery `json:"delivery,omitempty"`
	Session    *StartDeliveryResultSession  `json:"session,omitempty"`
	// Reconciliation says what this call created and, in its skipped
	// list, what it declined to create and why.
	Reconciliation *deliveryservice.ReconcileReport `json:"reconciliation,omitempty"`

	OrchestrationId string `json:"orchestration_id,omitempty"`
	// ExecutionId is the delivery execution map_delivery_work_item binds
	// against. It is a different id than OrchestrationId, and there is no
	// other call that hands it to a caller who has just started work.
	ExecutionId string `json:"execution_id,omitempty"`
	// Title sits beside the id so a caller quoting this response back to a
	// human has something to quote other than 26 opaque characters.
	Title              string                           `json:"title,omitempty"`
	RequirementSources []StartDeliveryRequirementSource `json:"requirement_sources,omitempty"`
	View               *delivery.DeliveryView           `json:"view,omitempty"`
}

func startDeliveryHandler(a *app.App, agentReg agent.AgentRegistry) func(context.Context, *mcp.CallToolRequest, StartDeliveryInput) (*mcp.CallToolResult, StartDeliveryOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in StartDeliveryInput) (*mcp.CallToolResult, StartDeliveryOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, StartDeliveryOutput{}, err
		}

		plans, err := a.OpenPlan()
		if err != nil {
			return nil, StartDeliveryOutput{}, err
		}
		ts, err := OpenTelemetryStore(ctx, a)
		if err != nil {
			return nil, StartDeliveryOutput{}, err
		}
		svc := deliveryservice.New(store, plans, deliveryservice.WithTelemetryStore(ts), deliveryservice.WithAgentRegistry(agentReg))

		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		start := deliveryservice.StartRequest{
			IdempotencyKey:       key,
			Title:                in.Title,
			Description:          in.Description,
			WorkflowDefinitionID: in.WorkflowDefinitionId,
			PlanID:               in.PlanID,
			PlanRevision:         in.PlanRevision,
			Projects:             startDeliveryProjectDrafts(in),
			Session:              startDeliverySession(req, in.Session, a.Workspace.Root),
		}
		if in.Source != nil {
			start.Source = &deliveryservice.SourceIdentity{
				Kind:   deliveryservice.SourceKind(strings.TrimSpace(in.Source.Kind)),
				Tenant: in.Source.Tenant,
				Key:    in.Source.Key,
			}
		}

		result, needsInput, err := svc.StartOrResolve(ctx, start)
		if err != nil {
			return nil, StartDeliveryOutput{}, fmt.Errorf("mcpserver: start_delivery: %w", err)
		}
		if needsInput != nil {
			return nil, StartDeliveryOutput{Status: "needs_input", NeedsInput: needsInput}, nil
		}

		orchestrationID := result.Execution.OrchestrationID
		revision, err := store.GetProjectionRevision(ctx, orchestrationID)
		if err != nil {
			return nil, StartDeliveryOutput{}, fmt.Errorf("mcpserver: read projection revision: %w", err)
		}
		// The view is built after reconciliation, never before it: the
		// whole point of accepting projects on this call is that the
		// caller sees the live delivery in this one response.
		view, err := store.BuildDeliveryView(ctx, orchestrationID)
		if err != nil {
			return nil, StartDeliveryOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		sources, err := store.ListRequirementSources(ctx, orchestrationID)
		if err != nil {
			return nil, StartDeliveryOutput{}, fmt.Errorf("mcpserver: list requirement sources: %w", err)
		}

		out := StartDeliveryOutput{
			Status:             "started",
			Delivery:           &StartDeliveryResultDelivery{ID: orchestrationID, ProjectionRevision: revision},
			Reconciliation:     &result.Reconciliation,
			OrchestrationId:    orchestrationID,
			ExecutionId:        result.Execution.ID,
			Title:              view.Title,
			RequirementSources: startDeliveryRequirementSources(sources),
			View:               view,
		}
		if result.Session != nil {
			out.Session = &StartDeliveryResultSession{ID: result.Session.ID}
			// The marker is how a lifecycle hook, which has no MCP session
			// of its own, finds the delivery that owns the checkout it is
			// running in. Without it the session exists but every token,
			// cost, and tool-call total reported later has nothing to
			// attach to.
			writeSessionMarker(result.Session)
		}
		return nil, out, nil
	}
}

// startDeliveryProjectDrafts expands one project-with-many-tasks request
// into the one-task-per-draft shape deliveryservice reconciles. A task
// that names no reference covers the delivery's own source key, which is
// the ordinary case for a single-issue Jira delivery and the difference
// between getting a lane and getting nothing.
func startDeliveryProjectDrafts(in StartDeliveryInput) []deliveryservice.ProjectDraft {
	defaultKey := ""
	if in.Source != nil {
		defaultKey = strings.TrimSpace(in.Source.Key)
	}
	drafts := make([]deliveryservice.ProjectDraft, 0, len(in.Projects))
	for _, p := range in.Projects {
		base := deliveryservice.ProjectDraft{
			Slug:          strings.TrimSpace(p.Slug),
			RepositoryURL: strings.TrimSpace(p.RepositoryUrl),
			DefaultBranch: strings.TrimSpace(p.DefaultBranch),
			PlanID:        strings.TrimSpace(p.PlanID),
			PlanRevision:  p.PlanRevision,
		}
		if len(p.Tasks) == 0 {
			drafts = append(drafts, base)
			continue
		}
		for _, task := range p.Tasks {
			draft := base
			draft.Title = strings.TrimSpace(task.Title)
			draft.TaskKey = defaultKey
			for _, ref := range task.References {
				if trimmed := strings.TrimSpace(ref); trimmed != "" {
					draft.TaskKey = trimmed
					break
				}
			}
			drafts = append(drafts, draft)
		}
	}
	return drafts
}

// startDeliverySession fills in the session a caller did not fully
// describe. Both defaults matter: a session with no participant is never
// opened at all, and a session with no worktree path leaves no marker for
// the lifecycle hooks to find, either of which silently costs the delivery
// its entire usage and cost record.
func startDeliverySession(req *mcp.CallToolRequest, in *StartDeliverySessionStart, workspaceRoot string) deliveryservice.SessionStart {
	out := deliveryservice.SessionStart{}
	if in != nil {
		out = deliveryservice.SessionStart{
			Participant:       strings.TrimSpace(in.Participant),
			ResumedFromID:     strings.TrimSpace(in.ResumedFromID),
			WorktreePath:      strings.TrimSpace(in.WorktreePath),
			Provider:          strings.TrimSpace(in.Provider),
			ExternalSessionID: strings.TrimSpace(in.ExternalSessionID),
		}
	}
	if out.Participant == "" {
		out.Participant = mcpClientName(req)
	}
	if out.WorktreePath == "" {
		out.WorktreePath = workspaceRoot
	}
	return out
}

// mcpClientName is the connected client's own declared name, used as the
// session participant when the caller names none.
func mcpClientName(req *mcp.CallToolRequest) string {
	if req == nil || req.Session == nil {
		return "connected-agent"
	}
	params := req.Session.InitializeParams()
	if params == nil || params.ClientInfo == nil {
		return "connected-agent"
	}
	if name := strings.TrimSpace(params.ClientInfo.Name); name != "" {
		return name
	}
	return "connected-agent"
}

func startDeliveryRequirementSources(sources []*protocol.RequirementSource) []StartDeliveryRequirementSource {
	out := make([]StartDeliveryRequirementSource, 0, len(sources))
	for _, src := range sources {
		if src == nil {
			continue
		}
		entry := StartDeliveryRequirementSource{Id: src.Id, Provider: string(src.Provider), CanonicalKey: src.CanonicalKey}
		if src.ExternalId != nil {
			entry.ExternalId = *src.ExternalId
		}
		out = append(out, entry)
	}
	return out
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
		store, err := OpenDeliveryStore(ctx, a)
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
		store, err := OpenDeliveryStore(ctx, a)
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
		store, err := OpenDeliveryStore(ctx, a)
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

// CompleteDeliveryInput is complete_delivery's input.
type CompleteDeliveryInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	ExpectedRevision int    `json:"expected_revision" jsonschema:"the orchestration's current revision from get_delivery, so completing an already-superseded view is never silently accepted"`
}

func completeDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CompleteDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CompleteDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliveryViewOutput{}, err
		}
		if _, err := store.CompleteOrchestration(ctx, delivery.NewID(), in.OrchestrationId, in.ExpectedRevision); err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: complete orchestration: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, DeliveryViewOutput{View: *view}, nil
	}
}
