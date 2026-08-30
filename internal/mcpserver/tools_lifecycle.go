package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jirahooks"
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

func startDeliverySessionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, StartDeliverySessionInput) (*mcp.CallToolResult, DeliverySessionOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in StartDeliverySessionInput) (*mcp.CallToolResult, DeliverySessionOutput, error) {
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

func finalizeDeliverySessionHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, FinalizeDeliverySessionInput) (*mcp.CallToolResult, DeliveryUsageProjectionOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in FinalizeDeliverySessionInput) (*mcp.CallToolResult, DeliveryUsageProjectionOutput, error) {
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
			req := in.Snapshot.toSnapshotRequest(in.SessionID)
			snapshot = &req
		}
		if _, _, err := tstore.Finalize(ctx, telemetry.FinalizeRequest{SessionID: in.SessionID, StopID: stopID, StopReason: in.StopReason, Snapshot: snapshot}); err != nil {
			return nil, DeliveryUsageProjectionOutput{}, fmt.Errorf("mcpserver: finalize delivery session: %w", err)
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

type AssessJiraDeliveryInput struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id,omitempty"`
	SnapshotID     string `json:"snapshot_id,omitempty"`
	SnapshotTitle  string `json:"snapshot_title,omitempty"`
	SnapshotBody   string `json:"snapshot_body,omitempty"`
	Clarity        string `json:"clarity"`
	Rationale      string `json:"rationale"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type AssessJiraDeliveryOutput struct {
	Assessment delivery.JiraAssessment `json:"assessment"`
	View       delivery.DeliveryView   `json:"view"`
}

func assessJiraDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, AssessJiraDeliveryInput) (*mcp.CallToolResult, AssessJiraDeliveryOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in AssessJiraDeliveryInput) (*mcp.CallToolResult, AssessJiraDeliveryOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		snapshotID := in.SnapshotID
		if snapshotID == "" && (in.SnapshotTitle != "" || in.SnapshotBody != "") {
			snapshot, err := store.CaptureJiraSnapshot(ctx, key+":snapshot", in.ExecutionID, in.SessionID, in.SnapshotTitle, in.SnapshotBody)
			if err != nil {
				return nil, AssessJiraDeliveryOutput{}, fmt.Errorf("mcpserver: capture Jira snapshot: %w", err)
			}
			snapshotID = snapshot.ID
		}
		assessment, err := store.AssessJira(ctx, key, in.ExecutionID, in.SessionID, snapshotID, in.Clarity, in.Rationale)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, fmt.Errorf("mcpserver: assess Jira delivery: %w", err)
		}
		execution, err := store.GetExecutionByCase(ctx, assessment.CaseID)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, AssessJiraDeliveryOutput{}, err
		}
		return nil, AssessJiraDeliveryOutput{Assessment: *assessment, View: *view}, nil
	}
}

type QueueJiraWriteInput struct {
	ExecutionID             string         `json:"execution_id"`
	SessionID               string         `json:"session_id,omitempty"`
	JiraIssueKey            string         `json:"jira_issue_key"`
	Action                  string         `json:"action"`
	RefreshStoryPointsField bool           `json:"refresh_story_points_field,omitempty"`
	Payload                 map[string]any `json:"payload,omitempty"`
	IdempotencyKey          string         `json:"idempotency_key,omitempty"`
}
type QueueJiraWriteOutput struct {
	Intent delivery.JiraWriteIntent `json:"intent"`
	View   delivery.DeliveryView    `json:"view"`
}

func queueJiraWriteHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, QueueJiraWriteInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in QueueJiraWriteInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		if in.Action == "update_story_points" {
			if in.RefreshStoryPointsField || !hasStoryPointsFieldMapping(in.Payload) {
				mapping, err := jirahooks.NewLifecycle(store, a.AdapterRegistry).ResolveStoryPointsField(ctx, in.ExecutionID, key+":story-points-field", in.RefreshStoryPointsField)
				if err != nil {
					return nil, QueueJiraWriteOutput{}, fmt.Errorf("mcpserver: discover Jira story-points field: %w", err)
				}
				payload := make(map[string]any, len(in.Payload)+1)
				for name, value := range in.Payload {
					payload[name] = value
				}
				payload["field_metadata"] = map[string]any{"id": mapping.FieldID, "name": mapping.FieldName}
				in.Payload = payload
			}
		}
		intent, err := store.CreateJiraWriteIntent(ctx, key, in.ExecutionID, in.SessionID, in.JiraIssueKey, in.Action, in.Payload)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, fmt.Errorf("mcpserver: queue Jira write: %w", err)
		}
		execution, err := store.GetExecutionByCase(ctx, intent.CaseID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		return nil, QueueJiraWriteOutput{Intent: *intent, View: *view}, nil
	}
}

func hasStoryPointsFieldMapping(payload map[string]any) bool {
	for _, key := range []string{"story_points_field_id", "storyPointsFieldId"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	metadata, ok := payload["field_metadata"].(map[string]any)
	if !ok {
		return false
	}
	value, ok := metadata["id"].(string)
	return ok && strings.TrimSpace(value) != ""
}

type MapDeliveryWorkItemInput struct {
	ExecutionID         string `json:"execution_id"`
	SessionID           string `json:"session_id,omitempty"`
	ParentTaskID        string `json:"parent_task_id"`
	RequirementSourceID string `json:"requirement_source_id"`
	JiraIssueKey        string `json:"jira_issue_key"`
	IdempotencyKey      string `json:"idempotency_key,omitempty"`
}
type MapDeliveryWorkItemOutput struct {
	Mapping delivery.JiraWorkItemMapping `json:"mapping"`
	View    delivery.DeliveryView        `json:"view"`
}

func mapDeliveryWorkItemHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, MapDeliveryWorkItemInput) (*mcp.CallToolResult, MapDeliveryWorkItemOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in MapDeliveryWorkItemInput) (*mcp.CallToolResult, MapDeliveryWorkItemOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, MapDeliveryWorkItemOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		mapping, err := store.MapWorkItemToJiraTask(ctx, key, in.ExecutionID, in.SessionID, in.ParentTaskID, in.RequirementSourceID, in.JiraIssueKey)
		if err != nil {
			return nil, MapDeliveryWorkItemOutput{}, fmt.Errorf("mcpserver: map delivery work item: %w", err)
		}
		view, err := store.BuildDeliveryView(ctx, mapping.OrchestrationID)
		if err != nil {
			return nil, MapDeliveryWorkItemOutput{}, err
		}
		return nil, MapDeliveryWorkItemOutput{Mapping: *mapping, View: *view}, nil
	}
}

type HydrateJiraDeliveryInput struct {
	ExecutionID    string `json:"execution_id"`
	SessionID      string `json:"session_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type HydrateJiraDeliveryOutput struct {
	Sources []jirahooks.HydratedJiraSource `json:"sources"`
	View    delivery.DeliveryView          `json:"view"`
}

func hydrateJiraDeliveryHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, HydrateJiraDeliveryInput) (*mcp.CallToolResult, HydrateJiraDeliveryOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in HydrateJiraDeliveryInput) (*mcp.CallToolResult, HydrateJiraDeliveryOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		sources, err := jirahooks.NewLifecycle(store, a.AdapterRegistry).Hydrate(ctx, in.ExecutionID, in.SessionID, key)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, fmt.Errorf("mcpserver: hydrate Jira delivery: %w", err)
		}
		execution, err := store.GetExecution(ctx, in.ExecutionID)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, HydrateJiraDeliveryOutput{}, err
		}
		return nil, HydrateJiraDeliveryOutput{Sources: sources, View: *view}, nil
	}
}

type ExecuteJiraWritesInput struct {
	IntentID       string `json:"intent_id,omitempty"`
	ExecutionID    string `json:"execution_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}
type ExecuteJiraWritesOutput struct {
	Intents []delivery.JiraWriteIntent `json:"intents"`
	View    delivery.DeliveryView      `json:"view"`
}

type CancelJiraWriteIntentInput struct {
	IntentID       string `json:"intent_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func cancelJiraWriteIntentHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, CancelJiraWriteIntentInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CancelJiraWriteIntentInput) (*mcp.CallToolResult, QueueJiraWriteOutput, error) {
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		intent, err := store.CancelJiraWriteIntent(ctx, key, in.IntentID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, fmt.Errorf("mcpserver: cancel Jira write intent: %w", err)
		}
		execution, err := store.GetExecution(ctx, intent.ExecutionID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, QueueJiraWriteOutput{}, err
		}
		return nil, QueueJiraWriteOutput{Intent: *intent, View: *view}, nil
	}
}

func executeJiraWritesHandler(a *app.App) func(context.Context, *mcp.CallToolRequest, ExecuteJiraWritesInput) (*mcp.CallToolResult, ExecuteJiraWritesOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ExecuteJiraWritesInput) (*mcp.CallToolResult, ExecuteJiraWritesOutput, error) {
		if (in.IntentID == "") == (in.ExecutionID == "") {
			return nil, ExecuteJiraWritesOutput{}, fmt.Errorf("mcpserver: execute Jira writes requires exactly one of intent_id or execution_id")
		}
		store, err := OpenDeliveryStore(ctx, a)
		if err != nil {
			return nil, ExecuteJiraWritesOutput{}, err
		}
		key := in.IdempotencyKey
		if key == "" {
			key = delivery.NewID()
		}
		lifecycle := jirahooks.NewLifecycle(store, a.AdapterRegistry)
		var intents []delivery.JiraWriteIntent
		var execution *delivery.DeliveryExecution
		if in.IntentID != "" {
			intent, err := lifecycle.Execute(ctx, in.IntentID, key)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, fmt.Errorf("mcpserver: execute Jira write intent: %w", err)
			}
			intents = []delivery.JiraWriteIntent{*intent}
			execution, err = store.GetExecution(ctx, intent.ExecutionID)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, err
			}
		} else {
			intents, err = lifecycle.ExecutePending(ctx, in.ExecutionID, key)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, fmt.Errorf("mcpserver: execute pending Jira writes: %w", err)
			}
			execution, err = store.GetExecution(ctx, in.ExecutionID)
			if err != nil {
				return nil, ExecuteJiraWritesOutput{}, err
			}
		}
		view, err := store.BuildDeliveryView(ctx, execution.OrchestrationID)
		if err != nil {
			return nil, ExecuteJiraWritesOutput{}, err
		}
		return nil, ExecuteJiraWritesOutput{Intents: intents, View: *view}, nil
	}
}
