package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/mcpserver"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/telemetry"
	"github.com/ygrip/punakawan/internal/telemetry/clienthooks"
	"github.com/ygrip/punakawan/internal/transcriptusage"
)

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Codex and Claude Code hook integrations",
	}
	cmd.AddCommand(newHooksRecordUsageCmd())
	cmd.AddCommand(newHooksIngestCmd())
	cmd.AddCommand(newHooksInstallGlobalCmd())
	return cmd
}

// newHooksInstallGlobalCmd installs punakawan's machine-global Codex
// lifecycle hook config (~/.codex/hooks.json). It exists so a
// non-interactive installer (scripts/install.sh, scripts/install.ps1,
// scripts/configure-agent.sh) can configure Codex the same way `punakawan
// setup` already configures Claude Code's per-project hooks, without
// opening the credentialed subshell `setup` also does. Codex hooks are
// user-global rather than per-project, unlike Claude Code's
// .claude/settings.json, so there is no project directory to install
// against here.
func newHooksInstallGlobalCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-global",
		Short: "Install punakawan's machine-global Codex lifecycle hook config",
		RunE: func(cmd *cobra.Command, args []string) error {
			binaryPath, err := resolvePanelServiceBinary()
			if err != nil {
				return fmt.Errorf("hooks install-global: resolve installed binary: %w", err)
			}
			changed, err := ensureCodexHooks(binaryPath)
			if err != nil {
				return fmt.Errorf("hooks install-global: %w", err)
			}
			if changed {
				fmt.Fprintln(cmd.OutOrStdout(), "configured codex lifecycle hooks in ~/.codex/hooks.json")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "codex lifecycle hooks already configured")
			}
			return nil
		},
	}
}

