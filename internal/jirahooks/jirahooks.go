// Package jirahooks implements internal/deliveryhooks.Hook against Jira:
// when a delivery's captured requirements say it is for a specific Jira
// issue and the workspace's jira-workflow.yaml has opted in, JiraHook
// enqueues a comment (and, if configured, a workflow transition) for the
// delivery events the workspace asked to hear about, instead of relying on
// a connected agent to remember to call a Jira tool itself.
//
// This package deliberately lives outside internal/delivery, importing it
// rather than being imported by it: resolving a delivery's linked Jira
// issue means reading back its captured requirements
// (Store.ListRequirementSources), and internal/delivery itself must stay
// unaware of any concrete Hook implementation so that adding or changing
// an integration never touches it. internal/delivery only depends on
// internal/deliveryhooks' generic Event/Hook types; a caller that wants
// Jira updates wires a *JiraHook into a delivery.Store via
// delivery.WithHooks separately (see internal/mcpserver's
// OpenDeliveryStore).
//
// Handle itself never calls a Jira write directly: every effect it decides
// to make is enqueued as one durable outbox intent
// (internal/providerwrite/internal/outbox) and executed later by the
// daemon's worker pool (or, for a caller that wants the result immediately,
// providerwrite.ExecuteNow). This is what "one intent per effect" buys a
// multi-effect event like EventWorkLogged with TransitionOnComplete also
// enabled: a transition failure retries only the transition, never the
// comment or worklog sync that already succeeded.
//
// LogWork projects an explicit, task-bound delivery worklog to Jira when the
// workspace opts in. It never derives time from a lease or wall clock: only
// deliveryhooks.EventWorkLogged carries a measured duration and exact target.
package jirahooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/internal/storage"
)

// defaultTransitionTargetStatus is the Jira workflow status JiraHook.Handle
// tries to transition a linked issue to when TransitionOnComplete is
// enabled and a delivery completes. The configuration shape
// (auto_log/comment_events/transition_on_complete/log_work) has no
// separate field naming which status to transition to, so this is a fixed,
// common-convention choice rather than something read from config.
// enqueueTransition resolves it tolerantly (case-insensitive, matched
// against either a transition's own name or its target status name), and a
// workspace whose workflow has no "Done" status reachable from wherever
// the issue currently sits simply enqueues nothing - exactly as if none had
// been requested, not an error.
const defaultTransitionTargetStatus = "Done"

// gateResolver is the subset of *adapters.Registry's behavior JiraHook
// depends on - resolving the running atlassian adapter's Gate on demand -
// so a test can substitute a fake that hands back a Gate built over a fake
// caller (as internal/adapters' own tests and
// tools_jiraprogress_test.go do) instead of spawning a real adapter
// subprocess.
type gateResolver interface {
	Gate(ctx context.Context, adapterID string) (*adapters.Gate, error)
}

// JiraHook implements deliveryhooks.Hook by enqueuing Jira comments and,
// optionally, a workflow transition, for a delivery's configured events
// against whichever Jira issue its captured requirements say it is for.
type JiraHook struct {
	db       *storage.DB
	store    *delivery.Store
	registry gateResolver
	outbox   *outbox.Store
	cfg      *jiraworkflow.Config
}

// NewJiraHook builds a JiraHook. db must be the same storage kernel handle
// store was built from - JiraHook needs it directly (rather than only
// through store) to persist its own dispatch-idempotency marker in a table
// store has no reason to know about. registry resolves the running
// atlassian adapter on demand (an *adapters.Registry satisfies this);
// outboxStore is where every effect Handle decides to make is durably
// enqueued; cfg is the workspace's loaded Jira workflow configuration.
func NewJiraHook(db *storage.DB, store *delivery.Store, registry *adapters.Registry, outboxStore *outbox.Store, cfg *jiraworkflow.Config) *JiraHook {
	return &JiraHook{db: db, store: store, registry: registry, outbox: outboxStore, cfg: cfg}
}

