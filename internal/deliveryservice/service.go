package deliveryservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Service is the one composition entry point every delivery-starting
// surface (MCP, workflow invocation, daemon) routes through: it resolves
// identity before any later reconciliation, session, or telemetry step.
type Service struct {
	deliveries *delivery.Store
	plans      *plan.Store
}

// New wires a Service over the already-open delivery and plan stores.
func New(deliveries *delivery.Store, plans *plan.Store) *Service {
	return &Service{deliveries: deliveries, plans: plans}
}

// StartOrResolve resolves req.Source to one delivery lifetime and
// execution, validating every required piece of context before writing
// anything. It returns a structured NeedUserInput - never a partially
// written delivery - when required context is missing or contradictory.
func (s *Service) StartOrResolve(ctx context.Context, req StartRequest) (StartResult, *protocol.NeedUserInput, error) {
	if req.Source == nil {
		return StartResult{}, &protocol.NeedUserInput{
			Kind:          protocol.NeedUserInputKindMissingContext,
			Question:      "start_delivery requires a source (jira or adhoc).",
			MissingFields: []string{"source"},
		}, nil
	}
	normalized, needsInput, err := NormalizeSource(*req.Source)
	if err != nil {
		return StartResult{}, nil, err
	}
	if needsInput != nil {
		return StartResult{}, needsInput, nil
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		return StartResult{}, nil, fmt.Errorf("deliveryservice: idempotency_key is required")
	}

	resolved, err := s.deliveries.StartOrResolveExecution(ctx, req.IdempotencyKey, normalized.toDeliverySource(), delivery.OrchestrationOptions{
		Title:                strings.TrimSpace(req.Title),
		Description:          strings.TrimSpace(req.Description),
		WorkflowDefinitionID: req.WorkflowDefinitionID,
	})
	if err != nil {
		return StartResult{}, nil, err
	}
	result := StartResult{
		Lifetime:         *resolved.Lifetime,
		Execution:        *resolved.Execution,
		CreatedLifetime:  resolved.CreatedLifetime,
		CreatedExecution: resolved.CreatedExecution,
		Reconciliation:   ReconcileReport{},
	}
	if strings.TrimSpace(req.Session.Participant) != "" {
		session, err := s.deliveries.StartSession(ctx, req.IdempotencyKey+":session", resolved.Execution.ID, "", req.Session.Participant, req.Session.ResumedFromID, req.Session.WorktreePath, req.Session.Provider)
		if err != nil {
			return StartResult{}, nil, err
		}
		result.Session = session
	}
	return result, nil, nil
}

// Complete atomically completes orchestrationID's execution and returns
// its refreshed view.
//
// Deviation from the plan text: the plan's signature names a return type
// delivery.DeliveryDetail, which does not exist yet - the improvement
// plan's "one compact list, one exact detail" panel projection is a later
// task's package (internal/deliveryprojection, per the plan's own file
// structure table). This returns the real, already-existing
// *delivery.DeliveryView instead of fabricating that later type early.
func (s *Service) Complete(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int) (*delivery.DeliveryView, error) {
	if _, err := s.deliveries.CompleteOrchestration(ctx, idempotencyKey, orchestrationID, expectedRevision); err != nil {
		return nil, err
	}
	return s.deliveries.BuildDeliveryView(ctx, orchestrationID)
}

// Cancel cancels orchestrationID and returns its refreshed view. See
// Complete's doc comment for the same delivery.DeliveryView deviation.
func (s *Service) Cancel(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int) (*delivery.DeliveryView, error) {
	if _, err := s.deliveries.CancelOrchestration(ctx, idempotencyKey, orchestrationID, expectedRevision); err != nil {
		return nil, err
	}
	return s.deliveries.BuildDeliveryView(ctx, orchestrationID)
}
