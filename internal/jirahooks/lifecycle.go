package jirahooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
)

// Lifecycle hydrates a delivery's Jira source and retries an already
// recorded worklog's sync. Every actual Jira write it triggers goes through
// the durable outbox (internal/outbox/internal/providerwrite), never a
// direct adapter call.
type Lifecycle struct {
	store    *delivery.Store
	registry gateResolver
	outbox   *outbox.Store
}

// NewLifecycle builds a Lifecycle. outboxStore is where RetryWorkLogSync
// enqueues its jira.worklog intent.
func NewLifecycle(store *delivery.Store, registry gateResolver, outboxStore *outbox.Store) *Lifecycle {
	return &Lifecycle{store: store, registry: registry, outbox: outboxStore}
}

// HydratedJiraSource is one Jira issue (the delivery's parent issue, or
// one of its subtasks) as read fresh from the adapter, before it becomes
// a durable requirement source. ContentHash lets a caller detect whether
// a re-hydrated issue actually changed without comparing every field.
type HydratedJiraSource struct {
	IssueKey    string
	ParentKey   string
	Title       string
	Body        string
	Status      string
	IssueType   string
	ContentHash string
}

// jiraIssueFields is the subset of atlassian.getJiraIssue's normalized
// envelope Hydrate needs, for both the parent issue and every subtask.
type jiraIssueFields struct {
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Status      string `json:"status"`
	IssueType   string `json:"issueType"`
	Parent      *struct {
		Key string `json:"key"`
	} `json:"parent"`
	Subtasks []struct {
		Key     string `json:"key"`
		Summary string `json:"summary"`
	} `json:"subtasks"`
}

