package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/roles"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// defaultLeaseSeconds is used when a caller omits lease_seconds - long
// enough that a normal heartbeat cadence never races expiry, short
// enough that a crashed worker's lane returns to runnable within a
// reasonable time.
const defaultLeaseSeconds = 300

// workflowDefinitionResolver adapts internal/workflowdef.Store to
// delivery.WorkflowDefinitionResolver, the only place in this server
// where internal/delivery's decoupling interface is bound to the
// concrete workflowdef package - every delivery.Store call site here
// gets the same binding through openDeliveryStore below.
type workflowDefinitionResolver struct {
	store *workflowdef.Store
}

func (r workflowDefinitionResolver) ValidateEnabled(ctx context.Context, id string) error {
	def, err := r.store.Get(id)
	if err != nil {
		return err
	}
	if !def.Enabled {
		return fmt.Errorf("workflow definition %q is disabled", id)
	}
	return nil
}

func (r workflowDefinitionResolver) RequiredRoleStages(ctx context.Context, id string) (map[string]bool, error) {
	def, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(def.Roles))
	for role, restriction := range def.Roles {
		out[role] = restriction.Required
	}
	return out, nil
}

func openDeliveryStore(ctx context.Context, a *app.App) (*delivery.Store, error) {
	db, err := a.OpenStorage(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open storage kernel: %w", err)
	}
	defStore, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open workflow definition store: %w", err)
	}
	return delivery.NewStore(db, delivery.WithWorkflowDefinitionResolver(workflowDefinitionResolver{store: defStore})), nil
}

// ListRunnableLanesInput is list_runnable_lanes' input.
type ListRunnableLanesInput struct {
	OrchestrationId string `json:"orchestration_id"`
}

// ListRunnableLanesOutput is list_runnable_lanes' output: every lane
// currently on the frontier (no unresolved predecessor), ranked best
// candidate first - longest critical path, then highest unlock count,
// then oldest lane first - so a caller that always claims the first
// entry never starves older or higher-value work.
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
		var runnable []*protocol.DeliveryLane
		for _, l := range lanes {
			if l.Status == protocol.DeliveryLaneStatusRunnable {
				runnable = append(runnable, l)
			}
		}
		_, edges, err := store.ListGraph(ctx, in.OrchestrationId)
		if err != nil {
			return nil, ListRunnableLanesOutput{}, fmt.Errorf("mcpserver: list graph: %w", err)
		}
		delivery.RankLanes(runnable, edges)

		out := ListRunnableLanesOutput{Lanes: []protocol.DeliveryLane{}}
		for _, l := range runnable {
			out.Lanes = append(out.Lanes, *l)
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

// BuildLaneContextInput is build_lane_context's input.
type BuildLaneContextInput struct {
	OrchestrationId string `json:"orchestration_id"`
	LaneId          string `json:"lane_id"`
}

// BuildLaneContextOutput is a lane's bounded, hashed context: its
// pinned requirement sources, project delivery profile, and exact base
// commit, plus a digest identifying this exact combination.
type BuildLaneContextOutput struct {
	Lane       protocol.DeliveryLane           `json:"lane"`
	ParentTask protocol.ParentTask             `json:"parent_task"`
	Sources    []protocol.RequirementSource    `json:"sources"`
	Profile    protocol.ProjectDeliveryProfile `json:"profile"`
	BaseSha    string                          `json:"base_sha"`
	Digest     string                          `json:"digest"`
}

func buildLaneContextHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, BuildLaneContextInput) (*mcp.CallToolResult, BuildLaneContextOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in BuildLaneContextInput) (*mcp.CallToolResult, BuildLaneContextOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, BuildLaneContextOutput{}, err
		}
		lc, err := store.BuildLaneContext(ctx, in.OrchestrationId, in.LaneId)
		if err != nil {
			return nil, BuildLaneContextOutput{}, fmt.Errorf("mcpserver: build lane context: %w", err)
		}
		out := BuildLaneContextOutput{
			Lane:       *lc.Lane,
			ParentTask: *lc.ParentTask,
			Sources:    []protocol.RequirementSource{},
			Profile:    *lc.Profile,
			BaseSha:    lc.BaseSha,
			Digest:     lc.Digest,
		}
		for _, src := range lc.Sources {
			out.Sources = append(out.Sources, *src)
		}
		return nil, out, nil
	}
}

// SubmitLaneStageOutput is submit_lane_review's output: the record it
// validated and persisted, and the lane's state right after that stage
// was recorded.
type SubmitLaneStageOutput struct {
	Lane     protocol.DeliveryLane `json:"lane"`
	RecordId string                `json:"record_id"`
}

func recordLaneStage(ctx context.Context, a *app.App, orchestrationID, laneID, leaseToken string, stage delivery.RoleStage, recordID string, expectedRevision int) (SubmitLaneStageOutput, error) {
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		return SubmitLaneStageOutput{}, err
	}
	lane, err := store.RecordRoleStage(ctx, delivery.NewID(), orchestrationID, laneID, leaseToken, stage, recordID, expectedRevision)
	if err != nil {
		return SubmitLaneStageOutput{}, fmt.Errorf("mcpserver: record role stage: %w", err)
	}
	return SubmitLaneStageOutput{Lane: *lane, RecordId: recordID}, nil
}

