// Package jiraintegration implements the Jira side of delivery lifecycle
// synchronization as a small set of named, independently callable
// operations - one per lifecycle instant a delivery can reach - instead of
// a single generic event handler. Each enqueues its own outbox intent(s)
// separately: a failure enqueuing a comment never blocks or is blocked by
// a transition, and a transition failure never blocks or is blocked by a
// comment.
//
// Every write Service decides to make is enqueued through the durable
// outbox (internal/outbox/internal/providerwrite) and executed later by
// the worker pool, or immediately via providerwrite.ExecuteNow for a
// caller that wants the result right away - Service itself never calls an
// adapter write directly. Enqueueing is idempotent by construction: the
// outbox's own fingerprint dedup (see internal/providerwrite/fingerprint.go)
// already collapses a redelivered or retried call describing the same
// logical effect onto the same intent, so Service needs no separate
// dispatch-idempotency bookkeeping of its own.
package jiraintegration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/providerwrite"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// GateResolver is the subset of *adapters.Registry's behavior Service
// depends on, so a test can substitute a fake instead of spawning a real
// adapter subprocess.
type GateResolver interface {
	Gate(ctx context.Context, adapterID string) (*adapters.Gate, error)
}

// Service implements Jira lifecycle synchronization for one workspace.
type Service struct {
	store    *delivery.Store
	registry GateResolver
	outbox   *outbox.Store
	cfg      *jiraworkflow.Config
}

// NewService builds a Service. cfg may be nil, in which case every method
// is a no-op - a workspace with no Jira workflow configuration behaves
// exactly as if AutoLog were false.
func NewService(store *delivery.Store, registry GateResolver, outboxStore *outbox.Store, cfg *jiraworkflow.Config) *Service {
	return &Service{store: store, registry: registry, outbox: outboxStore, cfg: cfg}
}

// ErrTransitionNotConfigured is wrapped into the error OnDeliveryStarted or
// OnDeliveryCompleted returns when a project's configured transition
// target status is not reachable from the issue's current status through
// any currently available transition - a workspace configuration problem
// (a typo in start_status/complete_status, or a workflow that changed) an
// operator must fix, not something retrying the same call resolves.
var ErrTransitionNotConfigured = errors.New("jiraintegration: configured transition target status is not reachable")

// ErrTransitionAmbiguous is returned when more than one currently
// available transition on an issue matches the configured target status
// name (by transition name or by target status name) - picking one
// arbitrarily would be a guess, not a resolved decision.
type ErrTransitionAmbiguous struct {
	IssueKey     string
	TargetStatus string
	Options      []protocol.NeedUserInputOptionsElem
}

func (e *ErrTransitionAmbiguous) Error() string {
	return fmt.Sprintf("jiraintegration: %d transitions on %s match target status %q; a human decision is required", len(e.Options), e.IssueKey, e.TargetStatus)
}

// NeedUserInput renders this ambiguity as a protocol.NeedUserInput
// clarification result, for a caller (e.g. an MCP tool handler) that wants
// to surface it directly instead of as a plain error.
func (e *ErrTransitionAmbiguous) NeedUserInput() protocol.NeedUserInput {
	return protocol.NeedUserInput{
		Kind:     protocol.NeedUserInputKindDecisionRequired,
		Question: fmt.Sprintf("Which transition should move %s to %q?", e.IssueKey, e.TargetStatus),
		Options:  e.Options,
	}
}

// OnDeliveryStarted enqueues a "delivery started" comment (if the
// workspace has "delivery.started" configured in comment_events) and, if
// the issue's project has a configured TransitionPolicy.StartStatus,
// enqueues that transition too. Either effect's failure is returned
// without blocking or being blocked by the other.
func (s *Service) OnDeliveryStarted(ctx context.Context, deliveryID string) error {
	if s.cfg == nil || !s.cfg.AutoLog {
		return nil
	}
	issueKey, err := s.resolveIssueKey(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("jiraintegration: resolve linked jira issue for delivery %s: %w", deliveryID, err)
	}
	if issueKey == "" {
		return nil
	}

	if s.cfg.ShouldComment("delivery.started") {
		if err := s.enqueueComment(ctx, deliveryID, issueKey, "delivery started", "delivery.started"); err != nil {
			return fmt.Errorf("jiraintegration: enqueue delivery-started comment for %s: %w", issueKey, err)
		}
	}

	if policy, ok := s.cfg.TransitionPolicyFor(jiraworkflow.ProjectKeyFromIssueKey(issueKey)); ok && policy.StartStatus != "" {
		if err := s.enqueueTransition(ctx, deliveryID, issueKey, policy.StartStatus); err != nil {
			return fmt.Errorf("jiraintegration: enqueue start transition for %s: %w", issueKey, err)
		}
	}
	return nil
}

