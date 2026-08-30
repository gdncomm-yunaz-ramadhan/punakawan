package clienthooks

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/telemetry"
)

// ClientKindCodex is the client_kind ParseCodexEvent's mapped requests are
// always tagged with.
const ClientKindCodex = "codex"

// codexEventPayload is the subset of a Codex CLI hook's JSON stdin
// payload this package reads: every event carries session_id,
// transcript_path, cwd, hook_event_name, and model; a tool event
// additionally carries tool_name/tool_input/tool_use_id; a subagent event
// additionally carries agent_id/agent_type, and SubagentStop specifically
// also carries agent_transcript_path (the subagent's own transcript,
// distinct from the parent session's transcript_path).
type codexEventPayload struct {
	SessionID           string `json:"session_id"`
	TranscriptPath      string `json:"transcript_path"`
	Cwd                 string `json:"cwd"`
	HookEventName       string `json:"hook_event_name"`
	Model               string `json:"model"`
	AgentID             string `json:"agent_id"`
	AgentType           string `json:"agent_type"`
	AgentTranscriptPath string `json:"agent_transcript_path"`
	ToolName            string `json:"tool_name"`
	ToolUseID           string `json:"tool_use_id"`
	Reason              string `json:"reason"`
}

// ParseCodexEvent maps one Codex lifecycle hook event onto a telemetry
// action, mirroring ParseClaudeEvent's mapping for the events Codex and
// Claude Code share (see its doc comment for the shared rationale):
//
//	SessionStart -> begin or resume the external session
//	PostToolUse  -> a cumulative usage snapshot for the turn (or subagent,
//	                when agent_id is set) that just made a tool call
//	SubagentStart/SubagentStop -> an initial, then final, cumulative usage
//	                snapshot for source_id=agent_id (SubagentStop reads
//	                agent_transcript_path, the subagent's own transcript,
//	                rather than the parent session's transcript_path)
//	Stop         -> a cumulative usage snapshot for the main turn
//	SessionEnd   -> finalize the session; Codex documents SessionEnd as
//	                running synchronously (it must complete before the
//	                process exits), so this applies its final snapshot and
//	                returns without any asynchronous follow-up
//
// Every other event this package does not track usage against returns
// ActionIgnore; an unrecognized event name is an error.
func ParseCodexEvent(eventName string, payload []byte) (Mapped, error) {
	var p codexEventPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Mapped{}, fmt.Errorf("clienthooks: decode codex %s payload: %w", eventName, err)
	}
	sessionID := strings.TrimSpace(p.SessionID)
	now := time.Now().UTC()

	switch eventName {
	case "SessionStart":
		if sessionID == "" {
			return Mapped{}, fmt.Errorf("clienthooks: codex %s payload has no session_id", eventName)
		}
		return Mapped{Action: ActionBegin, ExternalSessionID: sessionID, Begin: &telemetry.BeginRequest{
			ClientKind: ClientKindCodex, ExternalSessionID: sessionID,
			Provider: ClientKindCodex, Model: strings.TrimSpace(p.Model), WorktreePath: strings.TrimSpace(p.Cwd),
		}}, nil

	case "PostToolUse":
		if strings.TrimSpace(p.TranscriptPath) == "" {
			return ignoredFor(sessionID), nil
		}
		summary, err := summarizeTranscript(p.TranscriptPath)
		if err != nil {
			return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for codex %s: %w", eventName, err)
		}
		snap := snapshotFromTranscript("", sourceIDFor(p.AgentID), summary, now)
		return Mapped{Action: ActionSnapshot, ExternalSessionID: sessionID, Snapshot: &snap}, nil

	case "SubagentStart":
		if strings.TrimSpace(p.AgentID) == "" {
			return ignoredFor(sessionID), nil
		}
		snap := telemetry.SnapshotRequest{SourceID: sourceIDFor(p.AgentID), Sequence: 0, ObservedAt: now}
		return Mapped{Action: ActionSnapshot, ExternalSessionID: sessionID, Snapshot: &snap}, nil

	case "SubagentStop":
		transcriptPath := strings.TrimSpace(p.AgentTranscriptPath)
		if transcriptPath == "" {
			transcriptPath = strings.TrimSpace(p.TranscriptPath)
		}
		if transcriptPath == "" || strings.TrimSpace(p.AgentID) == "" {
			return ignoredFor(sessionID), nil
		}
		summary, err := summarizeTranscript(transcriptPath)
		if err != nil {
			return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for codex %s: %w", eventName, err)
		}
		snap := snapshotFromTranscript("", sourceIDFor(p.AgentID), summary, now)
		return Mapped{Action: ActionSnapshot, ExternalSessionID: sessionID, Snapshot: &snap}, nil

	case "Stop":
		if strings.TrimSpace(p.TranscriptPath) == "" {
			return ignoredFor(sessionID), nil
		}
		summary, err := summarizeTranscript(p.TranscriptPath)
		if err != nil {
			return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for codex %s: %w", eventName, err)
		}
		snap := snapshotFromTranscript("", mainSourceID, summary, now)
		return Mapped{Action: ActionSnapshot, ExternalSessionID: sessionID, Snapshot: &snap}, nil

	case "SessionEnd":
		if sessionID == "" {
			return ignoredFor(sessionID), nil
		}
		var snapshot *telemetry.SnapshotRequest
		if strings.TrimSpace(p.TranscriptPath) != "" {
			summary, err := summarizeTranscript(p.TranscriptPath)
			if err != nil {
				return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for codex %s: %w", eventName, err)
			}
			snap := snapshotFromTranscript("", mainSourceID, summary, now)
			snapshot = &snap
		}
		reason := strings.TrimSpace(p.Reason)
		if reason == "" {
			reason = "session_end"
		}
		return Mapped{Action: ActionFinalize, ExternalSessionID: sessionID, Finalize: &telemetry.FinalizeRequest{
			StopID: stopIDFor(ClientKindCodex, sessionID), StoppedAt: now, StopReason: reason, Snapshot: snapshot,
		}}, nil

	default:
		return Mapped{}, fmt.Errorf("clienthooks: unrecognized codex event %q", eventName)
	}
}
