package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/mcpserver"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/telemetry"
)

// setUpSubagentUsageFixture builds a real workspace (via newSmokeWorkspace),
// an active delivery session rooted at repo-a, and drops the session marker
// startDeliverySessionHandler would have written - the same shape
// recordSubagentUsage/ingestHookEvent look for.
func setUpSubagentUsageFixture(t *testing.T) (worktree string, sessionID string) {
	t.Helper()
	dir := newSmokeWorkspace(t)
	ctx := context.Background()

	a, err := app.Load(dir)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	t.Cleanup(func() { a.Close() })

	store, err := mcpserver.OpenDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("OpenDeliveryStore: %v", err)
	}

	resolved, err := store.ResolveJiraDelivery(ctx, "resolve-"+delivery.NewID(), "PAY-1", delivery.ResolveJiraDeliveryOptions{})
	if err != nil {
		t.Fatalf("ResolveJiraDelivery: %v", err)
	}

	worktree = filepath.Join(dir, "repo-a")
	session, err := store.StartSession(ctx, "session-"+delivery.NewID(), resolved.Execution.ID, "", "semar", "", worktree, "claude-code")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	markerDir := filepath.Join(worktree, mcpserver.SessionMarkerDir)
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	marker := mcpserver.SessionMarker{SessionID: session.ID, ExecutionID: session.ExecutionID, OrchestrationID: session.OrchestrationID}
	data, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("marshal marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, mcpserver.SessionMarkerFile), data, 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	return worktree, session.ID
}

func writeFakeSubagentTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-test.jsonl")
	data := ""
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

// telemetryTotals opens worktree's marker-named delivery and returns its
// telemetry.Store cumulative usage projection.
func telemetryTotals(t *testing.T, worktree string) telemetry.UsageProjection {
	t.Helper()
	a, err := app.Load(worktree)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(worktree, mcpserver.SessionMarkerDir, mcpserver.SessionMarkerFile))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var marker mcpserver.SessionMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("decode marker: %v", err)
	}
	totals, err := telemetry.NewStore(db).TotalsByDelivery(context.Background(), marker.OrchestrationID)
	if err != nil {
		t.Fatalf("TotalsByDelivery: %v", err)
	}
	return totals
}

func TestRecordSubagentUsageWritesTokenAndTimeEntries(t *testing.T) {
	worktree, sessionID := setUpSubagentUsageFixture(t)
	_ = sessionID

	transcript := writeFakeSubagentTranscript(t,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":0,"cache_read_input_tokens":5}}}`,
		`{"type":"assistant","timestamp":"2026-08-27T07:01:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
	)

	payload, err := json.Marshal(subagentStopPayload{AgentID: "agent-1", TranscriptPath: transcript, Cwd: worktree})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var stderr bytes.Buffer
	recordSubagentUsage(context.Background(), bytes.NewReader(payload), &stderr)

	got := telemetryTotals(t, worktree)
	if got.Counters.InputTokens != 11 || got.Counters.OutputTokens != 22 || got.Counters.CacheReadTokens != 5 {
		t.Errorf("counters = %+v, want input=11 output=22 cache_read=5 (stderr: %s)", got.Counters, stderr.String())
	}
	if got.Counters.ElapsedMS != 60000 {
		t.Errorf("elapsed ms = %d, want 60000 (stderr: %s)", got.Counters.ElapsedMS, stderr.String())
	}
}

func TestRecordSubagentUsageAcceptsRootTranscriptUsage(t *testing.T) {
	worktree, _ := setUpSubagentUsageFixture(t)
	transcript := writeFakeSubagentTranscript(t,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
	)
	payload, err := json.Marshal(subagentStopPayload{AgentID: "agent-1", TranscriptPath: transcript, Cwd: worktree})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	recordSubagentUsage(context.Background(), bytes.NewReader(payload), &bytes.Buffer{})
	if got := telemetryTotals(t, worktree).Counters.InputTokens; got != 10 {
		t.Fatalf("input tokens = %v, want 10", got)
	}
}

func TestRecordSubagentUsageNoOpsWithoutAgentID(t *testing.T) {
	worktree, _ := setUpSubagentUsageFixture(t)
	payload, _ := json.Marshal(subagentStopPayload{Cwd: worktree})
	var stderr bytes.Buffer
	recordSubagentUsage(context.Background(), bytes.NewReader(payload), &stderr)
	if got := telemetryTotals(t, worktree).Counters.InputTokens; got != 0 {
		t.Fatalf("expected no usage recorded with an empty agent_id, got input tokens = %v", got)
	}
}

func TestRecordSubagentUsageNoOpsWithoutSessionMarker(t *testing.T) {
	dir := newSmokeWorkspace(t)
	worktree := filepath.Join(dir, "repo-a")
	transcript := writeFakeSubagentTranscript(t,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":20}}}`,
	)
	payload, _ := json.Marshal(subagentStopPayload{AgentID: "agent-1", TranscriptPath: transcript, Cwd: worktree})
	var stderr bytes.Buffer

	// Must not panic or block despite there being no punakawan session
	// tracked at this worktree.
	recordSubagentUsage(context.Background(), bytes.NewReader(payload), &stderr)
}

