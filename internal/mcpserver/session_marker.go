package mcpserver

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ygrip/punakawan/internal/delivery"
)

// SessionMarkerDir is the per-worktree directory a started delivery session
// drops its marker file into, and the directory name the hooks CLI
// (cmd/punakawan hooks record-usage) walks upward from cwd looking for.
const SessionMarkerDir = ".punakawan"

// SessionMarkerFile is SessionMarkerDir's marker file name.
const SessionMarkerFile = "session.json"

// SessionMarker is the marker file's content: just enough to let a process
// with no MCP session of its own (a Claude Code hook) find the punakawan
// delivery session that owns the worktree it's running in.
type SessionMarker struct {
	SessionID       string `json:"session_id"`
	ExecutionID     string `json:"execution_id"`
	OrchestrationID string `json:"orchestration_id"`
}

// writeSessionMarker best-effort drops a SessionMarker at
// <session.WorktreePath>/.punakawan/session.json. It never fails
// startDeliverySessionHandler's caller: a hook that can't find this marker
// simply has nothing to report, which is not the same class of failure as
// the delivery session itself failing to start.
func writeSessionMarker(session *delivery.DeliverySession) {
	if session == nil || strings.TrimSpace(session.WorktreePath) == "" {
		return
	}
	dir := filepath.Join(session.WorktreePath, SessionMarkerDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("mcpserver: create session marker dir", "worktree_path", session.WorktreePath, "error", err)
		return
	}
	marker := SessionMarker{SessionID: session.ID, ExecutionID: session.ExecutionID, OrchestrationID: session.OrchestrationID}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		slog.Warn("mcpserver: encode session marker", "error", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, SessionMarkerFile), data, 0o644); err != nil {
		slog.Warn("mcpserver: write session marker", "worktree_path", session.WorktreePath, "error", err)
	}
}