func fetchJiraIssueFields(ctx context.Context, gate *adapters.Gate, runID, issueKey string) (jiraIssueFields, error) {
	raw, err := gate.Call(ctx, runID, "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": issueKey})
	if err != nil {
		return jiraIssueFields{}, fmt.Errorf("jirahooks: hydrate Jira issue %s: %w", issueKey, err)
	}
	var result struct {
		Normalized jiraIssueFields `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return jiraIssueFields{}, fmt.Errorf("jirahooks: decode Jira issue %s: %w", issueKey, err)
	}
	if result.Normalized.Key == "" {
		result.Normalized.Key = issueKey
	}
	return result.Normalized, nil
}

func toHydratedSource(fields jiraIssueFields, parentKey string) HydratedJiraSource {
	src := HydratedJiraSource{
		IssueKey: fields.Key, ParentKey: parentKey,
		Title: fields.Summary, Body: fields.Description,
		Status: fields.Status, IssueType: fields.IssueType,
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{src.Title, src.Body, src.Status, src.IssueType}, "\x00")))
	src.ContentHash = "sha256:" + hex.EncodeToString(sum[:])
	return src
}

// Hydrate reads the delivery case's exact Jira issue and every one of its
// subtasks through the configured adapter, capturing each as its own
// durable requirement source (canonical key jira:<TENANT>:<KEY> when the
// case has a tenant, matching every pre-0028 tenant-less case exactly as
// before). It also records the parent's title, body, transition catalog,
// and a rollup of every subtask as one immutable snapshot for callers
// still reading DeliveryView.JiraActivity/lifecycle.jira_snapshots.
// idempotencyKey is owned by the caller so repeated MCP calls update the
// observed snapshot on real content changes without duplicating either
// the snapshot or any requirement source.
//
// Jira's own REST API returns every subtask inline on the parent issue's
// own fields.subtasks - there is no separate cursor/page token to walk
// for subtasks in this adapter - so every subtask currently returned by
// the parent is individually re-fetched here for its own full content;
// there is no further adapter-level pagination to drive.
func (l *Lifecycle) Hydrate(ctx context.Context, executionID, sessionID, idempotencyKey string) ([]HydratedJiraSource, error) {
	execution, err := l.store.GetExecution(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get delivery execution: %w", err)
	}
	lifecycle, err := l.store.GetDeliveryLifecycle(ctx, execution.OrchestrationID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get delivery lifecycle: %w", err)
	}
	gate, err := l.registry.Gate(ctx, jiraAdapterID(lifecycle.Case.SourceTenant))
	if err != nil {
		return nil, fmt.Errorf("jirahooks: open atlassian adapter: %w", err)
	}

	parentFields, err := fetchJiraIssueFields(ctx, gate, lifecycle.Case.ID, lifecycle.Case.JiraIssueKey)
	if err != nil {
		return nil, err
	}
	sources := []HydratedJiraSource{toHydratedSource(parentFields, "")}

	var body strings.Builder
	body.WriteString(parentFields.Description)
	if status := strings.TrimSpace(parentFields.Status); status != "" {
		fmt.Fprintf(&body, "\n\nJira status: %s", status)
	}
	for _, subtask := range parentFields.Subtasks {
		if strings.TrimSpace(subtask.Key) == "" {
			continue
		}
		childFields, err := fetchJiraIssueFields(ctx, gate, lifecycle.Case.ID, subtask.Key)
		if err != nil {
			return nil, err
		}
		sources = append(sources, toHydratedSource(childFields, lifecycle.Case.JiraIssueKey))
		fmt.Fprintf(&body, "\n\n## Subtask %s: %s\n%s", childFields.Key, childFields.Summary, childFields.Description)
	}

	raw, err := gate.Call(ctx, lifecycle.Case.ID, "atlassian.getTransitionsForJiraIssue", map[string]any{"issueIdOrKey": lifecycle.Case.JiraIssueKey})
	if err != nil {
		return nil, fmt.Errorf("jirahooks: hydrate Jira transitions for %s: %w", lifecycle.Case.JiraIssueKey, err)
	}
	transitions, err := adapters.DecodeJiraTransitions(raw)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: decode Jira transitions for %s: %w", lifecycle.Case.JiraIssueKey, err)
	}
	if catalog := formatTransitionCatalog(transitions); catalog != "" {
		fmt.Fprintf(&body, "\n\n%s", catalog)
	}

	for _, src := range sources {
		if _, err := l.store.CaptureRequirement(ctx, idempotencyKey+":source:"+src.IssueKey, execution.OrchestrationID, delivery.SourceInput{
			Provider: "jira", ExternalID: src.IssueKey, ParentKey: src.ParentKey,
			Title: src.Title, Summary: src.Body, Tenant: lifecycle.Case.SourceTenant,
		}); err != nil {
			return nil, fmt.Errorf("jirahooks: capture requirement for %s: %w", src.IssueKey, err)
		}
	}

	if _, err := l.store.CaptureJiraSnapshot(ctx, idempotencyKey, executionID, sessionID, parentFields.Summary, body.String()); err != nil {
		return nil, err
	}
	return sources, nil
}

// RetryWorkLogSync replays one existing unsynced worklog through Jira
// without recording a second ledger row. It enqueues (or reuses, by
// fingerprint) the same jira.worklog intent JiraHook.Handle would have
// enqueued for this entry, then attempts it immediately so the caller sees
// the outcome right away instead of waiting for the background worker
// pool; Jira's returned worklog id marks that same interval synced, so
// duplicate local worklogs cannot be created during recovery.
func (l *Lifecycle) RetryWorkLogSync(ctx context.Context, orchestrationID, worklogID string) (*delivery.WorkLogEntry, error) {
	entry, err := l.store.GetWorkLog(ctx, orchestrationID, worklogID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: get worklog: %w", err)
	}
	if entry.SyncStatus == "synced" {
		return entry, nil
	}
	org, err := l.store.JiraOrgForDelivery(ctx, orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("jirahooks: resolve delivery organisation: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"time_spent_seconds": entry.DurationSeconds,
		"comment":            entry.Summary,
		"worklog_entry_id":   entry.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("jirahooks: encode worklog payload: %w", err)
	}
	resolved, err := providerwrite.ExecuteNow(ctx, l.outbox, l.registry, "jira-worklog-retry", outbox.Intent{
		OrchestrationID: orchestrationID, AdapterID: jiraAdapterID(org), Operation: "atlassian.addWorklog",
		TargetKey: entry.JiraIssueKey, PayloadJSON: string(payload),
		OperationFingerprint: providerwrite.JiraWorklogFingerprint(entry.ID),
	})
	if err != nil {
		return nil, fmt.Errorf("jirahooks: retry worklog %s: %w", entry.ID, err)
	}
	if resolved.Status == outbox.StatusSucceeded && resolved.ExternalID != "" {
		if err := l.store.MarkWorkLogSynced(ctx, orchestrationID, entry.ID, resolved.ExternalID); err != nil {
			return nil, fmt.Errorf("jirahooks: mark retried worklog synced: %w", err)
		}
	}
	return l.store.GetWorkLog(ctx, orchestrationID, entry.ID)
}

func formatTransitionCatalog(transitions []adapters.JiraTransition) string {
	if len(transitions) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("Available transitions:")
	for _, transition := range transitions {
		fmt.Fprintf(&out, "\n- %s (%s) -> %s", transition.Name, transition.ID, transition.ToStatusName)
	}
	return out.String()
}

// jiraAdapterID names the adapter process that speaks for one Jira
// organisation. A delivery whose case names no organisation keeps using
// the bare "atlassian" adapter, which is the host's single configured
// site.
func jiraAdapterID(org string) string {
	return adapters.QualifyAdapterID("atlassian", org)
}
