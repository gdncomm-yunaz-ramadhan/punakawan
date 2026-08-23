// tools_updatedelivery.go implements update_delivery, the one tool for
// editing everything about a delivery orchestration that describes it
// rather than drives it: its title and description, the plan it was
// built from, the session driving it, and which projects it involves.
// These are deliberately one tool rather than five near-identical ones -
// they share an orchestration id, an expected revision, and a returned
// view, and the only thing that would differ between separate tools is
// which single field they carry.
package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/plan"
)

// UpdateDeliveryInput is update_delivery's input. Every field beyond the
// orchestration id and expected revision is optional, and only the ones
// actually supplied change anything.
//
// The four text fields are pointers rather than plain strings so that
// "leave this alone" and "clear this" stay distinguishable: omitting
// title entirely keeps whatever title the run has, whereas passing it as
// an empty string removes it, and the run goes back to showing a label
// derived from its requirement references.
type UpdateDeliveryInput struct {
	OrchestrationId string `json:"orchestration_id"`
	// ExpectedRevision guards the whole call, not just its first change:
	// it is checked against the orchestration's current revision before
	// anything is written, so an edit composed against a view somebody
	// else has already moved past conflicts instead of landing silently.
	ExpectedRevision int `json:"expected_revision" jsonschema:"the orchestration's current revision from get_delivery, so an edit against an already-superseded view is never silently accepted"`

	Title        *string `json:"title,omitempty" jsonschema:"short human-readable summary of what this delivery delivers. Pass an empty string to remove it, after which the delivery again shows a label derived from its requirement references"`
	Description  *string `json:"description,omitempty" jsonschema:"longer prose about what this delivery is for and why it exists. Pass an empty string to remove it; nothing is derived in its place"`
	// PlanRecordId is deprecated: prefer PlanId+PlanRevision, which name
	// an exact internal/plan revision instead of a knowledge record from
	// the old plan-as-knowledge write path (§4.4).
	PlanRecordId *string `json:"plan_record_id,omitempty" jsonschema:"deprecated - id of the knowledge record holding this delivery's final plan, from the old submit_final_plan write path. Rejected if no such record exists. Pass an empty string to remove the reference"`
	PlanId       *string `json:"plan_id,omitempty" jsonschema:"id of the internal/plan lineage this delivery is built from. Must be supplied together with plan_revision. Rejected if no such plan exists. Pass an empty string to remove the reference"`
	PlanRevision *int    `json:"plan_revision,omitempty" jsonschema:"exact revision of plan_id this delivery is built from. Must be supplied together with plan_id. Rejected if that revision does not exist"`
	SessionId    *string `json:"session_id,omitempty" jsonschema:"id of the workflow run driving this delivery - the same id passed as run_id elsewhere. Pass an empty string to remove it"`

	AttachProjectIds []string `json:"attach_project_ids,omitempty" jsonschema:"ids of already-registered, active projects this delivery involves. Attaching one it already involves changes nothing"`
	DetachProjectIds []string `json:"detach_project_ids,omitempty" jsonschema:"ids of projects to stop listing on this delivery. Refused while that project still has lanes short of accepted or failed - lanes are never deleted or reassigned. Lanes that already finished keep their project, so the delivery still reports that work"`
}

func updateDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, UpdateDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in UpdateDeliveryInput) (*mcp.CallToolResult, DeliveryViewOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliveryViewOutput{}, err
		}

		details := delivery.OrchestrationDetails{
			Title: in.Title, Description: in.Description,
			PlanRecordID: in.PlanRecordId, PlanID: in.PlanId, PlanRevision: in.PlanRevision,
			SessionID: in.SessionId,
		}
		if len(in.AttachProjectIds) == 0 && len(in.DetachProjectIds) == 0 && !anyTextSupplied(details) {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: update_delivery: supply at least one field to change")
		}
		if err := validatePlanRecord(a, in.PlanRecordId); err != nil {
			return nil, DeliveryViewOutput{}, err
		}
		if err := validatePlan(ctx, a, in.PlanId, in.PlanRevision); err != nil {
			return nil, DeliveryViewOutput{}, err
		}

		// Each change is its own event, so each bumps the revision. The
		// caller's expected_revision therefore guards only the first
		// write; every later one uses the revision the previous write
		// just produced, which is exactly the sequence the caller would
		// have performed by hand had these been separate tools.
		revision := in.ExpectedRevision
		if anyTextSupplied(details) {
			orch, err := store.UpdateOrchestrationDetails(ctx, delivery.NewID(), in.OrchestrationId, revision, details)
			if err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: update delivery details: %w", err)
			}
			revision = orch.Revision
		}
		for _, projectID := range in.AttachProjectIds {
			orch, err := store.AttachProject(ctx, delivery.NewID(), in.OrchestrationId, revision, projectID)
			if err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: attach project %s: %w", projectID, err)
			}
			revision = orch.Revision
		}
		for _, projectID := range in.DetachProjectIds {
			orch, err := store.DetachProject(ctx, delivery.NewID(), in.OrchestrationId, revision, projectID)
			if err != nil {
				return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: detach project %s: %w", projectID, err)
			}
			revision = orch.Revision
		}

		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, DeliveryViewOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, DeliveryViewOutput{View: *view}, nil
	}
}

// anyTextSupplied reports whether the call asks to change any of the
// orchestration's descriptive fields at all (PlanRevision included,
// despite being an int - it is still one of update_delivery's
// change-or-leave-alone fields, just not free text).
func anyTextSupplied(details delivery.OrchestrationDetails) bool {
	return details.Title != nil || details.Description != nil ||
		details.PlanRecordID != nil || details.PlanID != nil || details.PlanRevision != nil ||
		details.SessionID != nil
}

// validatePlanRecord refuses a plan reference that names no knowledge
// record, so a delivery never advertises a plan a reader cannot open.
// The check lives here rather than in the delivery store because the
// knowledge store is a separate persistence lifecycle that the store has
// no handle on, whereas this layer already holds both. Clearing the
// reference is not a reference at all and is never checked.
func validatePlanRecord(a *app.App, planRecordID *string) error {
	if planRecordID == nil || *planRecordID == "" {
		return nil
	}
	store, err := a.OpenKnowledge()
	if err != nil {
		return fmt.Errorf("mcpserver: open knowledge store to verify plan_record_id: %w", err)
	}
	if _, err := store.Get(*planRecordID); err != nil {
		if errors.Is(err, knowledge.ErrNotFound) {
			return fmt.Errorf("mcpserver: update_delivery: plan_record_id %q names no knowledge record", *planRecordID)
		}
		return fmt.Errorf("mcpserver: verify plan_record_id %q: %w", *planRecordID, err)
	}
	return nil
}

// validatePlan refuses a plan_id/plan_revision pointing at a plan
// revision that does not exist, mirroring validatePlanRecord for the
// deprecated field. Unlike PlanRecordID, PlanId and PlanRevision may be
// edited independently across separate calls (e.g. bumping the revision
// after the underlying plan is revised, without re-stating the id), so
// this only validates as much of the pairing as this call actually
// supplied. Clearing (planID non-nil but empty) is never checked, same
// as validatePlanRecord.
func validatePlan(ctx context.Context, a *app.App, planID *string, planRevision *int) error {
	if planID == nil || *planID == "" {
		return nil
	}
	store, err := a.OpenPlan()
	if err != nil {
		return fmt.Errorf("mcpserver: open plan store to verify plan_id: %w", err)
	}
	if planRevision != nil {
		if _, err := store.GetRevision(ctx, *planID, *planRevision); err != nil {
			if errors.Is(err, plan.ErrNotFound) {
				return fmt.Errorf("mcpserver: update_delivery: plan_id %q has no revision %d", *planID, *planRevision)
			}
			return fmt.Errorf("mcpserver: verify plan_id %q revision %d: %w", *planID, *planRevision, err)
		}
		return nil
	}
	if _, err := store.Get(ctx, *planID); err != nil {
		if errors.Is(err, plan.ErrNotFound) {
			return fmt.Errorf("mcpserver: update_delivery: plan_id %q names no plan", *planID)
		}
		return fmt.Errorf("mcpserver: verify plan_id %q: %w", *planID, err)
	}
	return nil
}