// OnDeliveryCompleted enqueues a "delivery completed" comment (if
// configured) and, if TransitionOnComplete is enabled, a completion
// transition - to the project's configured TransitionPolicy.CompleteStatus
// if one exists, falling back to "Done" for a project with no configured
// policy, so a workspace that enabled TransitionOnComplete before
// TransitionPolicy existed keeps behaving exactly as it always did.
func (s *Service) OnDeliveryCompleted(ctx context.Context, deliveryID string) error {
	if s.cfg == nil || !s.cfg.AutoLog {
		return nil
	}
	issueKey, err := s.resolveIssueKey(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("jiraintegration: resolve linked jira issue for delivery %s: %w", deliveryID, err)
	}
	if issueKey == "" {
		return nil
	}

	if s.cfg.ShouldComment("delivery.completed") {
		if err := s.enqueueComment(ctx, deliveryID, issueKey, "delivery completed", "delivery.completed"); err != nil {
			return fmt.Errorf("jiraintegration: enqueue delivery-completed comment for %s: %w", issueKey, err)
		}
	}

	if s.cfg.TransitionOnComplete {
		targetStatus := "Done"
		if policy, ok := s.cfg.TransitionPolicyFor(jiraworkflow.ProjectKeyFromIssueKey(issueKey)); ok && policy.CompleteStatus != "" {
			targetStatus = policy.CompleteStatus
		}
		if err := s.enqueueTransition(ctx, deliveryID, issueKey, targetStatus); err != nil {
			return fmt.Errorf("jiraintegration: enqueue completion transition for %s: %w", issueKey, err)
		}
	}
	return nil
}

// OnImplementationCompleted moves a linked issue to the project's
// configured TransitionPolicy.ReviewStatus when a lane's work reaches a
// terminal outcome.
//
// A lane completing used to post a comment and nothing else, so an issue
// whose work was finished and awaiting review still read as in progress.
// It is policy-driven and silent by default: a workspace whose workflow
// has no state between in-progress and done configures no review_status
// and nothing moves.
func (s *Service) OnImplementationCompleted(ctx context.Context, deliveryID string) error {
	if s.cfg == nil || !s.cfg.AutoLog {
		return nil
	}
	issueKey, err := s.resolveIssueKey(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("jiraintegration: resolve linked jira issue for delivery %s: %w", deliveryID, err)
	}
	if issueKey == "" {
		return nil
	}
	policy, ok := s.cfg.TransitionPolicyFor(jiraworkflow.ProjectKeyFromIssueKey(issueKey))
	if !ok || policy.ReviewStatus == "" {
		return nil
	}
	if err := s.enqueueTransition(ctx, deliveryID, issueKey, policy.ReviewStatus); err != nil {
		return fmt.Errorf("jiraintegration: enqueue review transition for %s: %w", issueKey, err)
	}
	return nil
}

// OnRequirementUnclear parks a linked issue in the workspace's
// clarification status when it has one.
//
// ClarificationStatus has been configurable since this package's Jira
// workflow config existed and nothing ever transitioned to it, because
// nothing downstream of a "needs clarification" judgement did anything at
// all. This is what it was for: an issue whose delivery is waiting on an
// answer reads as waiting, rather than as in progress with nobody on it.
// Workspaces that have no such status configure none and nothing moves.
func (s *Service) OnRequirementUnclear(ctx context.Context, deliveryID string) error {
	if s.cfg == nil || !s.cfg.AutoLog || s.cfg.ClarificationStatus == "" {
		return nil
	}
	issueKey, err := s.resolveIssueKey(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("jiraintegration: resolve linked jira issue for delivery %s: %w", deliveryID, err)
	}
	if issueKey == "" {
		return nil
	}
	if err := s.enqueueTransition(ctx, deliveryID, issueKey, s.cfg.ClarificationStatus); err != nil {
		return fmt.Errorf("jiraintegration: enqueue clarification transition for %s: %w", issueKey, err)
	}
	return nil
}

