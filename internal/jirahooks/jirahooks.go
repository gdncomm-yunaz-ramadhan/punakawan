// Package jirahooks implements internal/deliveryhooks.Hook against Jira:
// when a delivery's captured requirements say it is for a specific Jira
// issue and the workspace's jira-workflow.yaml has opted in, JiraHook posts
// a comment (and, if configured, fires a workflow transition) for the
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
// openDeliveryStore).
//
// LogWork (internal/jiraworkflow.Config's log_work toggle) is read but not
// acted on: a deliveryhooks.Event carries no time-spent value, and none of
// the delivery state transitions this package's Handle reacts to produces
// one on its own - inventing a duration (e.g. from wall-clock time between
// lease grant and completion) would misrepresent actual effort as logged
// work, so this is left for whatever future change gives an event an
// explicit duration to act on, rather than approximated here.
package jirahooks

import (
	"context"
	"fmt"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// defaultTransitionTargetStatus is the Jira workflow status JiraHook.Handle
// tries to transition a linked issue to when TransitionOnComplete is
// enabled and a delivery completes. The configuration shape
// (auto_log/comment_events/transition_on_complete/log_work) has no
// separate field naming which status to transition to, so this is a fixed,
// common-convention choice rather than something read from config.
// MatchJiraTransition resolves it tolerantly (case-insensitive, matched
// against either a transition's own name or its target status name), and a
// workspace whose workflow has no "Done" status reachable from wherever
// the issue currently sits simply sees no transition fire - exactly as if
// none had been requested, not an error.
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

// JiraHook implements deliveryhooks.Hook by posting Jira comments and,
// optionally, firing a workflow transition, for a delivery's configured
// events against whichever Jira issue its captured requirements say it is
// for.
type JiraHook struct {
	db       *storage.DB
	store    *delivery.Store
	registry gateResolver
	cfg      *jiraworkflow.Config
}

// NewJiraHook builds a JiraHook. db must be the same storage kernel handle
// store was built from - JiraHook needs it directly (rather than only
// through store) to persist its own dispatch-idempotency marker in a table
// store has no reason to know about. registry resolves the running
// atlassian adapter on demand (an *adapters.Registry satisfies this); cfg
// is the workspace's loaded Jira workflow configuration.
func NewJiraHook(db *storage.DB, store *delivery.Store, registry *adapters.Registry, cfg *jiraworkflow.Config) *JiraHook {
	return &JiraHook{db: db, store: store, registry: registry, cfg: cfg}
}

// Handle implements deliveryhooks.Hook.
func (h *JiraHook) Handle(ctx context.Context, event deliveryhooks.Event) error {
	if h.cfg == nil || !h.cfg.AutoLog {
		return nil // master switch is off: this workspace has not opted in to any automatic Jira update
	}

	issueKey, err := h.resolveIssueKey(ctx, event.DeliveryID)
	if err != nil {
		return fmt.Errorf("jirahooks: resolve linked jira issue for delivery %s: %w", event.DeliveryID, err)
	}
	if issueKey == "" {
		return nil // no Jira-sourced requirement has been captured for this delivery: nothing to update
	}

	fired, err := h.alreadyFired(ctx, event.DeliveryID, event.Type, event.Revision)
	if err != nil {
		return fmt.Errorf("jirahooks: check dispatch marker for %s on %s: %w", event.Type, issueKey, err)
	}
	if fired {
		return nil
	}

	gate, err := h.registry.Gate(ctx, "atlassian")
	if err != nil {
		return fmt.Errorf("jirahooks: open atlassian adapter: %w", err)
	}
	// This runs from inside a delivery.Store method's post-commit dispatch,
	// not from an MCP tool call, so there is no client session available to
	// elicit a human approval decision from - gate.Call is used directly
	// rather than the MCP-facing invokeAdapterOperation wrapper. Posting a
	// comment or firing a transition is manifest-declared
	// approval-required, so until a human has approved adapter writes for
	// this run, the call below fails and Handle's caller logs and swallows
	// the error - the same fail-closed behavior every other
	// approval-gated adapter write in this system has, just without an
	// interactive prompt to resolve it inline; a later dispatch (the next
	// event this delivery raises) gets another chance once approved.
	runID := event.DeliveryID

	var acted bool
	if h.cfg.ShouldComment(string(event.Type)) {
		if _, err := gate.Call(ctx, runID, "atlassian.addJiraComment", map[string]any{
			"issueIdOrKey": issueKey,
			"commentBody":  buildComment(event),
		}); err != nil {
			return fmt.Errorf("jirahooks: post comment for %s on %s: %w", event.Type, issueKey, err)
		}
		acted = true
	}

	if event.Type == deliveryhooks.EventDeliveryCompleted && h.cfg.TransitionOnComplete {
		transitioned, err := h.transition(ctx, gate, runID, issueKey, defaultTransitionTargetStatus)
		if err != nil {
			return fmt.Errorf("jirahooks: transition %s on delivery completion: %w", issueKey, err)
		}
		acted = acted || transitioned
	}

	if !acted {
		return nil // nothing configured for this event type actually happened, so there is nothing to mark as fired
	}
	return h.markFired(ctx, event.DeliveryID, event.Type, event.Revision, issueKey)
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
		if src.Provider == protocol.RequirementSourceProviderJira && src.ExternalId != nil && *src.ExternalId != "" {
			return *src.ExternalId, nil
		}
	}
	return "", nil
}

// transition resolves targetStatusName against issueKey's currently
// available workflow transitions and fires the matching one, sharing its
// matching logic with tools_jiraprogress.go's transitionIssueToStatus via
// internal/adapters.DecodeJiraTransitions/MatchJiraTransition. No available
// transition reaching targetStatusName reports transitioned=false rather
// than an error - the issue may simply not have that status reachable from
// wherever it currently sits, a normal outcome rather than a failure of
// this call.
func (h *JiraHook) transition(ctx context.Context, gate *adapters.Gate, runID, issueKey, targetStatusName string) (transitioned bool, err error) {
	raw, err := gate.Call(ctx, runID, "atlassian.getTransitionsForJiraIssue", map[string]any{"issueIdOrKey": issueKey})
	if err != nil {
		return false, fmt.Errorf("list available transitions: %w", err)
	}
	transitions, err := adapters.DecodeJiraTransitions(raw)
	if err != nil {
		return false, err
	}
	match, _, ok := adapters.MatchJiraTransition(transitions, targetStatusName)
	if !ok {
		return false, nil
	}
	if _, err := gate.Call(ctx, runID, "atlassian.transitionJiraIssue", map[string]any{
		"issueIdOrKey": issueKey,
		"transitionId": match.ID,
	}); err != nil {
		return false, fmt.Errorf("fire transition %q: %w", match.ID, err)
	}
	return true, nil
}
