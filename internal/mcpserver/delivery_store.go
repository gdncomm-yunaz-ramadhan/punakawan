package mcpserver

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
	"github.com/ygrip/punakawan/internal/workflowdef"
)

type workflowDefinitionResolver struct {
	store *workflowdef.Store
}

func (r workflowDefinitionResolver) ValidateEnabled(_ context.Context, id string) error {
	definition, err := r.store.Get(id)
	if err != nil {
		return err
	}
	if !definition.Enabled {
		return fmt.Errorf("workflow definition %q is disabled", id)
	}
	return nil
}

func (r workflowDefinitionResolver) RequiredRoleStages(_ context.Context, id string) (map[string]bool, error) {
	definition, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	stages := make(map[string]bool, len(definition.Roles))
	for role, restriction := range definition.Roles {
		stages[role] = restriction.Required
	}
	return stages, nil
}

// OpenDeliveryStore is the single MCP binding from the public Delivery tools
// to the delivery domain and its configured integration hooks.
func OpenDeliveryStore(ctx context.Context, a *app.App) (*delivery.Store, error) {
	db, err := a.OpenStorage(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open storage kernel: %w", err)
	}
	definitionStore, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open workflow definition store: %w", err)
	}
	resolver := workflowDefinitionResolver{store: definitionStore}
	opts := []delivery.StoreOption{delivery.WithWorkflowDefinitionResolver(resolver)}
	if cfg, err := a.JiraWorkflow(); err != nil {
		slog.Warn("mcpserver: load Jira workflow config; continuing without Jira hooks", "error", err)
	} else {
		hookStore := delivery.NewStore(db, delivery.WithWorkflowDefinitionResolver(resolver))
		opts = append(opts, delivery.WithHooks(jirahooks.NewJiraHook(db, hookStore, a.AdapterRegistry, cfg)))
		if cfg.LogWork {
			opts = append(opts, delivery.WithRequiredJiraWorkLogs())
		}
	}
	return delivery.NewStore(db, opts...), nil
}