// OnWorkRecorded enqueues one jira.worklog intent for the immutable work
// interval identified by worklogID - the delivery ledger entry's own
// durable id, which is what the enqueued intent's fingerprint keys on, so
// this method is safe to call more than once for the same interval.
func (s *Service) OnWorkRecorded(ctx context.Context, worklogID string) error {
	if s.cfg == nil || !s.cfg.AutoLog || !s.cfg.LogWork {
		return nil
	}
	entry, err := s.store.GetWorkLogByID(ctx, worklogID)
	if err != nil {
		return fmt.Errorf("jiraintegration: get worklog %s: %w", worklogID, err)
	}
	payload, err := json.Marshal(map[string]any{
		"time_spent_seconds": entry.DurationSeconds,
		"comment":            entry.Summary,
		"worklog_entry_id":   entry.ID,
	})
	if err != nil {
		return fmt.Errorf("jiraintegration: encode worklog payload: %w", err)
	}
	adapterID, err := s.adapterIDFor(ctx, entry.OrchestrationID)
	if err != nil {
		return err
	}
	if _, err := s.outbox.Enqueue(ctx, outbox.Intent{
		OrchestrationID: entry.OrchestrationID, AdapterID: adapterID, Operation: "atlassian.addWorklog",
		TargetKey: entry.JiraIssueKey, PayloadJSON: string(payload),
		OperationFingerprint: providerwrite.JiraWorklogFingerprint(entry.ID),
	}); err != nil {
		return fmt.Errorf("jiraintegration: enqueue worklog sync for %s: %w", entry.JiraIssueKey, err)
	}
	return nil
}

// PostComment posts an arbitrary, caller-supplied comment body to issueKey,
// executed synchronously so the caller gets a definitive result in the same
// call.
//
// Unlike OnDeliveryStarted/OnDeliveryCompleted/etc., this is not gated by
// s.cfg.AutoLog: those methods post fixed, template-generated comments at
// lifecycle instants a workspace opted into, while this is an explicit,
// agent-directed action for an arbitrary comment - it must work even for a
// workspace with no Jira workflow configuration (nil cfg) or with AutoLog
// disabled, the same way a human posting a comment by hand doesn't need
// auto-logging turned on.
//
// idempotencyKey should be reused only when retrying this exact intended
// comment (e.g. the caller's own call failed and it is trying again); a
// different comment, even to the same issue, needs its own key - it is
// folded into the outbox fingerprint verbatim, not hashed from the comment
// body, so it carries no notion of "same content" on its own.
func (s *Service) PostComment(ctx context.Context, deliveryID, issueKey, commentBody, idempotencyKey string) (outbox.Intent, error) {
	issueKey = strings.TrimSpace(issueKey)
	if issueKey == "" {
		return outbox.Intent{}, fmt.Errorf("jiraintegration: post comment requires issue_key")
	}
	commentBody = strings.TrimSpace(commentBody)
	if commentBody == "" {
		return outbox.Intent{}, fmt.Errorf("jiraintegration: post comment requires comment_body")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return outbox.Intent{}, fmt.Errorf("jiraintegration: post comment requires idempotency_key")
	}

	adapterID, err := s.adapterIDFor(ctx, deliveryID)
	if err != nil {
		return outbox.Intent{}, err
	}
	payload, err := json.Marshal(map[string]any{"comment_body": commentBody})
	if err != nil {
		return outbox.Intent{}, fmt.Errorf("jiraintegration: encode comment payload: %w", err)
	}
	resolved, err := providerwrite.ExecuteNow(ctx, s.outbox, s.registry, "jira-post-comment-"+idempotencyKey, outbox.Intent{
		OrchestrationID: deliveryID, AdapterID: adapterID, Operation: "atlassian.addJiraComment",
		TargetKey: issueKey, PayloadJSON: string(payload),
		OperationFingerprint: providerwrite.JiraCommentFingerprint(deliveryID, "manual:"+idempotencyKey, issueKey),
	})
	if err != nil {
		return outbox.Intent{}, fmt.Errorf("jiraintegration: post comment to %s: %w", issueKey, err)
	}
	if resolved.Status != outbox.StatusSucceeded {
		reason := resolved.LastErrorRedacted
		if reason == "" {
			reason = fmt.Sprintf("intent ended in status %q", resolved.Status)
		}
		if _, cancelErr := s.outbox.Cancel(ctx, resolved.ID, "jiraintegration: giving up after one synchronous post_comment attempt"); cancelErr != nil {
			return outbox.Intent{}, fmt.Errorf("jiraintegration: cancel unresolved comment intent: %w", cancelErr)
		}
		return outbox.Intent{}, fmt.Errorf("jiraintegration: comment on %s did not post: %s", issueKey, reason)
	}
	return resolved, nil
}