// SubmitLaneReviewInput is submit_lane_review's input. One tool records
// all four role stages: role picks which stage this call is, and the
// payload field named after that role carries its content. The other
// three payload fields are ignored, so a caller only ever fills one in.
//
// The four payloads are structurally different knowledge records, so
// exactly one of them is meaningful per call - a conditional requirement
// a schema inferred from a Go struct cannot express. All four are
// therefore optional in the schema, and the real per-role requirement is
// enforced by internal/roles' own validators, which reject an empty or
// incomplete payload with a message naming the missing field.
type SubmitLaneReviewInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	LaneId           string `json:"lane_id"`
	LeaseToken       string `json:"lease_token" jsonschema:"must match the lane's current lease"`
	ExpectedRevision int    `json:"expected_revision"`
	Role             string `json:"role" jsonschema:"which stage this call records: semar, gareng, petruk, or bagong"`
	Title            string `json:"title" jsonschema:"human-readable title for this record"`

	SemarSynthesis protocol.KnowledgeRecordSemarSynthesis `json:"semar_synthesis,omitempty" jsonschema:"the payload when role is semar; ignored for every other role"`
	GarengReview   protocol.KnowledgeRecordGarengReview   `json:"gareng_review,omitempty" jsonschema:"the payload when role is gareng; ignored for every other role"`
	PetrukPlan     protocol.KnowledgeRecordPetrukPlan     `json:"petruk_plan,omitempty" jsonschema:"the payload when role is petruk; ignored for every other role"`
	BagongReview   protocol.KnowledgeRecordBagongReview   `json:"bagong_review,omitempty" jsonschema:"the payload when role is bagong; ignored for every other role"`
}

// laneRoleStages maps a validated role name onto the delivery stage it
// records. The names are the same four internal/delivery already uses as
// its workflow-definition role keys, so the two vocabularies cannot drift.
var laneRoleStages = map[protocol.EventRole]delivery.RoleStage{
	protocol.EventRoleSemar:  delivery.RoleStageSemar,
	protocol.EventRoleGareng: delivery.RoleStageGareng,
	protocol.EventRolePetruk: delivery.RoleStagePetruk,
	protocol.EventRoleBagong: delivery.RoleStageBagong,
}

func submitLaneReviewHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitLaneReviewInput) (*mcp.CallToolResult, SubmitLaneStageOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitLaneReviewInput) (*mcp.CallToolResult, SubmitLaneStageOutput, error) {
		role, err := validateLaneRole(in.Role)
		if err != nil {
			return nil, SubmitLaneStageOutput{}, err
		}

		kstore, err := a.OpenKnowledge()
		if err != nil {
			return nil, SubmitLaneStageOutput{}, fmt.Errorf("mcpserver: open knowledge store: %w", err)
		}

		// Petruk plans against Gareng's review, so a review still carrying
		// unresolved blocking findings has to be cleared before a plan built
		// on top of it can mean anything. None of the other three stages has
		// a precondition beyond the ordering RecordRoleStage already enforces.
		if role == protocol.EventRolePetruk {
			if err := requireClearGarengReview(ctx, a, kstore, in.OrchestrationId, in.LaneId); err != nil {
				return nil, SubmitLaneStageOutput{}, err
			}
		}

		var rec protocol.KnowledgeRecord
		switch role {
		case protocol.EventRoleSemar:
			rec, err = roles.SubmitSemarSynthesis(kstore, recordID(a, "semar", delivery.NewID()), in.Title, in.SemarSynthesis)
		case protocol.EventRoleGareng:
			rec, err = roles.SubmitGarengReview(kstore, recordID(a, "gareng", delivery.NewID()), in.Title, in.GarengReview)
		case protocol.EventRolePetruk:
			rec, err = roles.SubmitPetrukPlan(kstore, recordID(a, "petruk", delivery.NewID()), in.Title, in.PetrukPlan)
		case protocol.EventRoleBagong:
			rec, err = roles.SubmitBagongReview(kstore, recordID(a, "bagong", delivery.NewID()), in.Title, in.BagongReview)
		}
		if err != nil {
			return nil, SubmitLaneStageOutput{}, err
		}

		out, err := recordLaneStage(ctx, a, in.OrchestrationId, in.LaneId, in.LeaseToken, laneRoleStages[role], rec.Id, in.ExpectedRevision)
		if err != nil {
			return nil, SubmitLaneStageOutput{}, err
		}
		return nil, out, nil
	}
}

// requireClearGarengReview refuses a Petruk plan while the lane's recorded
// Gareng review still lists blocking findings. Gareng is optional by
// default, so a lane with no Gareng review recorded yet passes here and
// Petruk proceeds straight after Semar - this check only fires once a
// Gareng review actually exists and needs resolving.
func requireClearGarengReview(ctx context.Context, a *app.App, kstore *knowledge.Store, orchestrationID, laneID string) error {
	store, err := openDeliveryStore(ctx, a)
	if err != nil {
		return err
	}
	lane, err := store.GetLane(ctx, orchestrationID, laneID)
	if err != nil {
		return err
	}
	if lane.GarengRecordId == nil || *lane.GarengRecordId == "" {
		return nil
	}
	garengRec, err := kstore.Get(*lane.GarengRecordId)
	if err != nil {
		return fmt.Errorf("mcpserver: resolve this lane's gareng review: %w", err)
	}
	if garengRec.GarengReview != nil && len(garengRec.GarengReview.BlockingFindings) > 0 {
		return fmt.Errorf("mcpserver: gareng's review has unresolved blocking findings (%v); resubmit gareng's review once resolved before petruk can proceed", garengRec.GarengReview.BlockingFindings)
	}
	return nil
}
