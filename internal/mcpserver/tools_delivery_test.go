package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// seedRunnableLane creates an orchestration, project, task, and lane
// through the delivery package directly (no MCP tool creates these
// yet - that is separate, not-yet-built scope), returning the ids the
// MCP tools under test operate on.
func seedRunnableLane(t *testing.T, a *app.App) (orchestrationID, laneID string) {
	t.Helper()
	ctx := context.Background()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)

	orch, err := store.CreateOrchestration(ctx, "orch-"+delivery.NewID(), delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	proj, err := store.RegisterProject(ctx, "proj-"+delivery.NewID(), delivery.NewID(), "delivery-mcp-test", "https://example.test/delivery-mcp-test.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, "cap-"+delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "jira", ExternalID: "TEST-1", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), orch.Id, "seed task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := store.RouteParentTask(ctx, "route-"+delivery.NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := store.CreateLane(ctx, "lane-"+delivery.NewID(), delivery.NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	return orch.Id, lane.Id
}

// TestDeliveryLeaseToolsEndToEnd drives claim/heartbeat/complete over
// the real MCP wire protocol, covering the pull-based path an external
// agent uses: list what's runnable, claim it, keep the lease alive,
// then report it done.
func TestDeliveryLeaseToolsEndToEnd(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)

	var listOut ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listOut)
	if len(listOut.Lanes) != 1 || listOut.Lanes[0].Id != laneID {
		t.Fatalf("expected exactly the seeded lane runnable, got %+v", listOut.Lanes)
	}
	if listOut.Lanes[0].Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected runnable status, got %s", listOut.Lanes[0].Status)
	}

	var claimOut LaneOutput
	callTool(t, cs, "claim_lane", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": listOut.Lanes[0].Revision,
		"worker_id":         "agent-1",
	}, &claimOut)
	if claimOut.Lane.Status != protocol.DeliveryLaneStatusLeased {
		t.Fatalf("expected leased status after claim, got %s", claimOut.Lane.Status)
	}
	if claimOut.Lane.LeaseToken == nil || *claimOut.Lane.LeaseToken == "" {
		t.Fatal("expected a non-empty lease token")
	}
	leaseToken := *claimOut.Lane.LeaseToken

	var hbOut LaneOutput
	callTool(t, cs, "heartbeat_lease", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       leaseToken,
		"expected_revision": claimOut.Lane.Revision,
	}, &hbOut)
	if hbOut.Lane.Status != protocol.DeliveryLaneStatusRunning {
		t.Fatalf("expected running status after heartbeat, got %s", hbOut.Lane.Status)
	}

	var completeOut LaneOutput
	callTool(t, cs, "complete_lease", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"lease_token":       leaseToken,
		"expected_revision": hbOut.Lane.Revision,
	}, &completeOut)
	if completeOut.Lane.Status != protocol.DeliveryLaneStatusReview {
		t.Fatalf("expected review status after complete, got %s", completeOut.Lane.Status)
	}

	// The lane is no longer runnable, so a second listing must not
	// surface it again.
	var listAfter ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listAfter)
	if len(listAfter.Lanes) != 0 {
		t.Fatalf("expected no runnable lanes left, got %+v", listAfter.Lanes)
	}
}

// TestClaimLaneRejectsStaleRevision covers the concurrent-claim race: a
// second caller working from a stale revision must not also succeed.
func TestClaimLaneRejectsStaleRevision(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLane(t, a)
	cs := connect(t, a)

	var listOut ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listOut)
	staleRevision := listOut.Lanes[0].Revision

	var claimOut LaneOutput
	callTool(t, cs, "claim_lane", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": staleRevision,
		"worker_id":         "agent-1",
	}, &claimOut)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "claim_lane", Arguments: map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": staleRevision,
		"worker_id":         "agent-2",
	}})
	if err != nil {
		t.Fatalf("CallTool(claim_lane): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected a second claim against the same stale (already-leased) revision to fail")
	}
}