func TestRecordSubagentUsageNoOpsOnMalformedPayload(t *testing.T) {
	var stderr bytes.Buffer
	recordSubagentUsage(context.Background(), bytes.NewReader([]byte("not json")), &stderr)
}

func spoolFileCount(t *testing.T, dataDir string) int {
	t.Helper()
	dir, err := telemetry.SpoolDir(dataDir)
	if err != nil {
		t.Fatalf("SpoolDir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}

func TestIngestHookEventSessionStartBeginsAndDrainsSpool(t *testing.T) {
	worktree, _ := setUpSubagentUsageFixture(t)
	dataDir, err := storageDataDir()
	if err != nil {
		t.Fatalf("storageDataDir: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{
		"session_id": "codex-thr-1", "cwd": worktree, "hook_event_name": "SessionStart", "model": "gpt-5-codex",
	})
	var stdout, stderr bytes.Buffer
	ingestHookEvent(context.Background(), "codex", "SessionStart", bytes.NewReader(payload), &stdout, &stderr)

	a, err := app.Load(worktree)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}
	session, err := telemetry.NewStore(db).GetSessionByExternalID(context.Background(), "codex", "codex-thr-1")
	if err != nil {
		t.Fatalf("GetSessionByExternalID: %v (stderr: %s)", err, stderr.String())
	}
	if session.Status != "active" {
		t.Fatalf("session status = %q, want active", session.Status)
	}
	if got := spoolFileCount(t, dataDir); got != 0 {
		t.Fatalf("spool files remaining = %d, want 0 after successful immediate ingestion", got)
	}
}

func TestIngestHookEventPostToolUseSnapshotsUsage(t *testing.T) {
	worktree, _ := setUpSubagentUsageFixture(t)
	transcript := writeFakeSubagentTranscript(t,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":20},"content":[{"type":"tool_use","id":"toolu_1"}]}}`,
	)
	payload, _ := json.Marshal(map[string]any{
		"session_id": "codex-thr-2", "cwd": worktree, "hook_event_name": "PostToolUse",
		"transcript_path": transcript, "tool_name": "apply_patch", "tool_use_id": "call_1",
	})
	var stdout, stderr bytes.Buffer
	ingestHookEvent(context.Background(), "codex", "PostToolUse", bytes.NewReader(payload), &stdout, &stderr)

	got := telemetryTotals(t, worktree)
	if got.Counters.InputTokens != 10 || got.Counters.OutputTokens != 20 {
		t.Fatalf("counters = %+v, want input=10 output=20 (stderr: %s)", got.Counters, stderr.String())
	}
	if got.Counters.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want 1 (stderr: %s)", got.Counters.ToolCalls, stderr.String())
	}
}

func TestIngestHookEventNoOpsWithoutSessionMarker(t *testing.T) {
	dir := newSmokeWorkspace(t)
	worktree := filepath.Join(dir, "repo-a")
	payload, _ := json.Marshal(map[string]any{"session_id": "codex-thr-3", "cwd": worktree, "hook_event_name": "SessionStart"})
	var stdout, stderr bytes.Buffer
	// Must not panic or block despite there being no punakawan session
	// tracked at this worktree.
	ingestHookEvent(context.Background(), "codex", "SessionStart", bytes.NewReader(payload), &stdout, &stderr)
}

func TestIngestHookEventNoOpsOnMalformedPayload(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ingestHookEvent(context.Background(), "codex", "SessionStart", bytes.NewReader([]byte("not json")), &stdout, &stderr)
}

func TestIngestHookEventRejectsUnsupportedClient(t *testing.T) {
	var stdout, stderr bytes.Buffer
	ingestHookEvent(context.Background(), "not-a-real-client", "SessionStart", bytes.NewReader([]byte(`{}`)), &stdout, &stderr)
	if stderr.Len() == 0 {
		t.Fatal("expected a logged warning for an unsupported --client value")
	}
}

// TestIngestHookEventSessionStartSurfacesExistingPlanViaMarker covers the
// delivery-tracked path: a session marker whose orchestration already has a
// plan_id should surface a plan_get reminder on stdout.
func TestIngestHookEventSessionStartSurfacesExistingPlanViaMarker(t *testing.T) {
	worktree, _ := setUpSubagentUsageFixture(t)

	a, err := app.Load(worktree)
	if err != nil {
		t.Fatalf("app.Load: %v", err)
	}
	defer a.Close()
	db, err := a.OpenStorage(context.Background())
	if err != nil {
		t.Fatalf("OpenStorage: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(worktree, mcpserver.SessionMarkerDir, mcpserver.SessionMarkerFile))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var marker mcpserver.SessionMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatalf("decode marker: %v", err)
	}

	planStore := plan.NewStore(db)
	saved, err := planStore.Save(context.Background(), plan.Plan{ID: plan.NewID(), Objective: "Ship the bounded-concurrency change"})
	if err != nil {
		t.Fatalf("plan Save: %v", err)
	}

	deliveryStore := delivery.NewStore(db)
	orch, err := deliveryStore.GetOrchestration(context.Background(), marker.OrchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	planID, planRevision := saved.ID, saved.Revision
	if _, err := deliveryStore.UpdateOrchestrationDetails(context.Background(), "link-plan-"+delivery.NewID(), marker.OrchestrationID, orch.Revision, delivery.OrchestrationDetails{
		PlanID: &planID, PlanRevision: &planRevision,
	}); err != nil {
		t.Fatalf("UpdateOrchestrationDetails: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{"session_id": "codex-thr-plan-marker", "cwd": worktree, "hook_event_name": "SessionStart"})
	var stdout, stderr bytes.Buffer
	ingestHookEvent(context.Background(), "codex", "SessionStart", bytes.NewReader(payload), &stdout, &stderr)

	if !strings.Contains(stdout.String(), saved.ID) {
		t.Fatalf("stdout = %q, want it to mention saved plan id %q", stdout.String(), saved.ID)
	}
	if !strings.Contains(stdout.String(), "additionalContext") {
		t.Fatalf("stdout = %q, want hookSpecificOutput.additionalContext", stdout.String())
	}
}

// TestIngestHookEventSessionStartSurfacesExistingPlanViaGitRemoteFallback
// covers the ad-hoc path: a session with no punakawan session marker at all
// (the common case for an interactive Claude Code session, per the finding
// that motivated this feature) should still get the reminder, resolved via
// the cwd's git origin remote matched against a registered project.
func TestIngestHookEventSessionStartSurfacesExistingPlanViaGitRemoteFallback(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("PUNAKAWAN_DATA_DIR", dataDir)

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-q", "-b", "main")
	runGit(t, repoDir, "remote", "add", "origin", "git@github.com:acme/widgets.git")

	dbPath, err := storage.DBPath()
	if err != nil {
		t.Fatalf("DBPath: %v", err)
	}
	db, err := storage.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	defer db.Close()

	deliveryStore := delivery.NewStore(db)
	proj, err := deliveryStore.UpsertProject(context.Background(), "upsert-"+delivery.NewID(), delivery.NewID(), "widgets", "https://github.com/acme/widgets.git", "main")
	if err != nil {
		t.Fatalf("UpsertProject: %v", err)
	}

	planStore := plan.NewStore(db)
	saved, err := planStore.Save(context.Background(), plan.Plan{ID: plan.NewID(), Objective: "Add bulk export", ProjectIDs: []string{proj.Id}})
	if err != nil {
		t.Fatalf("plan Save: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{"session_id": "codex-thr-plan-remote", "cwd": repoDir, "hook_event_name": "SessionStart"})
	var stdout, stderr bytes.Buffer
	ingestHookEvent(context.Background(), "codex", "SessionStart", bytes.NewReader(payload), &stdout, &stderr)

	if !strings.Contains(stdout.String(), saved.ID) {
		t.Fatalf("stdout = %q, want it to mention saved plan id %q", stdout.String(), saved.ID)
	}
}