// ReconcileIntent re-reads remote Jira state to positively determine
// whether one of Service's own ambiguous write attempts already applied,
// dispatching by intent.Operation to the matching exported reconciler in
// internal/providerwrite - the single source of truth for how each
// operation is confirmed, shared with providerwrite's own worker so the
// same intent is never reconciled two different ways depending on which
// caller happens to retry it.
func (s *Service) ReconcileIntent(ctx context.Context, intent outbox.Intent) (providerwrite.ReconcileResult, error) {
	gate, err := s.registry.Gate(ctx, intent.AdapterID)
	if err != nil {
		return providerwrite.ReconcileResult{}, fmt.Errorf("jiraintegration: open %s adapter: %w", intent.AdapterID, err)
	}
	switch intent.Operation {
	case "atlassian.addJiraComment":
		return providerwrite.ReconcileJiraComment(ctx, gate, intent)
	case "atlassian.transitionJiraIssue":
		return providerwrite.ReconcileJiraTransition(ctx, gate, intent)
	case "atlassian.createJiraSubtask":
		return providerwrite.ReconcileJiraCreateSubtask(ctx, gate, intent)
	case "atlassian.addWorklog":
		return providerwrite.ReconcileJiraWorklog(ctx, gate, intent)
	default:
		return providerwrite.ReconcileResult{State: providerwrite.ReconcileUnknown, Diagnostic: "no reconciler registered for " + intent.Operation}, nil
	}
}

