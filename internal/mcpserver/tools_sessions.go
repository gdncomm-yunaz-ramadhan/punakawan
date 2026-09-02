package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/telemetry"
)

type StartDeliverySessionInput struct {
	ExecutionID    string `json:"execution_id"`
	Participant    string `json:"participant"`
	ResumedFromID  string `json:"resumed_from_id,omitempty"`
	WorktreePath   string `json:"worktree_path,omitempty"`
	Provider       string `json:"provider,omitempty"`
	ID             string `json:"id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type DeliverySessionOutput struct {
	Session delivery.DeliverySession `json:"session"`
	View    delivery.DeliveryView    `json:"view"`
}

func startDeliverySessionHandler(a *app.App, reg *toolIndex) func(context.Context, *mcp.CallToolRequest, StartDeliverySessionInput) (*mcp.CallToolResult, DeliverySessionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in StartDeliverySessionInput) (*mcp.CallToolResult, DeliverySessionOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, DeliverySessionOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		session, err := store.StartSession(ctx, key, in.ExecutionID, in.ID, in.Participant, in.ResumedFromID, in.WorktreePath, in.Provider)
		if err != nil {
			return nil, DeliverySessionOutput{}, fmt.Errorf("mcpserver: start delivery session: %w", err)
		}
		writeSessionMarker(session)
		// Bind this MCP connection to in.Participant's role, if it names one
		// of the four known roles, so subsequent tool calls on the same
		// connection are checked against that role's ToolPolicy (see
		// toolindex.go). A participant that isn't a known role, or no reg
		// configured, just means no binding - never an error, since
		// Participant has always been free-form.
		if reg != nil && reg.agents != nil && req != nil {
			if spec, err := reg.agents.Get(in.Participant); err == nil {
				reg.bindRole(req.Session, spec.ID)
			}
		}
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, DeliverySessionOutput{}, err
		}
		return nil, DeliverySessionOutput{Session: *session, View: *view}, nil
	}
}

type CheckpointDeliverySessionInput struct {
	SessionID       string   `json:"session_id"`
	Summary         string   `json:"summary"`
	ProgressPercent *float64 `json:"progress_percent,omitempty"`
	HandoffTo       string   `json:"handoff_to,omitempty"`
	ID              string   `json:"id,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
}
type CheckpointDeliverySessionOutput struct {
	Checkpoint delivery.SessionCheckpoint `json:"checkpoint"`
	View       delivery.DeliveryView      `json:"view"`
}

func checkpointDeliverySessionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CheckpointDeliverySessionInput) (*mcp.CallToolResult, CheckpointDeliverySessionOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CheckpointDeliverySessionInput) (*mcp.CallToolResult, CheckpointDeliverySessionOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		checkpoint, err := store.CheckpointSession(ctx, key, in.SessionID, in.ID, in.Summary, in.ProgressPercent, in.HandoffTo)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, fmt.Errorf("mcpserver: checkpoint delivery session: %w", err)
		}
		session, err := store.GetSession(ctx, in.SessionID)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, CheckpointDeliverySessionOutput{}, err
		}
		return nil, CheckpointDeliverySessionOutput{Checkpoint: *checkpoint, View: *view}, nil
	}
}

// OpenTelemetryStore is the single MCP binding from the public delivery
// telemetry tools to internal/telemetry, mirroring OpenDeliveryStore's
// shape for the additive, cumulative session-usage store.
func OpenTelemetryStore(ctx context.Context, a *app.App) (*telemetry.Store, error) {
	db, err := a.OpenStorage(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcpserver: open storage kernel: %w", err)
	}
	return telemetry.NewStore(db), nil
}

type DeliveryModelUsageInput struct {
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens,omitempty"`
	OutputTokens     int64  `json:"output_tokens,omitempty"`
	CacheWriteTokens int64  `json:"cache_write_tokens,omitempty"`
	CacheReadTokens  int64  `json:"cache_read_tokens,omitempty"`
}

func (in DeliveryModelUsageInput) toModelUsage() telemetry.ModelUsage {
	return telemetry.ModelUsage{Model: in.Model, InputTokens: in.InputTokens, OutputTokens: in.OutputTokens, CacheWriteTokens: in.CacheWriteTokens, CacheReadTokens: in.CacheReadTokens}
}

