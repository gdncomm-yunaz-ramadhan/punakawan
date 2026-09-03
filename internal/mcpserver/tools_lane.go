package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// CompleteDeliveryLaneInput closes one lane's work. It deliberately does
// not expose leases: acquiring one, heartbeating it, and handing back a
// token is orchestrator bookkeeping, not something an agent that has just
// finished a piece of work should have to narrate.
type CompleteDeliveryLaneInput struct {
	OrchestrationID  string `json:"orchestration_id"`
	LaneID           string `json:"lane_id"`
	ExpectedRevision int    `json:"expected_revision" jsonschema:"the lane's current revision from get_delivery, so closing an already-superseded view is never silently accepted"`
	// Outcome is accepted or failed - a lane's only two terminal states.
	// Failing a lane is a real outcome, not an error: it records that the
	// work was attempted and did not land.
	Outcome       string                             `json:"outcome" jsonschema:"accepted | failed"`
	Summary       string                             `json:"summary" jsonschema:"what was actually done on this lane"`
	Verifications []CompleteDeliveryLaneVerification `json:"verifications,omitempty" jsonschema:"what you verified and what it showed; punakawan records this, it never judges whether a dimension passed. Any of the six dimensions you leave out stays pending, and pending verification is reported as a gap when the delivery completes"`
	WorkerID      string                             `json:"worker_id,omitempty" jsonschema:"who did the work; defaults to the connected client's name"`
	SessionID     string                             `json:"session_id,omitempty" jsonschema:"the delivery session this work was done under; also the implementer identity the review-independence check runs against"`
	// IndependenceOverrideReason is required when the session that did the
	// work is also the one concluding the review, which is the ordinary
	// case for a single-agent delivery. Stating a reason keeps that
	// visible in the audit trail instead of silent.
	IndependenceOverrideReason string `json:"independence_override_reason,omitempty" jsonschema:"required when session_id also implemented this lane - say why no independent reviewer looked at it"`
	IdempotencyKey             string `json:"idempotency_key,omitempty"`
}

type CompleteDeliveryLaneVerification struct {
	Name    string `json:"name" jsonschema:"logic | unit | integration | quality | e2e | ci"`
	Status  string `json:"status" jsonschema:"passed | failed | pending"`
	Summary string `json:"summary,omitempty"`
	// Evidence names an already-recorded artifact this outcome rests on;
	// it is a reference, not a place to paste output.
	Evidence string `json:"evidence_id,omitempty"`
}

type CompleteDeliveryLaneOutput struct {
	Lane protocol.DeliveryLane `json:"lane"`
	View delivery.DeliveryView `json:"view"`
}

func completeDeliveryLaneHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CompleteDeliveryLaneInput) (*mcp.CallToolResult, CompleteDeliveryLaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CompleteDeliveryLaneInput) (*mcp.CallToolResult, CompleteDeliveryLaneOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, CompleteDeliveryLaneOutput{}, err
		}
		workerID := in.WorkerID
		if workerID == "" {
			workerID = mcpClientName(req)
		}
		sessionID := in.SessionID
		if sessionID == "" {
			sessionID = workerID
		}
		verifications := make([]delivery.LaneVerification, 0, len(in.Verifications))
		for _, v := range in.Verifications {
			verifications = append(verifications, delivery.LaneVerification{
				Name:     protocol.VerificationDimensionName(v.Name),
				Status:   protocol.VerificationDimensionStatus(v.Status),
				Summary:  v.Summary,
				Evidence: v.Evidence,
			})
		}
		lane, err := store.CompleteLaneWork(ctx, delivery.LaneCompletionRequest{
			IdempotencyKey:             in.IdempotencyKey,
			OrchestrationID:            in.OrchestrationID,
			LaneID:                     in.LaneID,
			ExpectedRevision:           in.ExpectedRevision,
			WorkerID:                   workerID,
			SessionID:                  sessionID,
			Verifications:              verifications,
			Outcome:                    protocol.DeliveryLaneStatus(in.Outcome),
			Summary:                    in.Summary,
			IndependenceOverrideReason: in.IndependenceOverrideReason,
		})
		if err != nil {
			return nil, CompleteDeliveryLaneOutput{}, fmt.Errorf("mcpserver: complete delivery lane: %w", err)
		}
		// Finishing a lane engages whichever issue that lane's task was
		// mapped to; a lane with no mapped task simply has nothing to touch.
		if lane.ParentTaskId != nil && *lane.ParentTaskId != "" {
			touchJiraIssueForTask(ctx, store, "touch:lane:"+lane.Id+":"+string(lane.Status), in.OrchestrationID, *lane.ParentTaskId, sessionID, time.Now().UTC())
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationID)
		if err != nil {
			return nil, CompleteDeliveryLaneOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, CompleteDeliveryLaneOutput{Lane: *lane, View: *view}, nil
	}
}
