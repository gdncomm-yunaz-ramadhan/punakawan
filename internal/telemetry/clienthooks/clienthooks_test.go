package clienthooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture reads testdata/name, substituting {{TRANSCRIPT}} and
// {{AGENT_TRANSCRIPT}} with absolute transcript paths.
//
// The main transcript is materialized under the fixture's own session id,
// because that is how both clients name a session's transcript and how
// sourceIDForTranscript recognizes it as the session's own. The subagent
// transcript keeps its own name, so it reads as the separate file it is.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	body, err := os.ReadFile(filepath.Join("testdata", "transcript_main.v1.jsonl"))
	if err != nil {
		t.Fatalf("read main transcript fixture: %v", err)
	}
	main := filepath.Join(t.TempDir(), payload.SessionID+".jsonl")
	if err := os.WriteFile(main, body, 0o600); err != nil {
		t.Fatalf("write main transcript fixture: %v", err)
	}
	agent, err := filepath.Abs(filepath.Join("testdata", "transcript_subagent.v1.jsonl"))
	if err != nil {
		t.Fatalf("resolve subagent transcript fixture: %v", err)
	}
	text := string(data)
	text = strings.ReplaceAll(text, "{{TRANSCRIPT}}", main)
	text = strings.ReplaceAll(text, "{{AGENT_TRANSCRIPT}}", agent)
	return []byte(text)
}

// subagentSourceID is what a snapshot over the subagent transcript
// fixture must be keyed by.
func subagentSourceID(t *testing.T, sessionID string) string {
	t.Helper()
	agent, err := filepath.Abs(filepath.Join("testdata", "transcript_subagent.v1.jsonl"))
	if err != nil {
		t.Fatalf("resolve subagent transcript fixture: %v", err)
	}
	return sourceIDForTranscript(sessionID, agent)
}

