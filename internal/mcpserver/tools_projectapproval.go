// tools_projectapproval.go implements request_project_approval: the
// missing progression step between "a project's lanes exist" and "a
// project has one explicit approval" that the pull-based lease model
// otherwise has no automatic trigger for (nothing in punakawand runs a
// background scheduler, per ADR-0016 - an agent decides when a project
// looks ready and calls this explicitly). It resolves every lane routed
// to project_id, checks each against MergeReadiness the same way
// check_merge_readiness does per-lane, and - only once every lane is
// ready - runs RunPreflight and calls CreateApprovalManifest, carrying a
// proposed worklog allocation derived from verified test-run hours and
// the project's configured Jira subtasks.
package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/worklogalloc"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// RequestProjectApprovalInput is request_project_approval's input.
// RequestedBy attributes the Jira subtask lookup this call may need to
// compute a proposed worklog allocation (list_jira_subtasks requires an
// attributed requester); this call itself does not act in a single
// role's name, so it defaults to semar when omitted rather than
// requiring every caller to supply one for a read that may not even
// touch Jira.
type RequestProjectApprovalInput struct {
	OrchestrationId string `json:"orchestration_id"`
	ProjectId       string `json:"project_id"`
	RequestedBy     string `json:"requested_by,omitempty" jsonschema:"one of semar|gareng|petruk|bagong; attributes any Jira subtask lookup this call makes while computing the proposed worklog allocation. Defaults to semar when omitted."`
}

// ProjectLaneGates is one not-yet-ready lane's blocking gates, mirroring
// check_merge_readiness's own failing_gates shape at project scope.
type ProjectLaneGates struct {
	LaneId       string   `json:"lane_id,omitempty"`
	FailingGates []string `json:"failing_gates"`
}

// RequestProjectApprovalOutput is request_project_approval's output.
// Manifest is set only when Ready is true - a not-ready result never
// creates or returns a manifest.
type RequestProjectApprovalOutput struct {
	Ready         bool                       `json:"ready"`
	NotReadyLanes []ProjectLaneGates         `json:"not_ready_lanes,omitempty"`
	Manifest      *protocol.ApprovalManifest `json:"manifest,omitempty"`
}

func requestProjectApprovalHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, RequestProjectApprovalInput) (*mcp.CallToolResult, RequestProjectApprovalOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in RequestProjectApprovalInput) (*mcp.CallToolResult, RequestProjectApprovalOutput, error) {
		store, err := openDeliveryStore(ctx, a)
		if err != nil {
			return nil, RequestProjectApprovalOutput{}, err
		}
		out, err := requestProjectApproval(ctx, req, a, store, in)
		return nil, out, err
	}
}

