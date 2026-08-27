package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/mcpserver"
	"github.com/ygrip/punakawan/internal/transcriptusage"
)

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Claude Code hook integrations",
	}
	cmd.AddCommand(newHooksRecordUsageCmd())
	return cmd
}

// subagentStopPayload is the subset of a Claude Code SubagentStop hook's
// JSON stdin payload this command reads.
type subagentStopPayload struct {
	AgentID        string `json:"agent_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
}

// newHooksRecordUsageCmd implements the SubagentStop end of punakawan's
// hook-based usage tracking: given the hook's JSON payload on stdin, it
// sums the finished subagent's real token usage and wall-clock time out of
// its transcript and records it via delivery.Store.RecordUsage - the same
// path report_delivery_usage uses, just invoked outside any MCP session.
func newHooksRecordUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "record-usage",
		Short: "Record a finished subagent's token usage and elapsed time (SubagentStop hook)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Never fail: a tracking hiccup here must never block or fail
			// the user's actual agent workflow. Every failure path below
			// logs to stderr and returns instead of propagating an error.
			recordSubagentUsage(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr())
			return nil
		},
	}
}

func recordSubagentUsage(ctx context.Context, stdin io.Reader, stderr io.Writer) {
	logger := slog.New(slog.NewTextHandler(stderr, nil))

	var payload subagentStopPayload
	if err := json.NewDecoder(stdin).Decode(&payload); err != nil {
		logger.Warn("hooks record-usage: decode hook payload", "error", err)
		return
	}
	if strings.TrimSpace(payload.AgentID) == "" {
		// Not a subagent completion (main-thread Stop, or a malformed
		// payload) - nothing to report.
		return
	}
	if strings.TrimSpace(payload.TranscriptPath) == "" {
		logger.Warn("hooks record-usage: hook payload has no transcript_path", "agent_id", payload.AgentID)
		return
	}

	markerDir, marker, err := findSessionMarker(payload.Cwd)
	if err != nil {
		logger.Info("hooks record-usage: no punakawan delivery session tracked here", "cwd", payload.Cwd, "reason", err)
		return
	}

	summary, err := transcriptusage.Summarize(payload.TranscriptPath)
	if err != nil {
		logger.Warn("hooks record-usage: summarize transcript", "transcript_path", payload.TranscriptPath, "error", err)
		return
	}

	a, err := app.Load(markerDir)
	if err != nil {
		logger.Warn("hooks record-usage: load app", "dir", markerDir, "error", err)
		return
	}
	defer a.Close()

	store, err := mcpserver.OpenDeliveryStore(ctx, a)
	if err != nil {
		logger.Warn("hooks record-usage: open delivery store", "error", err)
		return
	}

	for _, mu := range summary.ByModel {
		recordUsageEntry(ctx, store, logger, marker.SessionID, "tokens_input", mu.Model, float64(mu.InputTokens))
		recordUsageEntry(ctx, store, logger, marker.SessionID, "tokens_output", mu.Model, float64(mu.OutputTokens))
		recordUsageEntry(ctx, store, logger, marker.SessionID, "tokens_cache_creation", mu.Model, float64(mu.CacheCreationInputTokens))
		recordUsageEntry(ctx, store, logger, marker.SessionID, "tokens_cache_read", mu.Model, float64(mu.CacheReadInputTokens))
	}
	if summary.ElapsedSeconds > 0 {
		recordUsageEntry(ctx, store, logger, marker.SessionID, "wall_clock_time", "", summary.ElapsedSeconds)
	}
}

// recordUsageEntry records one usage ledger entry, logging rather than
// failing the caller if the write is rejected (e.g. the session has since
// closed). unit_price/currency/price_source are deliberately left unset -
// punakawan's own tool instructions forbid maintaining a hardcoded price
// table; cost enrichment from real, current pricing is a separate concern.
func recordUsageEntry(ctx context.Context, store *delivery.Store, logger *slog.Logger, sessionID, category, model string, quantity float64) {
	if quantity <= 0 {
		return
	}
	unit := "tokens"
	if category == "wall_clock_time" {
		unit = "seconds"
	}
	if _, err := store.RecordUsage(ctx, delivery.NewID(), sessionID, "", "actual", category, model, quantity, unit, nil, "", ""); err != nil {
		logger.Warn("hooks record-usage: record usage entry", "category", category, "model", model, "error", err)
	}
}

// findSessionMarker walks upward from startDir (falling back to the
// process's cwd if startDir is empty) looking for
// <dir>/.punakawan/session.json, the marker startDeliverySessionHandler
// drops when a delivery session names a worktree_path. Returns the
// directory the marker was found in and its decoded content.
func findSessionMarker(startDir string) (string, *mcpserver.SessionMarker, error) {
	dir := startDir
	if strings.TrimSpace(dir) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", nil, fmt.Errorf("getwd: %w", err)
		}
		dir = cwd
	}
	for range 10 {
		markerPath := filepath.Join(dir, mcpserver.SessionMarkerDir, mcpserver.SessionMarkerFile)
		if data, err := os.ReadFile(markerPath); err == nil {
			var marker mcpserver.SessionMarker
			if err := json.Unmarshal(data, &marker); err != nil {
				return "", nil, fmt.Errorf("decode session marker %s: %w", markerPath, err)
			}
			return dir, &marker, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", nil, fmt.Errorf("no %s/%s found above %s", mcpserver.SessionMarkerDir, mcpserver.SessionMarkerFile, startDir)
}