// TestReportDiscoveredDependencyBlocksOnlyAffectedLane seeds two
// independent, initially-runnable lanes in the same project, then
// reports (over the real MCP wire protocol) that one task turned out
// to depend on the other, and verifies only the dependent lane moves
// to blocked while the unrelated one stays runnable.
func TestReportDiscoveredDependencyBlocksOnlyAffectedLane(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)

	orch, err := store.CreateOrchestration(ctx, "orch-"+delivery.NewID(), delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	proj, err := store.RegisterProject(ctx, "proj-"+delivery.NewID(), delivery.NewID(), "discovered-dep-test", "https://example.test/discovered-dep-test.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, "cap-"+delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "jira", ExternalID: "TEST-2", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}

	dependent, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), orch.Id, "dependent task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask(dependent): %v", err)
	}
	if _, err := store.RouteParentTask(ctx, "route-"+delivery.NewID(), orch.Id, dependent.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(dependent): %v", err)
	}
	dependentLane, err := store.CreateLane(ctx, "lane-"+delivery.NewID(), delivery.NewID(), orch.Id, proj.Id, dependent.Id)
	if err != nil {
		t.Fatalf("CreateLane(dependent): %v", err)
	}

	blocker, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), orch.Id, "blocker task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask(blocker): %v", err)
	}
	if _, err := store.RouteParentTask(ctx, "route-"+delivery.NewID(), orch.Id, blocker.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(blocker): %v", err)
	}
	blockerLane, err := store.CreateLane(ctx, "lane-"+delivery.NewID(), delivery.NewID(), orch.Id, proj.Id, blocker.Id)
	if err != nil {
		t.Fatalf("CreateLane(blocker): %v", err)
	}

	unrelated, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), orch.Id, "unrelated task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask(unrelated): %v", err)
	}
	if _, err := store.RouteParentTask(ctx, "route-"+delivery.NewID(), orch.Id, unrelated.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask(unrelated): %v", err)
	}
	unrelatedLane, err := store.CreateLane(ctx, "lane-"+delivery.NewID(), delivery.NewID(), orch.Id, proj.Id, unrelated.Id)
	if err != nil {
		t.Fatalf("CreateLane(unrelated): %v", err)
	}

	if _, err := store.SyncFrontier(ctx, "sync-"+delivery.NewID(), orch.Id); err != nil {
		t.Fatalf("SyncFrontier: %v", err)
	}

	cs := connect(t, a)
	var out ReportDiscoveredDependencyOutput
	callTool(t, cs, "report_discovered_dependency", map[string]any{
		"orchestration_id": orch.Id,
		"from_task_id":     dependent.Id,
		"to_task_id":       blocker.Id,
		"evidence":         "worker found dependent task actually needs blocker task's output",
	}, &out)

	if out.Edge.FromTaskId != dependent.Id || out.Edge.ToTaskId != blocker.Id {
		t.Fatalf("unexpected edge recorded: %+v", out.Edge)
	}

	byID := map[string]protocol.DeliveryLane{}
	for _, l := range out.Lanes {
		byID[l.Id] = l
	}
	if lane, ok := byID[dependentLane.Id]; !ok || lane.Status != protocol.DeliveryLaneStatusBlocked {
		t.Fatalf("expected dependent lane %s blocked, got %+v (present=%v)", dependentLane.Id, lane, ok)
	}
	if lane, ok := byID[blockerLane.Id]; !ok || lane.Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected blocker lane %s to stay runnable, got %+v (present=%v)", blockerLane.Id, lane, ok)
	}
	if lane, ok := byID[unrelatedLane.Id]; !ok || lane.Status != protocol.DeliveryLaneStatusRunnable {
		t.Fatalf("expected unrelated lane %s to stay runnable, got %+v (present=%v)", unrelatedLane.Id, lane, ok)
	}
}

