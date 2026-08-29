package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/mcpserver"
)

// setUpSubagentUsageFixture builds a real workspace (via newSmokeWorkspace),
// an active delivery session rooted at repo-a, and drops the session marker
// startDeliverySessionHandler would have written - the same shape
// recordSubagentUsage looks for.
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

func usageEntries(t *testing.T, worktree string) map[string]float64 {
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
	rows, err := db.Reader().Query(`SELECT category, quantity FROM delivery_usage_ledger`)
	if err != nil {
		t.Fatalf("query usage ledger: %v", err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var category string
		var quantity float64
		if err := rows.Scan(&category, &quantity); err != nil {
			t.Fatalf("scan usage row: %v", err)
		}
		out[category] = quantity
	}
	return out
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

	got := usageEntries(t, worktree)
	want := map[string]float64{
		"tokens_input":      11,
		"tokens_output":     22,
		"tokens_cache_read": 5,
		"wall_clock_time":   60,
	}
	for category, quantity := range want {
		if got[category] != quantity {
			t.Errorf("usage[%q] = %v, want %v (all: %+v, stderr: %s)", category, got[category], quantity, got, stderr.String())
		}
	}
	if _, ok := got["tokens_cache_creation"]; ok {
		t.Errorf("expected no tokens_cache_creation entry (zero quantity), got %+v", got)
	}
}

func TestRecordSubagentUsageAcceptsRootTranscriptUsage(t *testing.T) {
	worktree, _ := setUpSubagentUsageFixture(t)
	transcript := writeFakeSubagentTranscript(t,
		`{"type":"assistant","timestamp":"2026-08-27T07:00:00.000Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
	)
	payload, err := json.Marshal(subagentStopPayload{TranscriptPath: transcript, Cwd: worktree})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	recordSubagentUsage(context.Background(), bytes.NewReader(payload), &bytes.Buffer{})
	if got := usageEntries(t, worktree)["tokens_input"]; got != 10 {
		t.Fatalf("tokens_input = %v, want 10", got)
	}
}

func TestRecordSubagentUsageNoOpsWithoutAgentID(t *testing.T) {
	worktree, _ := setUpSubagentUsageFixture(t)
	payload, _ := json.Marshal(subagentStopPayload{Cwd: worktree})
	var stderr bytes.Buffer
	recordSubagentUsage(context.Background(), bytes.NewReader(payload), &stderr)
	if got := usageEntries(t, worktree); len(got) != 0 {
		t.Fatalf("expected no usage entries with an empty agent_id, got %+v", got)
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
