package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
)

// QueueJiraWriteOutput is cancelJiraWriteIntentHandler's result shape,
// naming the pre-outbox jira_write_intents row it acted on (see
// delivery.Store.CancelJiraWriteIntent); queuing and executing an intent
// through that table no longer happens - every Jira write is now enqueued
// directly into the durable provider outbox (internal/outbox) by the domain
// code that decides to make it.
type QueueJiraWriteOutput struct {
	Intent delivery.JiraWriteIntent `json:"intent"`
	View   delivery.DeliveryView    `json:"view"`
}

type CancelJiraWriteIntentInput struct {
	IntentID       string `json:"intent_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func cancelJiraWriteIntentHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CancelJiraWriteIntentInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CancelJiraWriteIntentInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		intent, err := store.CancelJiraWriteIntent(ctx, key, in.IntentID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, fmt.Errorf("mcpserver: cancel Jira write intent: %w", err)
		}
		execution, err := store.GetExecution(ctx, intent.ExecutionID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		return nil, QueueJiraWriteOutput{Intent: *intent, View: *view}, nil
	}
}