// requestProjectApproval is requestProjectApprovalHandler's core logic.
func requestProjectApproval(ctx context.Context, req *mcp.CallToolRequest, a *app.App, store *delivery.Store, in RequestProjectApprovalInput) (RequestProjectApprovalOutput, error) {
	lanes, err := store.ListLanes(ctx, in.OrchestrationId)
	if err != nil {
		return RequestProjectApprovalOutput{}, fmt.Errorf("mcpserver: list lanes: %w", err)
	}
	var projectLanes []*protocol.DeliveryLane
	for _, l := range lanes {
		if l.ProjectId == in.ProjectId {
			projectLanes = append(projectLanes, l)
		}
	}
	if len(projectLanes) == 0 {
		return RequestProjectApprovalOutput{Ready: false, NotReadyLanes: []ProjectLaneGates{
			{FailingGates: []string{"no lanes exist yet for this project in this orchestration"}},
		}}, nil
	}

	profile, err := store.GetDeliveryProfile(ctx, in.ProjectId)
	if err != nil {
		return RequestProjectApprovalOutput{}, fmt.Errorf("mcpserver: load delivery profile for project %s: %w", in.ProjectId, err)
	}

	var notReady []ProjectLaneGates
	parentTaskIDSet := map[string]bool{}
	laneIDs := make([]string, 0, len(projectLanes))
	for _, lane := range projectLanes {
		laneIDs = append(laneIDs, lane.Id)
		ready, failingGates, err := store.MergeReadiness(ctx, in.OrchestrationId, lane.Id, profile)
		if err != nil {
			return RequestProjectApprovalOutput{}, fmt.Errorf("mcpserver: check merge readiness for lane %s: %w", lane.Id, err)
		}
		if !ready {
			if failingGates == nil {
				failingGates = []string{}
			}
			notReady = append(notReady, ProjectLaneGates{LaneId: lane.Id, FailingGates: failingGates})
			continue
		}
		if lane.ParentTaskId != nil && *lane.ParentTaskId != "" {
			parentTaskIDSet[*lane.ParentTaskId] = true
		}
	}
	if len(notReady) > 0 {
		return RequestProjectApprovalOutput{Ready: false, NotReadyLanes: notReady}, nil
	}

	parentTaskIDs := make([]string, 0, len(parentTaskIDSet))
	for id := range parentTaskIDSet {
		parentTaskIDs = append(parentTaskIDs, id)
	}
	sort.Strings(parentTaskIDs)
	if len(parentTaskIDs) == 0 {
		// Every lane individually passed MergeReadiness, but none of them
		// is routed to a parent task - CreateApprovalManifest requires at
		// least one, and a manifest with no task backing it would have
		// nothing to name its own scope. Honest not-ready, not an error.
		return RequestProjectApprovalOutput{Ready: false, NotReadyLanes: []ProjectLaneGates{
			{FailingGates: []string{"no lane in this project is routed to a parent task yet"}},
		}}, nil
	}

	project, err := store.GetProject(ctx, in.ProjectId)
	if err != nil {
		return RequestProjectApprovalOutput{}, fmt.Errorf("mcpserver: load project %s: %w", in.ProjectId, err)
	}
	repoSlug := ""
	if project.RepositoryUrl != "" {
		if slug, ok := gitops.RepoSlug(project.RepositoryUrl); ok {
			repoSlug = slug
		}
	}
	var githubGate *adapters.Gate
	if repoSlug != "" {
		if gate, gerr := a.AdapterRegistry.Gate(ctx, "github"); gerr == nil {
			githubGate = gate
		}
	}
	checks := delivery.RunPreflight(ctx, profile, a.Inspector, githubGate, repoSlug)

	requestedBy := in.RequestedBy
	if requestedBy == "" {
		requestedBy = "semar"
	}
	subtasks, err := gatherJiraSubtasksForProject(ctx, req, a.AdapterRegistry, requestedBy, store, in.OrchestrationId, parentTaskIDs)
	if err != nil {
		return RequestProjectApprovalOutput{}, err
	}
	totalHours := projectVerifiedHours(ctx, a, laneIDs)
	allocation := worklogalloc.Allocate(totalHours, subtasks)

	var plannedBranches []string
	for _, lane := range projectLanes {
		if lane.Branch != nil && *lane.Branch != "" {
			plannedBranches = append(plannedBranches, *lane.Branch)
		}
	}

	plan := delivery.ManifestPlan{
		PlannedBaseRef:    profile.BaseBranch,
		PlannedBranches:   plannedBranches,
		ExpectsCommits:    true,
		ExpectsPushes:     true,
		ExpectsPRs:        true,
		ExpectsJiraWrites: len(subtasks) > 0,
		Checks:            checks,
		ProposedWorklog:   allocation,
	}

	key := approvalManifestKey(in.OrchestrationId, in.ProjectId, parentTaskIDs)
	manifest, err := store.CreateApprovalManifest(ctx, key, key, in.OrchestrationId, in.ProjectId, parentTaskIDs, plan)
	if err != nil {
		return RequestProjectApprovalOutput{}, fmt.Errorf("mcpserver: create approval manifest: %w", err)
	}
	return RequestProjectApprovalOutput{Ready: true, Manifest: manifest}, nil
}

