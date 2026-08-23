// tools_delivery_pr.go adds the MCP tools for turning a verified lane into
// a published pull request, running a bounded repair loop when review/CI
// comes back lacking, and checking merge-readiness
// against a project's required verification gates. Kept separate from
// tools_delivery.go, which already covers lease/worktree/role-stage tools.
//
// None of these tools ever call a GitHub merge/close endpoint - publish_pr
// only opens a pull request, and check_merge_readiness only reports an
// answer, it never acts on it.
package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// PublishPrInput is publish_pr's input.
type PublishPrInput struct {
	OrchestrationId  string `json:"orchestration_id"`
	LaneId           string `json:"lane_id"`
	LeaseToken       string `json:"lease_token" jsonschema:"must match the lane's current lease"`
	ExpectedRevision int    `json:"expected_revision"`

	RepoSlug   string `json:"repo_slug" jsonschema:"owner/repo the pull request is opened against"`
	BaseBranch string `json:"base_branch"`
	HeadBranch string `json:"head_branch"`
	Title      string `json:"title"`
	Body       string `json:"body" jsonschema:"the pull request body, in the caller's own words - this tool templates nothing"`
}

func publishPrHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, PublishPrInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in PublishPrInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		out, err := publishPr(ctx, store, a.AdapterRegistry, in)
		return nil, out, err
	}
}

// publishPr is publishPrHandler's core logic, split out the same way
// createPrHandler delegates to createPr: registry is the same narrow
// adapterGateProvider interface createPr already depends on (never a
// second way to get a Gate), so a test can substitute a Registry pointed
// at a fake/local adapter spec instead of one that spawns a real GitHub
// adapter subprocess.
func publishPr(ctx context.Context, store *delivery.Store, registry adapterGateProvider, in PublishPrInput) (LaneOutput, error) {
	provider := lazyGitHubPRProvider{registry: registry}
	lane, err := store.PublishPullRequest(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.LeaseToken, provider, delivery.PublishPRRequest{
		RepoSlug:   in.RepoSlug,
		BaseBranch: in.BaseBranch,
		HeadBranch: in.HeadBranch,
		Title:      in.Title,
		Body:       in.Body,
	}, in.ExpectedRevision)
	if err != nil {
		return LaneOutput{}, fmt.Errorf("mcpserver: publish pull request: %w", err)
	}
	return LaneOutput{Lane: *lane}, nil
}

// lazyGitHubPRProvider defers resolving a github adapter Gate until Publish
// is actually called. PublishPullRequest never calls PRProvider.Publish for
// a lane that already has a published pull request (its idempotent-resume
// path) - resolving the Gate eagerly in publishPr would otherwise force
// every resumed publish_pr call to require a configured github adapter for
// no reason, even when it is never going to be used.
type lazyGitHubPRProvider struct {
	registry adapterGateProvider
}

func (p lazyGitHubPRProvider) Publish(ctx context.Context, req delivery.PublishPRRequest) (int, string, error) {
	gate, err := p.registry.Gate(ctx, "github")
	if err != nil {
		return 0, "", fmt.Errorf("no github adapter configured: %w", err)
	}
	return delivery.GitHubPRProvider{Gate: gate}.Publish(ctx, req)
}

func (p lazyGitHubPRProvider) ProviderName() protocol.DeliveryLanePrProvider {
	return protocol.DeliveryLanePrProviderGithub
}

// RecordVerificationDimensionInput is record_verification_dimension's input.
type RecordVerificationDimensionInput struct {
	OrchestrationId  string                               `json:"orchestration_id"`
	LaneId           string                               `json:"lane_id"`
	ExpectedRevision int                                  `json:"expected_revision"`
	Name             protocol.VerificationDimensionName   `json:"name"`
	Status           protocol.VerificationDimensionStatus `json:"status"`
	EvidenceId       string                               `json:"evidence_id,omitempty"`
	Summary          string                               `json:"summary,omitempty"`
}

func recordVerificationDimensionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RecordVerificationDimensionInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RecordVerificationDimensionInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		if err := store.RecordVerificationDimension(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.Name, in.Status, in.EvidenceId, in.Summary, in.ExpectedRevision); err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: record verification dimension: %w", err)
		}
		lane, err := store.GetLane(ctx, in.OrchestrationId, in.LaneId)
		if err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: reload lane after recording verification dimension: %w", err)
		}
		return nil, LaneOutput{Lane: *lane}, nil
	}
}

// RecordCiCheckInput is record_ci_check's input.
type RecordCiCheckInput struct {
	OrchestrationId  string           `json:"orchestration_id"`
	LaneId           string           `json:"lane_id"`
	ExpectedRevision int              `json:"expected_revision"`
	Check            protocol.CICheck `json:"check"`
}

func recordCiCheckHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RecordCiCheckInput) (*mcp.CallToolResult, LaneOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RecordCiCheckInput) (*mcp.CallToolResult, LaneOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, LaneOutput{}, err
		}
		if err := store.RecordCICheck(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.Check, in.ExpectedRevision); err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: record ci check: %w", err)
		}
		lane, err := store.GetLane(ctx, in.OrchestrationId, in.LaneId)
		if err != nil {
			return nil, LaneOutput{}, fmt.Errorf("mcpserver: reload lane after recording ci check: %w", err)
		}
		return nil, LaneOutput{Lane: *lane}, nil
	}
}

// SubmitReviewConclusionInput is submit_review_conclusion's input.
// ImplementerSessionId is the caller's responsibility to know and supply -
// this tool does not capture it automatically from the role-stage flow.
// Conclusion.Id/LaneId/RecordedAt are required by protocol.ReviewConclusion's
// own schema but are unconditionally overwritten by RecordReviewConclusion,
// so any placeholder value satisfies them.
type SubmitReviewConclusionInput struct {
	OrchestrationId      string                    `json:"orchestration_id"`
	LaneId               string                    `json:"lane_id"`
	ExpectedRevision     int                       `json:"expected_revision"`
	Conclusion           protocol.ReviewConclusion `json:"conclusion"`
	ImplementerSessionId string                    `json:"implementer_session_id" jsonschema:"the session that implemented the attempt being reviewed, so a same-session review conclusion can be rejected unless it explicitly overrides independence"`
}

// SubmitReviewConclusionOutput is submit_review_conclusion's output.
type SubmitReviewConclusionOutput struct {
	Conclusion protocol.ReviewConclusion `json:"conclusion"`
}

func submitReviewConclusionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, SubmitReviewConclusionInput) (*mcp.CallToolResult, SubmitReviewConclusionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in SubmitReviewConclusionInput) (*mcp.CallToolResult, SubmitReviewConclusionOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, SubmitReviewConclusionOutput{}, err
		}
		stored, err := store.RecordReviewConclusion(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.Conclusion, in.ImplementerSessionId, in.ExpectedRevision)
		if err != nil {
			return nil, SubmitReviewConclusionOutput{}, fmt.Errorf("mcpserver: submit review conclusion: %w", err)
		}
		return nil, SubmitReviewConclusionOutput{Conclusion: *stored}, nil
	}
}

// StartRepairCycleInput is start_repair_cycle's input.
type StartRepairCycleInput struct {
	OrchestrationId  string   `json:"orchestration_id"`
	LaneId           string   `json:"lane_id"`
	ExpectedRevision int      `json:"expected_revision"`
	Reason           string   `json:"reason" jsonschema:"required; why this attempt needs another pass"`
	EvidenceIds      []string `json:"evidence_ids,omitempty"`
}

// StartRepairCycleOutput is start_repair_cycle's output. Escalated=true with
// Reason set is the normal, expected result once a lane's repair-cycle
// budget is exhausted - not a tool-call error, since it is honest, actionable
// information for the caller to act on (notify a human), the same way
// create_pr's created=false is not an error.
type StartRepairCycleOutput struct {
	Lane      protocol.DeliveryLane `json:"lane"`
	Escalated bool                  `json:"escalated"`
	Reason    string                `json:"reason,omitempty"`
}

func startRepairCycleHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, StartRepairCycleInput) (*mcp.CallToolResult, StartRepairCycleOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in StartRepairCycleInput) (*mcp.CallToolResult, StartRepairCycleOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, StartRepairCycleOutput{}, err
		}
		lane, err := store.StartRepairCycle(ctx, delivery.NewID(), in.OrchestrationId, in.LaneId, in.Reason, in.EvidenceIds, in.ExpectedRevision)
		if errors.Is(err, delivery.ErrRepairCyclesExhausted) {
			return nil, StartRepairCycleOutput{Lane: *lane, Escalated: true, Reason: in.Reason}, nil
		}
		if err != nil {
			return nil, StartRepairCycleOutput{}, fmt.Errorf("mcpserver: start repair cycle: %w", err)
		}
		return nil, StartRepairCycleOutput{Lane: *lane}, nil
	}
}

// GetVerificationMatrixInput is get_verification_matrix's input.
type GetVerificationMatrixInput struct {
	OrchestrationId string `json:"orchestration_id"`
	LaneId          string `json:"lane_id"`
}

// GetVerificationMatrixOutput is get_verification_matrix's output.
type GetVerificationMatrixOutput struct {
	Matrix protocol.VerificationMatrix `json:"matrix"`
}

func getVerificationMatrixHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, GetVerificationMatrixInput) (*mcp.CallToolResult, GetVerificationMatrixOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in GetVerificationMatrixInput) (*mcp.CallToolResult, GetVerificationMatrixOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, GetVerificationMatrixOutput{}, err
		}
		matrix, err := store.BuildVerificationMatrix(ctx, in.OrchestrationId, in.LaneId)
		if err != nil {
			return nil, GetVerificationMatrixOutput{}, fmt.Errorf("mcpserver: build verification matrix: %w", err)
		}
		return nil, GetVerificationMatrixOutput{Matrix: *matrix}, nil
	}
}

// CheckMergeReadinessInput is check_merge_readiness's input. project_id is
// optional - when omitted, the lane's own project_id (from GetLane) is used
// to resolve the ProjectDeliveryProfile, the same lane-to-project-to-profile
// path build_lane_context's Store method already follows.
type CheckMergeReadinessInput struct {
	OrchestrationId string `json:"orchestration_id"`
	LaneId          string `json:"lane_id"`
	ProjectId       string `json:"project_id,omitempty" jsonschema:"defaults to the lane's own project_id"`
}

// CheckMergeReadinessOutput is check_merge_readiness's output.
type CheckMergeReadinessOutput struct {
	Ready        bool     `json:"ready"`
	FailingGates []string `json:"failing_gates"`
}

func checkMergeReadinessHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CheckMergeReadinessInput) (*mcp.CallToolResult, CheckMergeReadinessOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in CheckMergeReadinessInput) (*mcp.CallToolResult, CheckMergeReadinessOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, CheckMergeReadinessOutput{}, err
		}
		projectID := in.ProjectId
		if projectID == "" {
			lane, err := store.GetLane(ctx, in.OrchestrationId, in.LaneId)
			if err != nil {
				return nil, CheckMergeReadinessOutput{}, fmt.Errorf("mcpserver: resolve lane's project: %w", err)
			}
			projectID = lane.ProjectId
		}
		profile, err := store.GetDeliveryProfile(ctx, projectID)
		if err != nil {
			return nil, CheckMergeReadinessOutput{}, fmt.Errorf("mcpserver: load delivery profile for project %s: %w", projectID, err)
		}
		ready, failingGates, err := store.MergeReadiness(ctx, in.OrchestrationId, in.LaneId, profile)
		if err != nil {
			return nil, CheckMergeReadinessOutput{}, fmt.Errorf("mcpserver: check merge readiness: %w", err)
		}
		if failingGates == nil {
			failingGates = []string{}
		}
		return nil, CheckMergeReadinessOutput{Ready: ready, FailingGates: failingGates}, nil
	}
}