// resolveIssueKey returns the Jira issue key linked to deliveryID via its
// captured requirements - internal/delivery has no separate "linked Jira
// issue" field, so the first Jira-sourced requirement a delivery has
// captured is the closest existing analog to one - or "" if none has been
// captured.
func (s *Service) resolveIssueKey(ctx context.Context, deliveryID string) (string, error) {
	sources, err := s.store.ListRequirementSources(ctx, deliveryID)
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

// enqueueComment builds and enqueues one Jira comment intent for the given
// lifecycle label (e.g. "delivery started") against issueKey.
// eventFingerprintKey is folded into the intent's fingerprint so this
// instant's comment never collides with any other event's.
func (s *Service) enqueueComment(ctx context.Context, deliveryID, issueKey, label, eventFingerprintKey string) error {
	orch, err := s.store.GetOrchestration(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("get delivery: %w", err)
	}
	payload, err := json.Marshal(map[string]any{"comment_body": buildComment(label, orch)})
	if err != nil {
		return fmt.Errorf("encode comment payload: %w", err)
	}
	adapterID, err := s.adapterIDFor(ctx, deliveryID)
	if err != nil {
		return err
	}
	if _, err := s.outbox.Enqueue(ctx, outbox.Intent{
		OrchestrationID: deliveryID, AdapterID: adapterID, Operation: "atlassian.addJiraComment",
		TargetKey: issueKey, PayloadJSON: string(payload),
		OperationFingerprint: providerwrite.JiraCommentFingerprint(deliveryID, eventFingerprintKey, issueKey),
	}); err != nil {
		return fmt.Errorf("enqueue comment: %w", err)
	}
	return nil
}

// buildComment renders a compact Jira comment body for one delivery
// lifecycle instant.
func buildComment(label string, orch *protocol.DeliveryOrchestration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Punakawan %s", label)
	if orch.Title != nil && strings.TrimSpace(*orch.Title) != "" {
		fmt.Fprintf(&b, "\n\n%q", *orch.Title)
	}
	if len(orch.ProjectIds) > 0 {
		fmt.Fprintf(&b, "\n\nProjects: %s", strings.Join(orch.ProjectIds, ", "))
	}
	fmt.Fprintf(&b, "\n\nDelivery: %s", orch.Id)
	return b.String()
}

// enqueueTransition resolves targetStatus against issueKey's currently
// available transitions and, if exactly one matches, enqueues one
// jira.transition intent capturing the observed from/to status pair. The
// issue already being at targetStatus enqueues nothing (a normal outcome,
// not an error). Zero matches returns an error wrapping
// ErrTransitionNotConfigured; more than one match returns
// *ErrTransitionAmbiguous.
func (s *Service) enqueueTransition(ctx context.Context, deliveryID, issueKey, targetStatus string) error {
	adapterID, err := s.adapterIDFor(ctx, deliveryID)
	if err != nil {
		return err
	}
	gate, err := s.registry.Gate(ctx, adapterID)
	if err != nil {
		return fmt.Errorf("open %s adapter: %w", adapterID, err)
	}
	fromStatus, err := currentJiraStatus(ctx, gate, deliveryID, issueKey)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(fromStatus), strings.TrimSpace(targetStatus)) {
		return nil
	}

	raw, err := gate.Call(ctx, deliveryID, "atlassian.getTransitionsForJiraIssue", map[string]any{"issueIdOrKey": issueKey})
	if err != nil {
		return fmt.Errorf("list available transitions for %s: %w", issueKey, err)
	}
	transitions, err := adapters.DecodeJiraTransitions(raw)
	if err != nil {
		return err
	}
	matches := adapters.MatchAllJiraTransitions(transitions, targetStatus)
	switch len(matches) {
	case 0:
		return fmt.Errorf("%w: no transition on %s reaches configured status %q", ErrTransitionNotConfigured, issueKey, targetStatus)
	case 1:
		// proceed below
	default:
		options := make([]protocol.NeedUserInputOptionsElem, 0, len(matches))
		for _, m := range matches {
			options = append(options, protocol.NeedUserInputOptionsElem{
				Id: m.ID, Label: m.Name, Impact: fmt.Sprintf("Moves %s to %s", issueKey, m.ToStatusName),
			})
		}
		return &ErrTransitionAmbiguous{IssueKey: issueKey, TargetStatus: targetStatus, Options: options}
	}

	payload, err := json.Marshal(map[string]any{"target_status": targetStatus, "from_status": fromStatus})
	if err != nil {
		return fmt.Errorf("encode transition payload: %w", err)
	}
	if _, err := s.outbox.Enqueue(ctx, outbox.Intent{
		OrchestrationID: deliveryID, AdapterID: adapterID, Operation: "atlassian.transitionJiraIssue",
		TargetKey: issueKey, PayloadJSON: string(payload),
		OperationFingerprint: providerwrite.JiraTransitionFingerprint(deliveryID, issueKey, fromStatus, targetStatus),
	}); err != nil {
		return fmt.Errorf("enqueue transition: %w", err)
	}
	return nil
}

// currentJiraStatus reads issueKey's current workflow status.
func currentJiraStatus(ctx context.Context, gate *adapters.Gate, runID, issueKey string) (string, error) {
	raw, err := gate.Call(ctx, runID, "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": issueKey})
	if err != nil {
		return "", fmt.Errorf("fetch jira issue %s: %w", issueKey, err)
	}
	var result struct {
		Normalized struct {
			Status string `json:"status"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("decode jira issue %s: %w", issueKey, err)
	}
	return result.Normalized.Status, nil
}

// adapterIDFor names the adapter process that speaks for the Jira
// organisation this delivery's case lives on. It is resolved per write
// rather than once at construction because one Service handles deliveries
// across every configured organisation, and a write queued for one must
// never execute against another.
func (s *Service) adapterIDFor(ctx context.Context, deliveryID string) (string, error) {
	org, err := s.store.JiraOrgForDelivery(ctx, deliveryID)
	if err != nil {
		return "", fmt.Errorf("resolve organisation for delivery %s: %w", deliveryID, err)
	}
	return adapters.QualifyAdapterID("atlassian", org), nil
}