type IngestDeliveryUsageSnapshotInput struct {
	SessionID        string                    `json:"session_id"`
	SourceID         string                    `json:"source_id,omitempty" jsonschema:"the turn or named subagent this snapshot is for; defaults to main"`
	Sequence         int64                     `json:"sequence" jsonschema:"monotonic - a sequence at or below what is already stored for this session/source is a no-op"`
	InputTokens      int64                     `json:"input_tokens,omitempty"`
	OutputTokens     int64                     `json:"output_tokens,omitempty"`
	CacheWriteTokens int64                     `json:"cache_write_tokens,omitempty"`
	CacheReadTokens  int64                     `json:"cache_read_tokens,omitempty"`
	ToolCalls        int64                     `json:"tool_calls,omitempty"`
	ElapsedMS        int64                     `json:"elapsed_ms,omitempty"`
	ModelUsage       []DeliveryModelUsageInput `json:"model_usage,omitempty" jsonschema:"per-model token usage; omitted models leave cost explicitly unknown, never zero"`
}

func (in IngestDeliveryUsageSnapshotInput) toSnapshotRequest(sessionID string) telemetry.SnapshotRequest {
	sourceID := strings.TrimSpace(in.SourceID)
	if sourceID == "" {
		sourceID = "main"
	}
	models := make([]telemetry.ModelUsage, 0, len(in.ModelUsage))
	for _, m := range in.ModelUsage {
		models = append(models, m.toModelUsage())
	}
	return telemetry.SnapshotRequest{
		SessionID: sessionID, SourceID: sourceID, Sequence: in.Sequence,
		InputTokens: in.InputTokens, OutputTokens: in.OutputTokens, CacheWriteTokens: in.CacheWriteTokens, CacheReadTokens: in.CacheReadTokens,
		ToolCalls: in.ToolCalls, ElapsedMS: in.ElapsedMS, ModelUsage: models,
	}
}

type DeliveryUsageProjectionOutput struct {
	Projection telemetry.UsageProjection `json:"projection"`
	View       delivery.DeliveryView     `json:"view"`
}

func ingestDeliveryUsageSnapshotHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, IngestDeliveryUsageSnapshotInput) (*mcp.CallToolResult, DeliveryUsageProjectionOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in IngestDeliveryUsageSnapshotInput) (*mcp.CallToolResult, DeliveryUsageProjectionOutput, error) {
		tstore, err := OpenTelemetryStore(ctx, a)
		if err != nil {
			return nil, DeliveryUsageProjectionOutput{}, err
		}
		if _, err := tstore.IngestSnapshot(ctx, in.toSnapshotRequest(in.SessionID)); err != nil {
			return nil, DeliveryUsageProjectionOutput{}, fmt.Errorf("mcpserver: ingest delivery usage snapshot: %w", err)
		}
		return telemetrySessionResult(ctx, a, tstore, in.SessionID)
	}
}

type FinalizeDeliverySessionInput struct {
	SessionID  string                            `json:"session_id"`
	StopID     string                            `json:"stop_id,omitempty" jsonschema:"defaults to a fresh id; supply your own to make a retried finalize call idempotent"`
	StopReason string                            `json:"stop_reason,omitempty"`
	Snapshot   *IngestDeliveryUsageSnapshotInput `json:"snapshot,omitempty" jsonschema:"applied atomically as this session's final usage snapshot"`
}

func finalizeDeliverySessionHandler(a *app.App, reg *toolIndex) func(context.Context, *mcp.CallToolRequest, FinalizeDeliverySessionInput) (*mcp.CallToolResult, DeliveryUsageProjectionOutput, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in FinalizeDeliverySessionInput) (*mcp.CallToolResult, DeliveryUsageProjectionOutput, error) {
		tstore, err := OpenTelemetryStore(ctx, a)
		if err != nil {
			return nil, DeliveryUsageProjectionOutput{}, err
		}
		stopID := strings.TrimSpace(in.StopID)
		if stopID == "" {
			stopID = delivery.NewID()
		}
		var snapshot *telemetry.SnapshotRequest
		if in.Snapshot != nil {
			snapReq := in.Snapshot.toSnapshotRequest(in.SessionID)
			snapshot = &snapReq
		}
		if _, _, err := tstore.Finalize(ctx, telemetry.FinalizeRequest{SessionID: in.SessionID, StopID: stopID, StopReason: in.StopReason, Snapshot: snapshot}); err != nil {
			return nil, DeliveryUsageProjectionOutput{}, fmt.Errorf("mcpserver: finalize delivery session: %w", err)
		}
		if reg != nil && req != nil {
			reg.unbindRole(req.Session)
		}
		return telemetrySessionResult(ctx, a, tstore, in.SessionID)
	}
}