func TestParseClaudeEventSessionStartBegins(t *testing.T) {
	mapped, err := ParseClaudeEvent("SessionStart", loadFixture(t, "claude_session_start.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionBegin || mapped.Begin == nil {
		t.Fatalf("mapped = %+v, want ActionBegin", mapped)
	}
	if mapped.Begin.ClientKind != ClientKindClaudeCode || mapped.Begin.ExternalSessionID != "claude-sess-1" {
		t.Fatalf("begin request = %+v, want claude-code/claude-sess-1", mapped.Begin)
	}
}

func TestParseClaudeEventPostToolUseSnapshotsCumulativeUsage(t *testing.T) {
	mapped, err := ParseClaudeEvent("PostToolUse", loadFixture(t, "claude_post_tool_use.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionSnapshot || mapped.Snapshot == nil {
		t.Fatalf("mapped = %+v, want ActionSnapshot", mapped)
	}
	if mapped.Snapshot.SourceID != mainSourceID {
		t.Fatalf("source id = %q, want %q", mapped.Snapshot.SourceID, mainSourceID)
	}
	// transcript_main.v1.jsonl sums to input=11 output=22 cache_read=5
	// across its two lines, plus two distinct tool_use ids.
	if mapped.Snapshot.InputTokens != 11 || mapped.Snapshot.OutputTokens != 22 || mapped.Snapshot.CacheReadTokens != 5 {
		t.Fatalf("snapshot = %+v, want the fixture transcript's summed totals", mapped.Snapshot)
	}
	if mapped.Snapshot.ToolCalls != 2 {
		t.Fatalf("tool calls = %d, want 2 distinct tool_use ids", mapped.Snapshot.ToolCalls)
	}
}

func TestParseClaudeEventPostToolUseFailureAlsoSnapshots(t *testing.T) {
	mapped, err := ParseClaudeEvent("PostToolUseFailure", loadFixture(t, "claude_post_tool_use_failure.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionSnapshot {
		t.Fatalf("mapped action = %q, want snapshot", mapped.Action)
	}
}

// A subagent start used to write a zero-usage placeholder row keyed by
// the agent id. With sources keyed by transcript there is nothing to
// place-hold, and the row was only ever a source that could not gain
// usage of its own.
func TestParseClaudeEventSubagentStartRecordsNothing(t *testing.T) {
	mapped, err := ParseClaudeEvent("SubagentStart", loadFixture(t, "claude_subagent_start.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionIgnore {
		t.Fatalf("mapped = %+v, want ActionIgnore", mapped)
	}
}

func TestParseClaudeEventSubagentStopSummarizesItsOwnTranscript(t *testing.T) {
	mapped, err := ParseClaudeEvent("SubagentStop", loadFixture(t, "claude_subagent_stop.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionSnapshot || mapped.Snapshot == nil {
		t.Fatalf("mapped = %+v, want ActionSnapshot", mapped)
	}
	if want := subagentSourceID(t, "claude-sess-1"); mapped.Snapshot.SourceID != want {
		t.Fatalf("source id = %q, want the subagent transcript's own key %q", mapped.Snapshot.SourceID, want)
	}
	if mapped.Snapshot.SourceID == mainSourceID {
		t.Fatal("a separate subagent transcript must not land on the session's own source")
	}
	if mapped.Snapshot.InputTokens != 7 || mapped.Snapshot.OutputTokens != 3 {
		t.Fatalf("snapshot = %+v, want the subagent transcript fixture's totals (input=7 output=3)", mapped.Snapshot)
	}
}

// The delivery that prompted this reported 2.3x its real usage: a
// PostToolUse carrying an agent id and a SessionEnd carrying none both
// summarized the same session transcript, and were stored under
// different source ids that the totals then added together.
func TestClaudeEventsOverOneTranscriptShareOneSource(t *testing.T) {
	postToolUse, err := ParseClaudeEvent("PostToolUse", loadFixture(t, "claude_post_tool_use.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent PostToolUse: %v", err)
	}
	stop, err := ParseClaudeEvent("Stop", loadFixture(t, "claude_stop.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent Stop: %v", err)
	}
	if postToolUse.Snapshot.SourceID != stop.Snapshot.SourceID {
		t.Fatalf("source ids %q and %q differ over the same session transcript - their totals would be summed",
			postToolUse.Snapshot.SourceID, stop.Snapshot.SourceID)
	}
	if postToolUse.Snapshot.SourceID != mainSourceID {
		t.Fatalf("source id = %q, want %q for a session's own transcript", postToolUse.Snapshot.SourceID, mainSourceID)
	}
}

func TestSourceIDForTranscriptIsStablePerPathAndDistinctAcrossPaths(t *testing.T) {
	const session = "sess-1"
	a := sourceIDForTranscript(session, "/tmp/agents/agent-a.jsonl")
	if a != sourceIDForTranscript(session, "/tmp/agents/./agent-a.jsonl") {
		t.Fatal("the same transcript reached by two spellings must key one source")
	}
	if a == sourceIDForTranscript(session, "/tmp/agents/agent-b.jsonl") {
		t.Fatal("two transcripts must key two sources")
	}
	if a == mainSourceID {
		t.Fatal("a transcript that is not the session's own must not claim the main source")
	}
	if got := sourceIDForTranscript(session, ""); got != mainSourceID {
		t.Fatalf("source id with no transcript = %q, want %q", got, mainSourceID)
	}
}

func TestParseClaudeEventStopSnapshotsMainTurn(t *testing.T) {
	mapped, err := ParseClaudeEvent("Stop", loadFixture(t, "claude_stop.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionSnapshot || mapped.Snapshot.SourceID != mainSourceID {
		t.Fatalf("mapped = %+v, want a main-source snapshot", mapped)
	}
}

func TestParseClaudeEventStopFailureAlsoSnapshots(t *testing.T) {
	mapped, err := ParseClaudeEvent("StopFailure", loadFixture(t, "claude_stop_failure.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionSnapshot {
		t.Fatalf("mapped action = %q, want snapshot", mapped.Action)
	}
}

func TestParseClaudeEventSessionEndFinalizesWithFinalSnapshot(t *testing.T) {
	mapped, err := ParseClaudeEvent("SessionEnd", loadFixture(t, "claude_session_end.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if mapped.Action != ActionFinalize || mapped.Finalize == nil {
		t.Fatalf("mapped = %+v, want ActionFinalize", mapped)
	}
	if mapped.Finalize.StopReason != "prompt_input_exit" {
		t.Fatalf("stop reason = %q, want the fixture's reason", mapped.Finalize.StopReason)
	}
	if mapped.Finalize.Snapshot == nil {
		t.Fatal("finalize snapshot = nil, want the final cumulative snapshot applied atomically")
	}
	if mapped.Finalize.StopID != stopIDFor(ClientKindClaudeCode, "claude-sess-1") {
		t.Fatalf("stop id = %q, want a deterministic id derived from the session", mapped.Finalize.StopID)
	}
}

func TestParseClaudeEventSessionEndIsIdempotentByConstruction(t *testing.T) {
	first, err := ParseClaudeEvent("SessionEnd", loadFixture(t, "claude_session_end.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	second, err := ParseClaudeEvent("SessionEnd", loadFixture(t, "claude_session_end.v1.json"))
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}
	if first.Finalize.StopID != second.Finalize.StopID {
		t.Fatalf("a replayed SessionEnd produced a different stop id: %q vs %q", first.Finalize.StopID, second.Finalize.StopID)
	}
}

func TestParseClaudeEventUnrecognizedEventErrors(t *testing.T) {
	if _, err := ParseClaudeEvent("NotARealEvent", []byte(`{}`)); err == nil {
		t.Fatal("expected an error for an unrecognized event name")
	}
}

func TestParseCodexEventSessionStartBegins(t *testing.T) {
	mapped, err := ParseCodexEvent("SessionStart", loadFixture(t, "codex_session_start.v1.json"))
	if err != nil {
		t.Fatalf("ParseCodexEvent: %v", err)
	}
	if mapped.Action != ActionBegin || mapped.Begin == nil {
		t.Fatalf("mapped = %+v, want ActionBegin", mapped)
	}
	if mapped.Begin.ClientKind != ClientKindCodex || mapped.Begin.ExternalSessionID != "codex-thr-1" {
		t.Fatalf("begin request = %+v, want codex/codex-thr-1", mapped.Begin)
	}
}

func TestParseCodexEventPostToolUseSnapshotsCumulativeUsage(t *testing.T) {
	mapped, err := ParseCodexEvent("PostToolUse", loadFixture(t, "codex_post_tool_use.v1.json"))
	if err != nil {
		t.Fatalf("ParseCodexEvent: %v", err)
	}
	if mapped.Action != ActionSnapshot || mapped.Snapshot.SourceID != mainSourceID {
		t.Fatalf("mapped = %+v, want a main-source snapshot", mapped)
	}
	if mapped.Snapshot.InputTokens != 11 || mapped.Snapshot.OutputTokens != 22 {
		t.Fatalf("snapshot = %+v, want the fixture transcript's summed totals", mapped.Snapshot)
	}
}

func TestParseCodexEventSubagentStartRecordsNothing(t *testing.T) {
	mapped, err := ParseCodexEvent("SubagentStart", loadFixture(t, "codex_subagent_start.v1.json"))
	if err != nil {
		t.Fatalf("ParseCodexEvent: %v", err)
	}
	if mapped.Action != ActionIgnore {
		t.Fatalf("mapped = %+v, want ActionIgnore", mapped)
	}
}

func TestParseCodexEventSubagentStopUsesAgentTranscriptPath(t *testing.T) {
	mapped, err := ParseCodexEvent("SubagentStop", loadFixture(t, "codex_subagent_stop.v1.json"))
	if err != nil {
		t.Fatalf("ParseCodexEvent: %v", err)
	}
	if want := subagentSourceID(t, "codex-thr-1"); mapped.Action != ActionSnapshot || mapped.Snapshot.SourceID != want {
		t.Fatalf("mapped = %+v, want a snapshot keyed by the agent transcript %q", mapped, want)
	}
	if mapped.Snapshot.InputTokens != 7 || mapped.Snapshot.OutputTokens != 3 {
		t.Fatalf("snapshot = %+v, want the subagent transcript fixture's totals (input=7 output=3), not the parent's", mapped.Snapshot)
	}
}

func TestParseCodexEventStopSnapshotsMainTurn(t *testing.T) {
	mapped, err := ParseCodexEvent("Stop", loadFixture(t, "codex_stop.v1.json"))
	if err != nil {
		t.Fatalf("ParseCodexEvent: %v", err)
	}
	if mapped.Action != ActionSnapshot || mapped.Snapshot.SourceID != mainSourceID {
		t.Fatalf("mapped = %+v, want a main-source snapshot", mapped)
	}
}

func TestParseCodexEventSessionEndFinalizesSynchronously(t *testing.T) {
	mapped, err := ParseCodexEvent("SessionEnd", loadFixture(t, "codex_session_end.v1.json"))
	if err != nil {
		t.Fatalf("ParseCodexEvent: %v", err)
	}
	if mapped.Action != ActionFinalize || mapped.Finalize == nil {
		t.Fatalf("mapped = %+v, want ActionFinalize", mapped)
	}
	if mapped.Finalize.StopReason != "shutdown" {
		t.Fatalf("stop reason = %q, want the fixture's reason", mapped.Finalize.StopReason)
	}
	if mapped.Finalize.Snapshot == nil {
		t.Fatal("finalize snapshot = nil, want the final cumulative snapshot applied atomically")
	}
}

func TestParseCodexEventUnrecognizedEventErrors(t *testing.T) {
	if _, err := ParseCodexEvent("NotARealEvent", []byte(`{}`)); err == nil {
		t.Fatal("expected an error for an unrecognized event name")
	}
}

func TestSnapshotSequenceIsStableAcrossIdenticalReadsAndGrowsWithNewLines(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/transcript.jsonl"
	line1 := `{"type":"assistant","timestamp":"2026-08-29T07:00:00.000Z","message":{"role":"assistant","model":"m","usage":{"input_tokens":10,"output_tokens":5},"content":[{"type":"tool_use","id":"t1"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(line1), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	first, err := summarizeTranscript(path)
	if err != nil {
		t.Fatalf("summarizeTranscript: %v", err)
	}
	second, err := summarizeTranscript(path)
	if err != nil {
		t.Fatalf("summarizeTranscript: %v", err)
	}
	if snapshotSequence(first) != snapshotSequence(second) {
		t.Fatalf("sequence changed across two reads of an unchanged transcript: %d vs %d", snapshotSequence(first), snapshotSequence(second))
	}

	line2 := `{"type":"assistant","timestamp":"2026-08-29T07:01:00.000Z","message":{"role":"assistant","model":"m","usage":{"input_tokens":1,"output_tokens":1},"content":[{"type":"tool_use","id":"t2"}]}}` + "\n"
	if err := appendFile(path, line2); err != nil {
		t.Fatalf("append transcript: %v", err)
	}
	third, err := summarizeTranscript(path)
	if err != nil {
		t.Fatalf("summarizeTranscript: %v", err)
	}
	if snapshotSequence(third) <= snapshotSequence(second) {
		t.Fatalf("sequence did not grow after the transcript grew: %d -> %d", snapshotSequence(second), snapshotSequence(third))
	}
}

func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content)
	return err
}