// Handle implements deliveryhooks.Hook.
func (h *JiraHook) Handle(ctx context.Context, event deliveryhooks.Event) error {
	if h.cfg == nil || !h.cfg.AutoLog {
		return nil
	}

	issueKey := event.JiraIssueKey
	if issueKey == "" {
		var err error
		issueKey, err = h.resolveIssueKey(ctx, event.DeliveryID)
		if err != nil {
			return fmt.Errorf("jirahooks: resolve linked jira issue for delivery %s: %w", event.DeliveryID, err)
		}
	}
	if issueKey == "" {
		return nil
	}

	fired, err := h.alreadyFired(ctx, event)
	if err != nil {
		return fmt.Errorf("jirahooks: check dispatch marker for %s on %s: %w", event.Type, issueKey, err)
	}
	if fired {
		return nil
	}

	acted := false

	if event.Type == deliveryhooks.EventWorkLogged && h.cfg.LogWork {
		payload, err := json.Marshal(map[string]any{
			"time_spent_seconds": event.DurationSeconds,
			"comment":            event.Summary,
			"worklog_entry_id":   event.EntityID,
		})
		if err != nil {
			return fmt.Errorf("jirahooks: encode worklog payload: %w", err)
		}
		if _, err := h.outbox.Enqueue(ctx, outbox.Intent{
			OrchestrationID: event.DeliveryID, AdapterID: "atlassian", Operation: "atlassian.addWorklog",
			TargetKey: issueKey, PayloadJSON: string(payload),
			OperationFingerprint: providerwrite.JiraWorklogFingerprint(event.EntityID),
		}); err != nil {
			return fmt.Errorf("jirahooks: enqueue worklog sync for %s: %w", issueKey, err)
		}
		acted = true
	}

	if h.cfg.ShouldComment(string(event.Type)) {
		payload, err := json.Marshal(map[string]any{"comment_body": buildComment(event)})
		if err != nil {
			return fmt.Errorf("jirahooks: encode comment payload: %w", err)
		}
		// eventKey folds EntityID (e.g. a lane id) into the fingerprint's
		// event slot when present, so two different lanes' otherwise
		// identically named events on the same delivery/issue (e.g.
		// implementation.started on lane-a vs lane-b) enqueue separate
		// comments instead of colliding onto one fingerprint.
		eventKey := string(event.Type)
		if event.EntityID != "" {
			eventKey += ":" + event.EntityID
		}
		if _, err := h.outbox.Enqueue(ctx, outbox.Intent{
			OrchestrationID: event.DeliveryID, AdapterID: "atlassian", Operation: "atlassian.addJiraComment",
			TargetKey: issueKey, PayloadJSON: string(payload),
			OperationFingerprint: providerwrite.JiraCommentFingerprint(event.DeliveryID, eventKey, issueKey),
		}); err != nil {
			return fmt.Errorf("jirahooks: enqueue comment for %s on %s: %w", event.Type, issueKey, err)
		}
		acted = true
	}

	if event.Type == deliveryhooks.EventDeliveryCompleted && h.cfg.TransitionOnComplete {
		enqueued, err := h.enqueueTransition(ctx, event.DeliveryID, issueKey)
		if err != nil {
			return fmt.Errorf("jirahooks: enqueue transition %s on delivery completion: %w", issueKey, err)
		}
		acted = acted || enqueued
	}

	if !acted {
		return nil
	}
	return h.markFired(ctx, event, issueKey)
}

// resolveIssueKey returns the Jira issue key linked to deliveryID via its
// captured requirements - internal/delivery has no separate "linked Jira
// issue" field, so the first Jira-sourced requirement a delivery has
// captured is the closest existing analog to one - or "" if none has been
// captured.
func (h *JiraHook) resolveIssueKey(ctx context.Context, deliveryID string) (string, error) {
	sources, err := h.store.ListRequirementSources(ctx, deliveryID)
	if err != nil {
		return "", err
	}
	for _, src := range sources {
		if src.Provider == "jira" && src.ExternalId != nil && *src.ExternalId != "" {
			return *src.ExternalId, nil
		}
	}
	return "", nil
}

// enqueueTransition reads issueKey's current status and available
// transitions (both read-only, never gated), and - only if a transition
// reaching defaultTransitionTargetStatus is actually reachable from
// wherever the issue currently sits, and it is not already there - enqueues
// one jira.transition intent capturing the observed from/to status pair.
// No reachable transition, or the issue already being at the target status,
// enqueues nothing and returns enqueued=false: a normal outcome, not an
// error.
func (h *JiraHook) enqueueTransition(ctx context.Context, deliveryID, issueKey string) (enqueued bool, err error) {
	gate, err := h.registry.Gate(ctx, "atlassian")
	if err != nil {
		return false, fmt.Errorf("open atlassian adapter: %w", err)
	}
	fields, err := fetchJiraIssueFields(ctx, gate, deliveryID, issueKey)
	if err != nil {
		return false, err
	}
	if fields.Status != "" && fields.Status == defaultTransitionTargetStatus {
		return false, nil
	}
	raw, err := gate.Call(ctx, deliveryID, "atlassian.getTransitionsForJiraIssue", map[string]any{"issueIdOrKey": issueKey})
	if err != nil {
		return false, fmt.Errorf("list available transitions: %w", err)
	}
	transitions, err := adapters.DecodeJiraTransitions(raw)
	if err != nil {
		return false, err
	}
	if _, _, ok := adapters.MatchJiraTransition(transitions, defaultTransitionTargetStatus); !ok {
		return false, nil
	}
	payload, err := json.Marshal(map[string]any{"target_status": defaultTransitionTargetStatus, "from_status": fields.Status})
	if err != nil {
		return false, fmt.Errorf("encode transition payload: %w", err)
	}
	if _, err := h.outbox.Enqueue(ctx, outbox.Intent{
		OrchestrationID: deliveryID, AdapterID: "atlassian", Operation: "atlassian.transitionJiraIssue",
		TargetKey: issueKey, PayloadJSON: string(payload),
		OperationFingerprint: providerwrite.JiraTransitionFingerprint(deliveryID, issueKey, fields.Status, defaultTransitionTargetStatus),
	}); err != nil {
		return false, fmt.Errorf("enqueue transition: %w", err)
	}
	return true, nil
}