// telemetrySessionResult reads sessionID's refreshed cumulative
// projection and its owning delivery's view - the shared tail
// ingestDeliveryUsageSnapshotHandler and finalizeDeliverySessionHandler
// both return.
func telemetrySessionResult(ctx context.Context, a *app.App, tstore *telemetry.Store, sessionID string) (*mcp.CallToolResult, DeliveryUsageProjectionOutput, error) {
	session, err := tstore.GetSession(ctx, sessionID)
	if err != nil {
		return nil, DeliveryUsageProjectionOutput{}, fmt.Errorf("mcpserver: %w", err)
	}
	projection, err := tstore.TotalsByDelivery(ctx, session.OrchestrationID)
	if err != nil {
		return nil, DeliveryUsageProjectionOutput{}, fmt.Errorf("mcpserver: %w", err)
	}
	dstore, err := OpenDeliveryStore(ctx, a)
	if err != nil {
		return nil, DeliveryUsageProjectionOutput{}, err
	}
	view, err := dstore.BuildDeliveryView(ctx, session.OrchestrationID)
	if err != nil {
		return nil, DeliveryUsageProjectionOutput{}, err
	}
	return nil, DeliveryUsageProjectionOutput{Projection: projection, View: *view}, nil
}

// ReportDeliveryUsageInput is report_delivery_usage's input, unchanged
// from before telemetry existed so an already-integrated caller keeps
// compiling against the same shape.
type ReportDeliveryUsageInput struct {
	SessionID      string   `json:"session_id"`
	Kind           string   `json:"kind"`
	Category       string   `json:"category"`
	Model          string   `json:"model,omitempty"`
	Quantity       float64  `json:"quantity"`
	Unit           string   `json:"unit"`
	UnitPrice      *float64 `json:"unit_price,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	PriceSource    string   `json:"price_source,omitempty"`
	ID             string   `json:"id,omitempty"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
	CorrectPrice   bool     `json:"correct_price,omitempty" jsonschema:"set only to enrich or clear price metadata for existing id; observed usage fields stay unchanged"`
}
type ReportDeliveryUsageOutput struct {
	Usage delivery.UsageEntry   `json:"usage"`
	View  delivery.DeliveryView `json:"view"`
}

// reportDeliveryUsageHandler is report_delivery_usage's deprecated
// compatibility handler, kept working for one release. CorrectPrice still
// enriches an already-existing pre-telemetry ledger row (delivery_usage_ledger
// stays readable, just no longer written to). The plain reporting path no
// longer appends a new ledger row per call - instead it maps onto one
// deterministic, cumulative telemetry source per (session, category,
// model), so repeated legacy calls accumulate the same way the ledger's
// own additive rows used to, without minting a fresh unpredictable row
// each time. It emits a deprecation notice pointing at
// ingest_delivery_usage_snapshot/finalize_delivery_session.
func reportDeliveryUsageHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReportDeliveryUsageInput) (*mcp.CallToolResult, ReportDeliveryUsageOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReportDeliveryUsageInput) (*mcp.CallToolResult, ReportDeliveryUsageOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ReportDeliveryUsageOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		var usage *delivery.UsageEntry
		if in.CorrectPrice {
			usage, err = store.CorrectUsagePricing(ctx, key, in.SessionID, in.ID, in.UnitPrice, in.Currency, in.PriceSource)
			if err != nil {
				return nil, ReportDeliveryUsageOutput{}, fmt.Errorf("mcpserver: report delivery usage: %w", err)
			}
		} else {
			usage, err = reportDeliveryUsageCompat(ctx, a, store, in)
			if err != nil {
				return nil, ReportDeliveryUsageOutput{}, fmt.Errorf("mcpserver: report delivery usage: %w", err)
			}
		}
		session, err := store.GetSession(ctx, in.SessionID)
		if err != nil {
			return nil, ReportDeliveryUsageOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, ReportDeliveryUsageOutput{}, err
		}
		result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
			Text: "report_delivery_usage is deprecated; use ingest_delivery_usage_snapshot and finalize_delivery_session instead.",
		}}}
		return result, ReportDeliveryUsageOutput{Usage: *usage, View: *view}, nil
	}
}

// legacyReportSourceID deterministically names the one telemetry source a
// given (category, model) legacy report accumulates onto - stable across
// calls, so repeated reports for the same category/model keep landing on
// the same (session, source) row rather than a fresh one each time.
func legacyReportSourceID(category, model string) string {
	id := "legacy-report:" + strings.TrimSpace(category)
	if strings.TrimSpace(model) != "" {
		id += ":" + strings.TrimSpace(model)
	}
	return id
}