// runGitCmd runs `git <args...>` for fixture setup - the code under
// test already exercises the supervised path, so the fixture just
// needs a real repository to point it at.
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s (dir=%q): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// seedRunnableLaneWithGitProject is seedRunnableLane plus a real bare
// remote and local checkout, and a delivery profile pointing at that
// checkout - everything create_worktree needs to actually build a
// worktree for the returned lane.
func seedRunnableLaneWithGitProject(t *testing.T, a *app.App) (orchestrationID, laneID string) {
	t.Helper()
	ctx := context.Background()

	remoteDir := t.TempDir()
	runGitCmd(t, remoteDir, "init", "--bare", "-b", "main")
	localDir := t.TempDir()
	runGitCmd(t, "", "clone", remoteDir, localDir)
	runGitCmd(t, localDir, "config", "user.email", "test@example.com")
	runGitCmd(t, localDir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(localDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	runGitCmd(t, localDir, "add", "README.md")
	runGitCmd(t, localDir, "commit", "-m", "initial commit")
	runGitCmd(t, localDir, "push", "-u", "origin", "main")

	db, err := a.OpenStorage(ctx)
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	store := delivery.NewStore(db)

	orch, err := store.CreateOrchestration(ctx, "orch-"+delivery.NewID(), delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	proj, err := store.RegisterProject(ctx, "proj-"+delivery.NewID(), delivery.NewID(), "run-in-lane-test", remoteDir, "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	if _, err := store.SetDeliveryProfile(ctx, "profile-"+delivery.NewID(), delivery.NewID(), proj.Id, delivery.ProfileInput{
		LocalPath:       localDir,
		CanonicalRemote: "origin",
		BaseBranch:      "main",
	}); err != nil {
		t.Fatalf("SetDeliveryProfile: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, "cap-"+delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "jira", ExternalID: "RUN-1", Title: "seed requirement"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	task, err := store.CreateParentTask(ctx, "task-"+delivery.NewID(), delivery.NewID(), orch.Id, "seed task", []string{source.Id})
	if err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := store.RouteParentTask(ctx, "route-"+delivery.NewID(), orch.Id, task.Id, proj.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	lane, err := store.CreateLane(ctx, "lane-"+delivery.NewID(), delivery.NewID(), orch.Id, proj.Id, task.Id)
	if err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	return orch.Id, lane.Id
}

// TestCreateWorktreeAndRunInLaneEndToEnd drives the full pull-based
// execution path over the real MCP wire: list, claim, create a
// worktree, then run a command scoped to it - proving the command
// actually executed inside the lane's own worktree, not the project's
// main checkout.
func TestCreateWorktreeAndRunInLaneEndToEnd(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLaneWithGitProject(t, a)
	cs := connect(t, a)

	var listOut ListRunnableLanesOutput
	callTool(t, cs, "list_runnable_lanes", map[string]any{"orchestration_id": orchID}, &listOut)
	if len(listOut.Lanes) != 1 {
		t.Fatalf("expected exactly one runnable lane, got %+v", listOut.Lanes)
	}

	var claimOut LaneOutput
	callTool(t, cs, "claim_lane", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": listOut.Lanes[0].Revision,
		"worker_id":         "agent-1",
	}, &claimOut)
	leaseToken := *claimOut.Lane.LeaseToken

	var wtOut LaneOutput
	callTool(t, cs, "create_worktree", map[string]any{
		"orchestration_id":  orchID,
		"lane_id":           laneID,
		"expected_revision": claimOut.Lane.Revision,
	}, &wtOut)
	if wtOut.Lane.WorktreePath == nil || *wtOut.Lane.WorktreePath == "" {
		t.Fatalf("expected worktree_path set, got %+v", wtOut.Lane)
	}

	var runOut RunInLaneOutput
	callTool(t, cs, "run_in_lane", map[string]any{
		"orchestration_id": orchID,
		"lane_id":          laneID,
		"lease_token":      leaseToken,
		"command":          "git",
		"args":             []string{"rev-parse", "--show-toplevel"},
	}, &runOut)
	if runOut.ExitCode != 0 {
		t.Fatalf("run_in_lane exited %d: %s", runOut.ExitCode, runOut.Stderr)
	}

	wantRoot, err := filepath.EvalSymlinks(*wtOut.Lane.WorktreePath)
	if err != nil {
		t.Fatalf("EvalSymlinks(worktree path): %v", err)
	}
	gotRoot, err := filepath.EvalSymlinks(strings.TrimSpace(runOut.Stdout))
	if err != nil {
		t.Fatalf("EvalSymlinks(command output): %v", err)
	}
	if gotRoot != wantRoot {
		t.Fatalf("run_in_lane executed with toplevel %q, want the lane's own worktree %q", gotRoot, wantRoot)
	}

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "run_in_lane", Arguments: map[string]any{
		"orchestration_id": orchID,
		"lane_id":          laneID,
		"lease_token":      "not-the-real-token",
		"command":          "git",
		"args":             []string{"status"},
	}})
	if err != nil {
		t.Fatalf("CallTool(run_in_lane): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected run_in_lane with a wrong lease token to fail")
	}
}

// TestBuildLaneContextEndToEnd checks that build_lane_context returns
// the lane's pinned source, its project's delivery profile, and a
// non-empty digest over the real MCP wire protocol.
func TestBuildLaneContextEndToEnd(t *testing.T) {
	a := newTestApp(t)
	orchID, laneID := seedRunnableLaneWithGitProject(t, a)
	cs := connect(t, a)

	var out BuildLaneContextOutput
	callTool(t, cs, "build_lane_context", map[string]any{
		"orchestration_id": orchID,
		"lane_id":          laneID,
	}, &out)

	if out.Lane.Id != laneID {
		t.Fatalf("expected lane %s, got %s", laneID, out.Lane.Id)
	}
	if len(out.Sources) != 1 {
		t.Fatalf("expected exactly one pinned source, got %+v", out.Sources)
	}
	if out.Profile.ProjectId == "" {
		t.Fatalf("expected a profile in the context, got %+v", out.Profile)
	}
	if out.Digest == "" {
		t.Fatal("expected a non-empty digest")
	}
}
