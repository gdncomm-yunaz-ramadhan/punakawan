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

// defaultLeaseSeconds is used when a caller omits lease_seconds - long
// enough that a normal heartbeat cadence never races expiry, short
// enough that a crashed worker's lane returns to runnable within a
// reasonable time.
const defaultLeaseSeconds = 300

func openDeliveryStore(ctx context.Context, a *app.App) (*delivery.Store, error) {
	db, err := a.OpenStorage(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open storage kernel: %w", err)
	}
	return delivery.NewStore(db), nil
}

// ListRunnableLanesInput is list_runnable_lanes' input.
type ListRunnableLanesInput struct {
	OrchestrationId string `json:"orchestration_id"`
}

// ListRunnableLanesOutput is list_runnable_lanes' output: every lane
// currently on the frontier (no unresolved predecessor), sorted by
// creation time.
type ListRunnableLanesOutput struct {
	Lanes []protocol.DeliveryLane `json:"lanes"`
}

func listRunnableLanesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ListRunnableLanesInput) (*mcp.CallToolResult, ListRunnableLanesOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListRunnableLanesInput) (*mcp.CallToolResult, ListRunnableLanesOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, ListRunnableLanesOutput{}, err
		}
		lanes, err := store.SyncFrontier(ctx, delivery.NewID(), in.OrchestrationId)
		if err != nil {
			return nil, ListRunnableLanesOutput{}, fmt.Errorf("mcpserver: sync frontier: %w", err)
		}
		out := ListRunnableLanesOutput{Lanes: []protocol.DeliveryLane{}}
		for _, l := range lanes {
			if l.Status == protocol.DeliveryLaneStatusRunnable {
				out.Lanes = append(out.Lanes, *l)
			}
		}
		return nil, out, nil
	}
}

// ClaimLaneInput is claim_lane's input.
type ClaimLaneInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	LaneId           string `json:"lane_id"`
	ExpectedRevision int    `json:"expected_revision" jsonschema:"the lane's revision from list_runnable_lanes, so a lane someone else already claimed is never leased twice"`
	WorkerId         string `json:"worker_id" jsonschema:"identifies the claiming session/agent, for observability"`
	LeaseSeconds     int    `json:"lease_seconds,omitempty" jsonschema:"how long before the lease expires without a heartbeat; defaults to 300"`
}

// LaneOutput wraps one DeliveryLane, used by every tool in this file
// that returns a single lane's post-transition state.
type LaneOutput struct {
	Lane protocol.DeliveryLane `json:"lane"`
}

func claimLaneHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ClaimLaneInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ClaimLaneInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		leaseSeconds := in.LeaseSeconds
		if leaseSeconds <= 0 {
			leaseSeconds = defaultLeaseSeconds
		}
		lane, err := store.GrantLease(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.ExpectedRevision, in.WorkerId, time.Duration(leaseSeconds)*time.Second)
		if err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: claim lane: %w", err)
		}
		return nil, LaneOutput{Lane: *lane}, nil
	}
}

// CreateWorktreeInput is create_worktree's input.
type CreateWorktreeInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	LaneId           string `json:"lane_id"`
	ExpectedRevision int    `json:"expected_revision" jsonschema:"the lane's current revision, so a concurrent change is never overwritten"`
}

func createWorktreeHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CreateWorktreeInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CreateWorktreeInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		lane, err := store.CreateWorktree(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.ExpectedRevision)
		if err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: create worktree: %w", err)
		}
		return nil, LaneOutput{Lane: *lane}, nil
	}
}

// LeaseActionInput is the shared input for heartbeat_lease, complete_lease,
// and reject_lease - every call that must present the lease token it was
// handed by claim_lane.
type LeaseActionInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	LaneId           string `json:"lane_id"`
	LeaseToken       string `json:"lease_token" jsonschema:"the token returned by claim_lane; a stale or mismatched token is rejected"`
	ExpectedRevision int    `json:"expected_revision"`
	LeaseSeconds     int    `json:"lease_seconds,omitempty" jsonschema:"heartbeat_lease only: how long to extend the lease; defaults to 300"`
}

func heartbeatLeaseHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, LeaseActionInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in LeaseActionInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		leaseSeconds := in.LeaseSeconds
		if leaseSeconds <= 0 {
			leaseSeconds = defaultLeaseSeconds
		}
		lane, err := store.Heartbeat(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.LeaseToken, in.ExpectedRevision, time.Duration(leaseSeconds)*time.Second)
		if err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: heartbeat lease: %w", err)
		}
		return nil, LaneOutput{Lane: *lane}, nil
	}
}

func completeLeaseHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, LeaseActionInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in LeaseActionInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		lane, err := store.CompleteLease(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.LeaseToken, in.ExpectedRevision)
		if err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: complete lease: %w", err)
		}
		return nil, LaneOutput{Lane: *lane}, nil
	}
}

func rejectLeaseHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, LeaseActionInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in LeaseActionInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		lane, err := store.RejectLease(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.LeaseToken, in.ExpectedRevision)
		if err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: reject lease: %w", err)
		}
		return nil, LaneOutput{Lane: *lane}, nil
	}
}

// ReportDiscoveredDependencyInput is report_discovered_dependency's
// input: a worker reports mid-execution that from_task_id turns out to
// require to_task_id, something the planner did not know about
// upfront.
type ReportDiscoveredDependencyInput struct {
	OrchestrationId string  `json:"orchestration_id"`
	FromTaskId      string  `json:"from_task_id" jsonschema:"the task that turned out to depend on to_task_id"`
	ToTaskId        string  `json:"to_task_id" jsonschema:"the task from_task_id actually depends on"`
	Evidence        string  `json:"evidence" jsonschema:"why this dependency was discovered; required, never accepted on confidence alone"`
	Confidence      float64 `json:"confidence,omitempty" jsonschema:"defaults to 1.0 when omitted or non-positive"`
}

// ReportDiscoveredDependencyOutput returns the recorded edge plus
// every lane in the orchestration after the frontier resync - only the
// lane(s) actually affected by the new edge move to blocked; every
// unrelated lane's status is left exactly as it was.
type ReportDiscoveredDependencyOutput struct {
	Edge  protocol.DependencyEdge `json:"edge"`
	Lanes []protocol.DeliveryLane `json:"lanes"`
}

func reportDiscoveredDependencyHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReportDiscoveredDependencyInput) (*mcp.CallToolResult, ReportDiscoveredDependencyOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ReportDiscoveredDependencyInput) (*mcp.CallToolResult, ReportDiscoveredDependencyOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, ReportDiscoveredDependencyOutput{}, err
		}
		confidence := in.Confidence
		if confidence <= 0 {
			confidence = 1.0
		}
		edge, err := store.AddDependencyEdge(ctx, delivery.NewID(), delivery.NewID(), in.OrchestrationId, in.FromTaskId, in.ToTaskId,
			protocol.DependencyEdgeTypeRequires, protocol.DependencyEdgeOriginModelInference, confidence, in.Evidence)
		if err != nil {
			return nil, ReportDiscoveredDependencyOutput{}, fmt.Errorf("mcpserver: report discovered dependency: %w", err)
		}

		lanes, err := store.SyncFrontier(ctx, delivery.NewID(), in.OrchestrationId)
		if err != nil {
			return nil, ReportDiscoveredDependencyOutput{}, fmt.Errorf("mcpserver: sync frontier: %w", err)
		}
		out := ReportDiscoveredDependencyOutput{Edge: *edge, Lanes: []protocol.DeliveryLane{}}
		for _, l := range lanes {
			out.Lanes = append(out.Lanes, *l)
		}
		return nil, out, nil
	}
}

// RunInLaneInput is run_in_lane's input: the only execution surface
// this delivery domain exposes, scoped strictly to the caller's own
// leased lane worktree - a worker can never touch anything outside its
// own lease's scope through this tool, regardless of what command or
// arguments it passes.
type RunInLaneInput struct {
	OrchestrationId string   `json:"orchestration_id"`
	LaneId          string   `json:"lane_id"`
	LeaseToken      string   `json:"lease_token" jsonschema:"must match the lane's current lease; a stale or mismatched token is rejected"`
	Command         string   `json:"command" jsonschema:"executable name, resolved via PATH only - never treated as an absolute path"`
	Args            []string `json:"args,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds,omitempty" jsonschema:"defaults to the supervisor's own default when omitted"`
}

// RunInLaneOutput is the completed command's result: a non-zero exit
// code is reported here, not as a tool error - only a command that
// could not be run at all (disallowed scope, failed to start, timeout)
// is a tool error.
type RunInLaneOutput struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	Truncated bool   `json:"truncated"`
}

func runInLaneHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RunInLaneInput) (*mcp.CallToolResult, RunInLaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RunInLaneInput) (*mcp.CallToolResult, RunInLaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, RunInLaneOutput{}, err
		}
		timeout := time.Duration(in.TimeoutSeconds) * time.Second
		res, err := store.RunInLane(ctx, in.OrchestrationId, in.LaneId, in.LeaseToken, in.Command, in.Args, timeout)
		if err != nil {
			return nil, RunInLaneOutput{}, fmt.Errorf("mcpserver: run in lane: %w", err)
		}
		return nil, RunInLaneOutput{
			Stdout:    string(res.Stdout),
			Stderr:    string(res.Stderr),
			ExitCode:  res.ExitCode,
			Truncated: res.Truncated,
		}, nil
	}
}