// subagentStopPayload is the subset of a Claude Code SubagentStop hook's
// JSON stdin payload this command reads.
type subagentStopPayload struct {
	AgentID        string `json:"agent_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
}

// newHooksRecordUsageCmd implements record-usage: a deprecated,
// SubagentStop-only alias kept working for any hook config installed by an
// earlier release (see setup_hooks.go's usageTrackingHookCommand history).
// It now records into the same additive telemetry tables `hooks ingest`
// does, rather than the deprecated delivery_usage_ledger, so it no longer
// depends on that table remaining writable. New installs get the full
// `hooks ingest --client claude-code --event SubagentStop` mapping
// instead (see setup_hooks.go); this alias exists only so an
// already-configured settings.json keeps working across the upgrade.
func newHooksRecordUsageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "record-usage",
		Short: "(deprecated - use 'hooks ingest --client claude-code --event SubagentStop') record a finished subagent's usage",
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
		logger.Info("hooks record-usage: hook payload has no agent_id")
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

	db, err := a.OpenStorage(ctx)
	if err != nil {
		logger.Warn("hooks record-usage: open storage kernel", "error", err)
		return
	}
	store := telemetry.NewStore(db)

	// The pre-telemetry punakawan session id is the only stable identity
	// this legacy payload has; reused as the external_session_id, it lets
	// repeated SubagentStop calls for the same subagent across one
	// punakawan session resume the same agent_sessions row rather than
	// minting a new one per call.
	session, err := store.Begin(ctx, telemetry.BeginRequest{
		DeliveryID: marker.OrchestrationID, ExecutionID: marker.ExecutionID,
		ClientKind: "legacy-hook", ExternalSessionID: marker.SessionID, Provider: "claude-code",
	})
	if err != nil {
		logger.Warn("hooks record-usage: begin telemetry session", "error", err)
		return
	}

	models := make([]telemetry.ModelUsage, 0, len(summary.ByModel))
	var input, output, cacheWrite, cacheRead int64
	for _, mu := range summary.ByModel {
		models = append(models, telemetry.ModelUsage{
			Model: mu.Model, InputTokens: mu.InputTokens, OutputTokens: mu.OutputTokens,
			CacheWriteTokens: mu.CacheCreationInputTokens, CacheReadTokens: mu.CacheReadInputTokens,
		})
		input += mu.InputTokens
		output += mu.OutputTokens
		cacheWrite += mu.CacheCreationInputTokens
		cacheRead += mu.CacheReadInputTokens
	}
	sequence := input + output + cacheWrite + cacheRead
	if _, err := store.IngestSnapshot(ctx, telemetry.SnapshotRequest{
		SessionID: session.ID, SourceID: "agent:" + payload.AgentID, Sequence: sequence,
		InputTokens: input, OutputTokens: output, CacheWriteTokens: cacheWrite, CacheReadTokens: cacheRead,
		ElapsedMS: int64(summary.ElapsedSeconds * 1000), ModelUsage: models,
	}); err != nil {
		logger.Warn("hooks record-usage: ingest usage snapshot", "agent_id", payload.AgentID, "error", err)
	}
}

// newHooksIngestCmd implements the general Codex/Claude Code lifecycle
// hook entry point every event this package supports goes through:
// `punakawan hooks ingest --client <kind> --event <event>` reads the raw
// hook JSON on stdin, spools it durably, then attempts immediate
// ingestion. See clienthooks.ParseCodexEvent/ParseClaudeEvent for the
// exact event-to-action mapping.
func newHooksIngestCmd() *cobra.Command {
	var client, event string
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest one Codex or Claude Code lifecycle hook event",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Never fail: see recordSubagentUsage's own doc comment - a
			// tracking hiccup must never block or fail the caller's actual
			// agent workflow.
			ingestHookEvent(cmd.Context(), client, event, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
			return nil
		},
	}
	cmd.Flags().StringVar(&client, "client", "", "client kind: codex or claude-code")
	cmd.Flags().StringVar(&event, "event", "", "lifecycle event name, e.g. SessionStart")
	return cmd
}

// cwdPayload reads only a hook payload's "cwd" field - both supported
// clients name it identically, so this command can locate the session
// marker before it needs to know which client-specific shape the rest of
// the payload has.
type cwdPayload struct {
	Cwd string `json:"cwd"`
}

func ingestHookEvent(ctx context.Context, client, event string, stdin io.Reader, stdout, stderr io.Writer) {
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	client = strings.TrimSpace(client)
	event = strings.TrimSpace(event)
	if client == "" || event == "" {
		logger.Warn("hooks ingest: --client and --event are required")
		return
	}

	payload, err := io.ReadAll(stdin)
	if err != nil {
		logger.Warn("hooks ingest: read hook payload", "error", err)
		return
	}

	var mapped clienthooks.Mapped
	switch client {
	case clienthooks.ClientKindCodex:
		mapped, err = clienthooks.ParseCodexEvent(event, payload)
	case clienthooks.ClientKindClaudeCode:
		mapped, err = clienthooks.ParseClaudeEvent(event, payload)
	default:
		err = fmt.Errorf("hooks ingest: unsupported --client %q (want %q or %q)", client, clienthooks.ClientKindCodex, clienthooks.ClientKindClaudeCode)
	}
	if err != nil {
		logger.Warn("hooks ingest: map hook event", "client", client, "event", event, "error", err)
		return
	}
	if mapped.Action == clienthooks.ActionIgnore {
		return
	}

	var cwd cwdPayload
	_ = json.Unmarshal(payload, &cwd)
	markerDir, marker, err := findSessionMarker(cwd.Cwd)

	// The plan reminder runs regardless of whether a delivery session
	// marker was found: a marker only exists for a session started via
	// start_delivery_session, but the plan-underutilization problem this
	// exists to fix is most acute in an ad-hoc interactive session, which
	// never has one. See emitPlanReminder's own doc comment for how it
	// resolves a project without a marker.
	if event == "SessionStart" {
		emitPlanReminder(ctx, cwd.Cwd, marker, stdout, logger)
	}

	if err != nil {
		logger.Info("hooks ingest: no punakawan delivery session tracked here", "cwd", cwd.Cwd, "reason", err)
		return
	}

	a, err := app.Load(markerDir)
	if err != nil {
		logger.Warn("hooks ingest: load app", "dir", markerDir, "error", err)
		return
	}
	defer a.Close()
	db, err := a.OpenStorage(ctx)
	if err != nil {
		logger.Warn("hooks ingest: open storage kernel", "error", err)
		return
	}
	store := telemetry.NewStore(db)

	// Snapshot/Finalize name the external session, not yet a concrete
	// telemetry.AgentSession id - resolve (or, if this client's
	// SessionStart hook never fired, lazily create) it now. Begin is
	// idempotent and cheap, so doing this unconditionally before spooling
	// is safe even when a real SessionStart already ran moments ago.
	if mapped.Action == clienthooks.ActionSnapshot || mapped.Action == clienthooks.ActionFinalize {
		session, err := store.Begin(ctx, telemetry.BeginRequest{
			DeliveryID: marker.OrchestrationID, ExecutionID: marker.ExecutionID,
			ClientKind: client, ExternalSessionID: mapped.ExternalSessionID,
		})
		if err != nil {
			logger.Warn("hooks ingest: resolve telemetry session", "error", err)
			return
		}
		if mapped.Action == clienthooks.ActionSnapshot {
			mapped.Snapshot.SessionID = session.ID
		} else {
			mapped.Finalize.SessionID = session.ID
		}
	}

	dataDir, err := storageDataDir()
	if err != nil {
		logger.Warn("hooks ingest: resolve data dir", "error", err)
		return
	}
	rec := telemetry.SpoolRecord{
		EventID: telemetry.NewEventID(), ClientKind: client, EventName: event,
		Begin: mapped.Begin, Snapshot: mapped.Snapshot, Finalize: mapped.Finalize,
	}
	if mapped.Begin != nil {
		rec.Begin.DeliveryID = marker.OrchestrationID
		rec.Begin.ExecutionID = marker.ExecutionID
	}
	if err := telemetry.WriteSpoolRecord(dataDir, rec); err != nil {
		logger.Warn("hooks ingest: write spool record", "event_id", rec.EventID, "error", err)
		return
	}

	if err := rec.Ingest(ctx, store); err != nil {
		// Left in the spool for the next drain pass - not an error the
		// caller (the actual coding agent's own hook invocation) should
		// ever see.
		logger.Info("hooks ingest: immediate ingestion deferred to next drain", "event_id", rec.EventID, "error", err)
		return
	}
	spoolDir, err := telemetry.SpoolDir(dataDir)
	if err != nil {
		logger.Warn("hooks ingest: resolve spool dir for cleanup", "error", err)
		return
	}
	if err := telemetry.RemoveSpoolFile(filepath.Join(spoolDir, rec.EventID+".json")); err != nil {
		logger.Warn("hooks ingest: remove spooled file after successful ingestion", "event_id", rec.EventID, "error", err)
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

// storageDataDir resolves ${PUNAKAWAN_DATA_DIR} (or the platform default),
// the machine-global directory the telemetry spool lives under - distinct
// from markerDir's project-scoped workspace app.Load opens.
func storageDataDir() (string, error) {
	return storage.DataDir()
}

// emitPlanReminder writes a SessionStart hookSpecificOutput.additionalContext
// JSON payload to stdout surfacing an existing saved plan for this project
// (nudging plan_get) or, when none exists, a reminder to call plan_save once
// one is produced. It resolves the project two ways: when marker is non-nil
// (a delivery session tracked via start_delivery_session), directly from the
// orchestration's own plan_id/project_ids; otherwise by matching cwd's git
// "origin" remote against a registered project's repository_url, so an
// ad-hoc interactive session - which never gets a session marker - still
// gets the reminder. Every failure is silent (logged at Info, not Warn): a
// tracking nudge must never look like an error to the calling agent.
func emitPlanReminder(ctx context.Context, cwd string, marker *mcpserver.SessionMarker, stdout io.Writer, logger *slog.Logger) {
	dbPath, err := storage.DBPath()
	if err != nil {
		logger.Info("hooks ingest: plan reminder: resolve db path", "error", err)
		return
	}
	db, err := storage.Open(ctx, dbPath)
	if err != nil {
		logger.Info("hooks ingest: plan reminder: open storage kernel", "error", err)
		return
	}
	defer db.Close()

	planStore := plan.NewStore(db)
	deliveryStore := delivery.NewStore(db)

	var plans []plan.Plan
	if marker != nil {
		orch, err := deliveryStore.GetOrchestration(ctx, marker.OrchestrationID)
		if err != nil {
			logger.Info("hooks ingest: plan reminder: get orchestration", "error", err)
			return
		}
		if orch.PlanId != nil && *orch.PlanId != "" {
			p, err := planStore.Get(ctx, *orch.PlanId)
			if err == nil {
				plans = []plan.Plan{p}
			}
		}
	} else {
		projectID, err := resolveProjectIDFromGitRemote(ctx, cwd, deliveryStore)
		if err != nil || projectID == "" {
			return
		}
		plans, err = planStore.ListByProject(ctx, projectID)
		if err != nil {
			logger.Info("hooks ingest: plan reminder: list plans by project", "error", err)
			return
		}
	}

	// Silent when nothing is saved yet, for both paths: a delivery-tracked
	// session with no plan_id is unremarkable this early (plan_id is set
	// once Petruk saves one), and an ad-hoc session with no matching plan
	// would just be noise on every unrelated project's session start.
	if len(plans) == 0 {
		return
	}
	p := plans[len(plans)-1]
	objective := p.Objective
	if len(objective) > 160 {
		objective = objective[:160] + "..."
	}
	reminder := fmt.Sprintf(
		"Punakawan: an existing plan is saved for this project (id %q, revision %d): %q. Call plan_get(id=%q) before re-deriving a plan from scratch.",
		p.ID, p.Revision, objective, p.ID,
	)

	encoded, err := json.Marshal(map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "SessionStart",
			"additionalContext": reminder,
		},
	})
	if err != nil {
		logger.Info("hooks ingest: plan reminder: encode additionalContext", "error", err)
		return
	}
	fmt.Fprintln(stdout, string(encoded))
}

// resolveProjectIDFromGitRemote resolves cwd's git "origin" remote URL and
// matches it against every registered project's repository_url, returning
// the first match's id (or "" if there is no origin remote, or no
// registered project matches it).
func resolveProjectIDFromGitRemote(ctx context.Context, cwd string, deliveryStore *delivery.Store) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", nil
	}
	out, err := exec.CommandContext(ctx, "git", "-C", cwd, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", nil //nolint:nilerr // no origin remote (or not a git repo) is a normal, silent no-match, not a failure
	}
	remote := normalizeRepositoryURL(strings.TrimSpace(string(out)))
	if remote == "" {
		return "", nil
	}

	projects, err := deliveryStore.ListProjects(ctx)
	if err != nil {
		return "", err
	}
	for _, proj := range projects {
		if normalizeRepositoryURL(proj.RepositoryUrl) == remote {
			return proj.Id, nil
		}
	}
	return "", nil
}

// normalizeRepositoryURL reduces a git remote URL to a bare, lowercase
// "host/path" form so "git@github.com:acme/widgets.git",
// "https://github.com/acme/widgets.git", and "https://github.com/acme/widgets"
// all compare equal.
func normalizeRepositoryURL(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		s = strings.Replace(rest, ":", "/", 1)
		return s
	}
	for _, prefix := range []string{"https://", "http://", "ssh://git@", "git://"} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			return rest
		}
	}
	return s
}