// approvalManifestKey derives a manifest id/idempotency key deterministically
// from the exact scope it covers, so re-calling request_project_approval with
// the same orchestration/project and the same set of ready parent tasks
// reuses the same manifest id and the same idempotency key -
// CreateApprovalManifest's own idempotency-key dedup (a repeat key short-
// circuits the write and re-reads whatever the first call persisted under
// the same id) then makes the retry a no-op by construction, without this
// tool needing a duplicate-detection layer of its own. A different set of
// ready parent tasks (lanes finishing or being added between calls) is a
// different scope and deliberately gets a different manifest.
func approvalManifestKey(orchestrationID, projectID string, parentTaskIDs []string) string {
	sorted := append([]string(nil), parentTaskIDs...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(orchestrationID + "|" + projectID + "|" + strings.Join(sorted, ",")))
	return "approval-manifest:" + hex.EncodeToString(sum[:])
}

// projectVerifiedHours sums buildDeliverySummary's VerifiedHours across
// laneIDs. Delivery lanes have no wiring into the run-scoped evidence
// ledger buildDeliverySummary actually reads from (that ledger predates
// internal/delivery and is keyed by an arbitrary caller-chosen run id, not
// a lane id) - using each lane's own id as its run id is the closest
// honest mapping available today: a lane with no matching run-scoped
// evidence simply contributes zero hours, which is a legitimate empty
// state (per deliverysummary.Summary.HasContent), not an error.
func projectVerifiedHours(ctx context.Context, a *app.App, laneIDs []string) float64 {
	var total float64
	for _, laneID := range laneIDs {
		total += buildDeliverySummary(ctx, a, laneID, "", "", "", "", "").VerifiedHours()
	}
	return total
}

// gatherJiraSubtasksForProject collects every distinct Jira subtask
// configured against taskIDs' own Jira-provider requirement sources, for
// worklogalloc.Allocate to map proposed hours onto. It resolves the
// atlassian Gate (which starts a real adapter process on first use) only
// once it already knows at least one Jira-sourced requirement exists to
// look subtasks up for - a project with only freetext/url/github-sourced
// tasks never needs an atlassian adapter at all, so it never pays to spawn
// one. Whether that turns out to be because no adapter is configured, or
// because the project simply has no Jira-sourced requirement, the result
// is the same empty (nil, nil) "no subtasks" outcome either way.
func gatherJiraSubtasksForProject(ctx context.Context, req *mcp.CallToolRequest, registry adapterGateProvider, requestedBy string, store *delivery.Store, orchestrationID string, taskIDs []string) ([]worklogalloc.Subtask, error) {
	seenIssueKeys := map[string]bool{}
	var issueKeys []string
	for _, taskID := range taskIDs {
		task, err := store.GetParentTask(ctx, orchestrationID, taskID)
		if err != nil {
			return nil, fmt.Errorf("mcpserver: load parent task %s: %w", taskID, err)
		}
		for _, sourceID := range task.SourceIds {
			source, err := store.GetRequirementSource(ctx, orchestrationID, sourceID)
			if err != nil {
				return nil, fmt.Errorf("mcpserver: load requirement source %s: %w", sourceID, err)
			}
			if source.Provider != protocol.RequirementSourceProviderJira || source.ExternalId == nil || *source.ExternalId == "" {
				continue
			}
			issueKey := *source.ExternalId
			if seenIssueKeys[issueKey] {
				continue
			}
			seenIssueKeys[issueKey] = true
			issueKeys = append(issueKeys, issueKey)
		}
	}
	if len(issueKeys) == 0 {
		return nil, nil
	}

	gate, err := registry.Gate(ctx, "atlassian")
	if err != nil {
		return nil, nil
	}

	seenSubtaskKeys := map[string]bool{}
	var subtasks []worklogalloc.Subtask
	for _, issueKey := range issueKeys {
		out, err := listJiraSubtasks(ctx, req, gate, ListJiraSubtasksInput{IssueIdOrKey: issueKey, RequestedBy: requestedBy})
		if err != nil {
			return nil, fmt.Errorf("mcpserver: list jira subtasks for %s: %w", issueKey, err)
		}
		for _, st := range out.Subtasks {
			if seenSubtaskKeys[st.Key] {
				continue
			}
			seenSubtaskKeys[st.Key] = true
			subtasks = append(subtasks, worklogalloc.Subtask{Key: st.Key, Summary: st.Summary})
		}
	}
	return subtasks, nil
}
