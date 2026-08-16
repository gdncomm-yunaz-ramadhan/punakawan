// tools_deliverygraph.go implements the four delivery graph-authoring
// MCP tools: register_project, create_parent_task,
// create_lane, and add_dependency_edge. Each one wraps an
// already-built, already-tested internal/delivery Store method
// (store.go, parenttasks.go, and graph.go) - none of the underlying
// persistence, validation, or cycle/scope checking is reimplemented
// here. Punakawan itself never decides how a requirement should be
// decomposed into tasks or what depends on what (ADR-0016); these tools
// only let a connected agent record a decomposition it already made.
//
// Named tools_deliverygraph.go rather than tools_taskgraph.go: that
// name is already taken by the unrelated Beads task graph
// (submit_task_graph, §10) in tools_taskgraph.go.
package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// RegisterProjectInput is register_project's input. The project's id is
// generated server-side (delivery.NewID(), same as start_delivery does
// for its orchestration id) rather than caller-supplied. A project has
// no orchestration scope of its own - it can be registered before,
// during, or independently of any orchestration - so unlike the other
// three tools in this file there is no DeliveryView to refresh.
type RegisterProjectInput struct {
	Slug          string `json:"slug" jsonschema:"unique short identifier for this project; RegisterProject fails a duplicate slug"`
	RepositoryUrl string `json:"repository_url"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

// RegisterProjectOutput is register_project's output: the created
// project, nothing else.
type RegisterProjectOutput struct {
	Project protocol.DeliveryProject `json:"project"`
}

func registerProjectHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RegisterProjectInput) (*mcp.CallToolResult, RegisterProjectOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RegisterProjectInput) (*mcp.CallToolResult, RegisterProjectOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, RegisterProjectOutput{}, err
		}
		project, err := store.RegisterProject(ctx, delivery.NewID(), delivery.NewID(), in.Slug, in.RepositoryUrl, in.DefaultBranch)
		if err != nil {
			return nil, RegisterProjectOutput{}, fmt.Errorf("mcpserver: register project: %w", err)
		}
		return nil, RegisterProjectOutput{Project: *project}, nil
	}
}

// CreateParentTaskInput is create_parent_task's input: one or more
// already-captured requirement sources (ids visible in a DeliveryView's
// orchestration, or returned by start_delivery/answer_delivery_question)
// grouped into a new, unrouted graph node. The task's id is generated
// server-side, not caller-supplied.
type CreateParentTaskInput struct {
	OrchestrationId string   `json:"orchestration_id"`
	Title           string   `json:"title"`
	SourceIds       []string `json:"source_ids" jsonschema:"ids of already-captured requirement sources this task groups; at least one is required"`
}

// CreateParentTaskOutput is create_parent_task's output: the created
// task plus a refreshed DeliveryView for the orchestration it belongs
// to, so a caller sees the new task alongside everything else in one
// round trip.
type CreateParentTaskOutput struct {
	ParentTask protocol.ParentTask   `json:"parent_task"`
	View       delivery.DeliveryView `json:"view"`
}

func createParentTaskHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateParentTaskInput) (*mcp.CallToolResult, CreateParentTaskOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateParentTaskInput) (*mcp.CallToolResult, CreateParentTaskOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, CreateParentTaskOutput{}, err
		}
		task, err := store.CreateParentTask(ctx, delivery.NewID(), delivery.NewID(), in.OrchestrationId, in.Title, in.SourceIds)
		if err != nil {
			return nil, CreateParentTaskOutput{}, fmt.Errorf("mcpserver: create parent task: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, CreateParentTaskOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, CreateParentTaskOutput{ParentTask: *task, View: *view}, nil
	}
}

// CreateLaneInput is create_lane's input. ParentTaskId is optional - a
// lane may legitimately be created before a task is assigned to it, per
// CreateLane's own signature. When set, it must already exist in this
// orchestration and, once routed, must be routed to this same
// ProjectId (CreateLane returns ErrScopeMismatch otherwise). The lane's
// id is generated server-side, not caller-supplied.
type CreateLaneInput struct {
	OrchestrationId string `json:"orchestration_id"`
	ProjectId       string `json:"project_id"`
	ParentTaskId    string `json:"parent_task_id,omitempty" jsonschema:"optional - a lane can be created before a task is assigned to it; if set, must already exist in this orchestration and, once routed, must be routed to this same project_id"`
}

// CreateLaneOutput is create_lane's output: the created lane plus a
// refreshed DeliveryView for the orchestration it belongs to.
type CreateLaneOutput struct {
	Lane protocol.DeliveryLane `json:"lane"`
	View delivery.DeliveryView `json:"view"`
}

func createLaneHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateLaneInput) (*mcp.CallToolResult, CreateLaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateLaneInput) (*mcp.CallToolResult, CreateLaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, CreateLaneOutput{}, err
		}
		lane, err := store.CreateLane(ctx, delivery.NewID(), delivery.NewID(), in.OrchestrationId, in.ProjectId, in.ParentTaskId)
		if err != nil {
			return nil, CreateLaneOutput{}, fmt.Errorf("mcpserver: create lane: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, CreateLaneOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, CreateLaneOutput{Lane: *lane, View: *view}, nil
	}
}

// AddDependencyEdgeInput is add_dependency_edge's input: an up-front,
// explicitly-authored dependency between two already-created parent
// tasks - the caller (a human or a connected agent) stating a
// dependency it already knows about, before any lane on either task has
// been leased. This is distinct from report_discovered_dependency,
// which is for a worker mid-execution reporting a dependency it only
// found out about while running; that tool is untouched by this one.
// Origin is not exposed here - it is always recorded as "user" (an
// explicitly-authored edge, never a model inference) - and Confidence
// defaults to full confidence (1.0) rather than report_discovered_dependency's
// same default for the same reason: a stated fact, not an inference,
// starts out fully trusted.
type AddDependencyEdgeInput struct {
	OrchestrationId string  `json:"orchestration_id"`
	FromTaskId      string  `json:"from_task_id" jsonschema:"the task that depends on to_task_id"`
	ToTaskId        string  `json:"to_task_id" jsonschema:"the task from_task_id depends on"`
	EdgeType        string  `json:"edge_type" jsonschema:"requires | produces-input-for | serializes-with | informational; only requires and produces-input-for block execution"`
	Confidence      float64 `json:"confidence,omitempty" jsonschema:"defaults to 1.0 when omitted or non-positive"`
	Evidence        string  `json:"evidence,omitempty" jsonschema:"optional note on why this dependency holds"`
}

// AddDependencyEdgeOutput is add_dependency_edge's output: the created
// edge plus a refreshed DeliveryView for the orchestration it belongs
// to (an edge can move lanes from runnable to blocked, so the view
// after adding one is worth returning in the same round trip).
type AddDependencyEdgeOutput struct {
	Edge protocol.DependencyEdge `json:"edge"`
	View delivery.DeliveryView   `json:"view"`
}

func addDependencyEdgeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AddDependencyEdgeInput) (*mcp.CallToolResult, AddDependencyEdgeOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in AddDependencyEdgeInput) (*mcp.CallToolResult, AddDependencyEdgeOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, AddDependencyEdgeOutput{}, err
		}
		confidence := in.Confidence
		if confidence <= 0 {
			confidence = 1.0
		}
		edge, err := store.AddDependencyEdge(ctx, delivery.NewID(), delivery.NewID(), in.OrchestrationId, in.FromTaskId, in.ToTaskId,
			protocol.DependencyEdgeType(in.EdgeType), protocol.DependencyEdgeOriginUser, confidence, in.Evidence)
		if err != nil {
			return nil, AddDependencyEdgeOutput{}, fmt.Errorf("mcpserver: add dependency edge: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, in.OrchestrationId)
		if err != nil {
			return nil, AddDependencyEdgeOutput{}, fmt.Errorf("mcpserver: build delivery view: %w", err)
		}
		return nil, AddDependencyEdgeOutput{Edge: *edge, View: *view}, nil
	}
}
