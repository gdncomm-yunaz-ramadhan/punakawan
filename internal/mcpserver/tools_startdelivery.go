// tools_startdelivery.go implements the six delivery-facade MCP tools
// (punokawan-14yn.10 phase 1): start_delivery, get_delivery,
// resume_delivery, answer_delivery_question, approve_project_delivery,
// and cancel_delivery. Each one wraps the already-built, already-tested
// internal/delivery Store API (deliveryview.go's DeliveryView and
// StartDelivery, plus the manifest/orchestration/routing methods
// store.go, manifests.go, and parenttasks.go already expose) - none of
// the underlying persistence or validation logic is reimplemented here.
package mcpserver

import (
	"context"
	"fmt"

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
}

// StartDeliveryOutput is start_delivery's output: the new
// orchestration's id plus its DeliveryView, so a caller sees what to do
// next (e.g. resolve a pending question, or create and route parent
// tasks) without a second round trip.
type StartDeliveryOutput struct {
	OrchestrationId string                `json:"orchestration_id"`
	View            delivery.DeliveryView `json:"view"`
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
		return nil, StartDeliveryOutput{OrchestrationId: view.Orchestration.Id, View: *view}, nil
	}
}

// GetDeliveryInput is get_delivery's (and resume_delivery's) input.
type GetDeliveryInput struct {
	OrchestrationId string `json:"orchestration_id"`
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
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, DeliveryViewOutput{View: *view}, nil
	}
}

// resumeDeliveryHandler is get_delivery's handler under a second name.
// This domain is event-sourced: current state is always derived fresh
// by replaying the event log, so there is no separate session or
// in-memory progress to "resume" - checking current status via
// BuildDeliveryView already tells a reconnecting caller everything a
// dedicated resume mechanism could, so resume_delivery does not
// implement one.
func resumeDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
	return getDeliveryHandler(a)
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
