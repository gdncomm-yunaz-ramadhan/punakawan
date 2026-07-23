package sources

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/panel/contract"
)

// TaskSource implements contract.TaskReader over bd, via internal/beads.
type TaskSource struct {
	App *app.App
}

func (t *TaskSource) checkWorkspace(workspaceID string) error {
	if workspaceID != t.App.Workspace.ID {
		return fmt.Errorf("sources: workspace %q is not available (only %q is)", workspaceID, t.App.Workspace.ID)
	}
	return nil
}

func (t *TaskSource) List(ctx context.Context, workspaceID string, filter contract.TaskFilter) ([]beads.ReadyIssue, error) {
	if err := t.checkWorkspace(workspaceID); err != nil {
		return nil, err
	}
	issues, err := beads.List(ctx, t.App.Supervisor, t.App.Workspace.Root, beads.ListOptions{
		Status:   filter.Status,
		Priority: filter.Priority,
		Type:     filter.Type,
		Assignee: filter.Assignee,
		Limit:    filter.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("sources: list tasks: %w", err)
	}
	if filter.ExternalIssue == "" {
		return issues, nil
	}
	var out []beads.ReadyIssue
	for _, issue := range issues {
		for _, label := range issue.Labels {
			if label == filter.ExternalIssue {
				out = append(out, issue)
				break
			}
		}
	}
	return out, nil
}

func (t *TaskSource) Get(ctx context.Context, workspaceID, taskID string) (beads.Issue, error) {
	if err := t.checkWorkspace(workspaceID); err != nil {
		return beads.Issue{}, err
	}
	issue, err := beads.Show(ctx, t.App.Supervisor, t.App.Workspace.Root, taskID)
	if err != nil {
		return beads.Issue{}, fmt.Errorf("sources: get task %q: %w", taskID, err)
	}
	return issue, nil
}

func (t *TaskSource) Dependencies(ctx context.Context, workspaceID string) (contract.TaskGraph, error) {
	if err := t.checkWorkspace(workspaceID); err != nil {
		return contract.TaskGraph{}, err
	}
	issues, err := beads.List(ctx, t.App.Supervisor, t.App.Workspace.Root, beads.ListOptions{})
	if err != nil {
		return contract.TaskGraph{}, fmt.Errorf("sources: task dependencies: %w", err)
	}

	graph := contract.TaskGraph{Nodes: issues}
	for _, issue := range issues {
		for _, dep := range issue.Dependencies {
			graph.Edges = append(graph.Edges, contract.TaskEdge{From: dep.IssueId, To: dep.DependsOnId, Type: dep.Type})
		}
	}
	return graph, nil
}