// reportDeliveryUsageCompat maps one legacy report_delivery_usage call
// onto telemetry: it resolves (or begins) a "legacy-report" telemetry
// session for in.SessionID's delivery, reads that source's current
// cumulative counters, adds this call's quantity onto whichever counter
// its category names, and ingests the new cumulative total at the next
// sequence. Categories this package does not recognize (anything beyond
// the fixed set cmd/punakawan's own hook previously produced) still
// succeed - the call is accepted and a session established - but do not
// move any counter, since there is no safe generic mapping from an
// arbitrary free-form category onto a token/tool-call/duration dimension.
// Price is intentionally left unknown here rather than approximated - see
// this handler's own doc comment.
func reportDeliveryUsageCompat(ctx context.Context, a *app.App, dstore *delivery.Store, in ReportDeliveryUsageInput) (*delivery.UsageEntry, error) {
	session, err := dstore.GetSession(ctx, in.SessionID)
	if err != nil {
		return nil, err
	}
	tstore, err := OpenTelemetryStore(ctx, a)
	if err != nil {
		return nil, err
	}
	telemetrySession, err := tstore.Begin(ctx, telemetry.BeginRequest{
		DeliveryID: session.OrchestrationID, ExecutionID: session.ExecutionID,
		ClientKind: "legacy-report", ExternalSessionID: session.ID,
	})
	if err != nil {
		return nil, err
	}

	sourceID := legacyReportSourceID(in.Category, in.Model)
	previous, err := tstore.GetSnapshot(ctx, telemetrySession.ID, sourceID)
	if err != nil {
		return nil, err
	}
	var counters telemetry.UsageTotals
	var sequence int64
	if previous != nil {
		counters = previous.Counters
		sequence = previous.Sequence
	}
	delta := int64(in.Quantity)
	switch strings.ToLower(strings.TrimSpace(in.Category)) {
	case "tokens_input":
		counters.InputTokens += delta
	case "tokens_output":
		counters.OutputTokens += delta
	case "tokens_cache_creation":
		counters.CacheWriteTokens += delta
	case "tokens_cache_read":
		counters.CacheReadTokens += delta
	case "tool_calls":
		counters.ToolCalls += delta
	case "wall_clock_time":
		if strings.EqualFold(in.Unit, "seconds") {
			counters.ElapsedMS += int64(in.Quantity * 1000)
		} else {
			counters.ElapsedMS += delta
		}
	}
	sequence++

	req := telemetry.SnapshotRequest{
		SessionID: telemetrySession.ID, SourceID: sourceID, Sequence: sequence,
		InputTokens: counters.InputTokens, OutputTokens: counters.OutputTokens,
		CacheWriteTokens: counters.CacheWriteTokens, CacheReadTokens: counters.CacheReadTokens,
		ToolCalls: counters.ToolCalls, ElapsedMS: counters.ElapsedMS,
	}
	if in.Model != "" {
		req.ModelUsage = []telemetry.ModelUsage{{
			Model: in.Model, InputTokens: counters.InputTokens, OutputTokens: counters.OutputTokens,
			CacheWriteTokens: counters.CacheWriteTokens, CacheReadTokens: counters.CacheReadTokens,
		}}
	}
	if _, err := tstore.IngestSnapshot(ctx, req); err != nil {
		return nil, err
	}

	return &delivery.UsageEntry{
		ID: sourceID, CaseID: session.CaseID, ExecutionID: session.ExecutionID, SessionID: session.ID,
		Kind: in.Kind, Category: in.Category, Model: in.Model, Quantity: in.Quantity, Unit: in.Unit,
		UnitPrice: in.UnitPrice, CostCurrency: in.Currency, PriceSource: in.PriceSource, RecordedAt: time.Now().UTC(),
	}, nil
}

type ReportDeliveryProgressInput struct {
	SessionID       string   `json:"session_id"`
	Summary         string   `json:"summary"`
	ProgressPercent *float64 `json:"progress_percent,omitempty"`
	ID              string   `json:"id,omitempty"`
	IdempotencyKey  string   `json:"idempotency_key,omitempty"`
}
type ReportDeliveryProgressOutput struct {
	Progress delivery.ProgressReport `json:"progress"`
	View     delivery.DeliveryView   `json:"view"`
}

func reportDeliveryProgressHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ReportDeliveryProgressInput) (*mcp.CallToolResult, ReportDeliveryProgressOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReportDeliveryProgressInput) (*mcp.CallToolResult, ReportDeliveryProgressOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		progress, err := store.ReportProgress(ctx, key, in.SessionID, in.ID, in.Summary, in.ProgressPercent)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, fmt.Errorf("mcpserver: report delivery progress: %w", err)
		}
		session, err := store.GetSession(ctx, in.SessionID)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, session.OrchestrationID)
		if err != nil {
			return nil, ReportDeliveryProgressOutput{}, err
		}
		return nil, ReportDeliveryProgressOutput{Progress: *progress, View: *view}, nil
	}
}
