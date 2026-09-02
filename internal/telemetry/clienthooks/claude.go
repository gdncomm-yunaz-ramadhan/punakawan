package clienthooks

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/telemetry"
)

// ClientKindClaudeCode is the client_kind ParseClaudeEvent's mapped
// requests are always tagged with.
const ClientKindClaudeCode = "claude-code"

// claudeEventPayload is the subset of a Claude Code hook's JSON stdin
// payload this package reads, per
// https://code.claude.com/docs/en/hooks: every event carries session_id,
// transcript_path, cwd, and hook_event_name; a tool event additionally
// carries tool_name/tool_input/tool_use_id; any event fired while running
// inside a subagent additionally carries agent_id/agent_type.
type claudeEventPayload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Model          string `json:"model"`
	AgentID        string `json:"agent_id"`
	AgentType      string `json:"agent_type"`
	ToolName       string `json:"tool_name"`
	ToolUseID      string `json:"tool_use_id"`
	Reason         string `json:"reason"`
}

// ParseClaudeEvent maps one Claude Code lifecycle hook event onto a
// telemetry action:
//
//	SessionStart              -> begin or resume the external session
//	PostToolUse/PostToolUseFailure -> a cumulative usage snapshot for the
//	                              turn (or subagent, when agent_id is set)
//	                              that just made a tool call
//	SubagentStart             -> an initial (zero) snapshot establishing
//	                              source_id=agent_id, so it is visible even
//	                              before the subagent's first real usage
//	SubagentStop              -> the subagent's final cumulative usage
//	                              snapshot, summarized from its own
//	                              transcript_path
//	Stop/StopFailure          -> a cumulative usage snapshot for the main
//	                              turn, summarized from the session's own
//	                              transcript_path
//	SessionEnd                -> finalize the session, applying one last
//	                              cumulative snapshot atomically
//
// Every other recognized-but-untracked event name returns ActionIgnore,
// never an error - an unrecognized event name is the one case this
// returns an error, so a caller mis-invoking `hooks ingest` with a typo'd
// --event value finds out immediately rather than silently no-op'ing.
func ParseClaudeEvent(eventName string, payload []byte) (Mapped, error) {
	var p claudeEventPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Mapped{}, fmt.Errorf("clienthooks: decode claude code %s payload: %w", eventName, err)
	}
	sessionID := strings.TrimSpace(p.SessionID)
	now := time.Now().UTC()

	switch eventName {
	case "SessionStart":
		if sessionID == "" {
			return Mapped{}, fmt.Errorf("clienthooks: claude code %s payload has no session_id", eventName)
		}
		return Mapped{Action: ActionBegin, ExternalSessionID: sessionID, Begin: &telemetry.BeginRequest{
			ClientKind: ClientKindClaudeCode, ExternalSessionID: sessionID,
			Provider: ClientKindClaudeCode, Model: strings.TrimSpace(p.Model), WorktreePath: strings.TrimSpace(p.Cwd),
		}}, nil

	case "PostToolUse", "PostToolUseFailure":
		if strings.TrimSpace(p.TranscriptPath) == "" {
			return ignoredFor(sessionID), nil
		}
		summary, err := summarizeTranscript(p.TranscriptPath)
		if err != nil {
			return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for claude code %s: %w", eventName, err)
		}
		snap := snapshotFromTranscript("", sourceIDForTranscript(sessionID, p.TranscriptPath), summary, now)
		return Mapped{Action: ActionSnapshot, ExternalSessionID: sessionID, Snapshot: &snap}, nil

	case "SubagentStart":
		// This used to write a zero-usage placeholder row keyed by the
		// agent id, so a subagent was visible before its first real usage.
		// With sources keyed by transcript there is nothing to place-hold:
		// the subagent's usage lands on whichever transcript reports it.
		return ignoredFor(sessionID), nil

	case "SubagentStop":
		if strings.TrimSpace(p.TranscriptPath) == "" || strings.TrimSpace(p.AgentID) == "" {
			return ignoredFor(sessionID), nil
		}
		summary, err := summarizeTranscript(p.TranscriptPath)
		if err != nil {
			return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for claude code %s: %w", eventName, err)
		}
		snap := snapshotFromTranscript("", sourceIDForTranscript(sessionID, p.TranscriptPath), summary, now)
		return Mapped{Action: ActionSnapshot, ExternalSessionID: sessionID, Snapshot: &snap}, nil

	case "Stop", "StopFailure":
		if strings.TrimSpace(p.TranscriptPath) == "" {
			return ignoredFor(sessionID), nil
		}
		summary, err := summarizeTranscript(p.TranscriptPath)
		if err != nil {
			return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for claude code %s: %w", eventName, err)
		}
		snap := snapshotFromTranscript("", sourceIDForTranscript(sessionID, p.TranscriptPath), summary, now)
		return Mapped{Action: ActionSnapshot, ExternalSessionID: sessionID, Snapshot: &snap}, nil

	case "SessionEnd":
		if sessionID == "" {
			return ignoredFor(sessionID), nil
		}
		var snapshot *telemetry.SnapshotRequest
		if strings.TrimSpace(p.TranscriptPath) != "" {
			summary, err := summarizeTranscript(p.TranscriptPath)
			if err != nil {
				return Mapped{}, fmt.Errorf("clienthooks: summarize transcript for claude code %s: %w", eventName, err)
			}
			snap := snapshotFromTranscript("", sourceIDForTranscript(sessionID, p.TranscriptPath), summary, now)
			snapshot = &snap
		}
		reason := strings.TrimSpace(p.Reason)
		if reason == "" {
			reason = "session_end"
		}
		return Mapped{Action: ActionFinalize, ExternalSessionID: sessionID, Finalize: &telemetry.FinalizeRequest{
			StopID: stopIDFor(ClientKindClaudeCode, sessionID), StoppedAt: now, StopReason: reason, Snapshot: snapshot,
		}}, nil

	default:
		return Mapped{}, fmt.Errorf("clienthooks: unrecognized claude code event %q", eventName)
	}
}
