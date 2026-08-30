// reconcile.go implements operation-specific reconciliation: before an
// intent whose most recent attempt was ambiguous is ever retried, the
// worker (see Worker.reconcile in worker.go) tries to positively determine
// whether the remote write already applied, by re-reading current remote
// state rather than by replaying the write.
//
// Every case this file cannot resolve - because the Atlassian and GitHub
// adapters (packages/adapter-atlassian, packages/github-adapter) do not yet
// expose a read capable of distinguishing "applied" from "not applied" for
// that operation - is deliberately left unregistered in reconcilers, so
// Worker.reconcile falls back to keeping the intent reconciling with a
// redacted diagnostic and never replays it blindly. As of this package:
//
//   - jira.worklog (atlassian.addWorklog): the Atlassian adapter has no
//     "list worklogs" read operation, so an ambiguous worklog sync cannot be
//     confirmed against the remote and stays reconciling until an adapter
//     capable of listing worklogs lands.
//   - github.create-pr, github.review, github.labels, github.reviewers,
//     github.reply, github.resolve-thread: the GitHub adapter exposes no
//     read that returns a PR by head/base, lists a PR's reviews, or reports
//     its current label/reviewer/thread-resolution state - only
//     github.getPullRequest by number, which cannot answer any of these.
//     Hardening the GitHub adapter with these reads is a separate,
//     later concern; this package's contract (ReconcileResult,
//     reconcilers) is ready to register them the moment such a read
//     exists.
package providerwrite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
)

// ReconcileState is what a reconciler determined about a remote write it
// could not confirm from the write attempt's own response.
type ReconcileState int

const (
	// ReconcileUnknown means the remote state still cannot distinguish
	// applied from not-applied; the intent stays reconciling.
	ReconcileUnknown ReconcileState = iota
	// ReconcileApplied means the reconciler found positive evidence the
	// write already landed; the intent is marked succeeded without ever
	// replaying the write.
	ReconcileApplied
	// ReconcileNotApplied means the reconciler found positive evidence the
	// write never landed; the intent is safe to retry.
	ReconcileNotApplied
)

// ReconcileResult is one reconciler's determination.
type ReconcileResult struct {
	State      ReconcileState
	ExternalID string
	Effects    []outbox.Effect
	// Diagnostic is recorded (redacted) when State is ReconcileUnknown, to
	// explain to an operator why this intent is still waiting.
	Diagnostic string
}

// reconcileFunc re-reads remote state for intent and reports what it found.
// It must never call gate.ExecuteWrite - only read-only Gate.Call
// operations - since a reconciler's entire purpose is to avoid ever
// replaying a write blindly.
type reconcileFunc func(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error)

// reconcilers is intentionally sparse - see this file's package doc comment
// for exactly which operations are not registered and why.
var reconcilers = map[string]reconcileFunc{
	"atlassian.addJiraComment":      reconcileJiraComment,
	"atlassian.transitionJiraIssue": reconcileJiraTransition,
	"atlassian.createJiraSubtask":   reconcileJiraCreateSubtask,
}

// reconcileJiraComment searches issue comments for the marker
// executeJiraComment embeds in every comment it posts.
func reconcileJiraComment(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	marker := jiraCommentMarker(intent.ID)
	raw, err := gate.Call(ctx, intent.ID, "atlassian.getJiraComments", map[string]any{"issueIdOrKey": intent.TargetKey, "maxResults": 100})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: list jira comments for %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Comments []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode jira comments: %w", err)
	}
	for _, c := range result.Comments {
		if strings.Contains(c.Body, marker) {
			return ReconcileResult{State: ReconcileApplied, ExternalID: c.ID}, nil
		}
	}
	return ReconcileResult{State: ReconcileNotApplied}, nil
}

// reconcileJiraTransition refetches the issue's current status and compares
// it against the intent's target status.
func reconcileJiraTransition(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return ReconcileResult{}, err
	}
	targetStatus, _ := payload["target_status"].(string)
	raw, err := gate.Call(ctx, intent.ID, "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": intent.TargetKey})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: refetch jira issue %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Normalized struct {
			Status string `json:"status"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode jira issue: %w", err)
	}
	if strings.EqualFold(strings.TrimSpace(result.Normalized.Status), strings.TrimSpace(targetStatus)) {
		return ReconcileResult{State: ReconcileApplied}, nil
	}
	return ReconcileResult{State: ReconcileNotApplied}, nil
}

// reconcileJiraCreateSubtask refetches the parent issue and matches its
// current subtasks by normalized summary. A subtask summary cannot embed an
// invisible marker the way a comment body can without changing what is
// visibly created on Jira, so this matches on normalized summary equality
// alone - the same normalization JiraCreateSubtaskFingerprint itself
// applies, so a candidate that truly already exists always compares equal.
func reconcileJiraCreateSubtask(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return ReconcileResult{}, err
	}
	candidates, _ := payload["candidates"].([]any)
	wanted := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if summary, ok := m["summary"].(string); ok {
			wanted[normalizeSummary(summary)] = true
		}
	}
	raw, err := gate.Call(ctx, intent.ID, "atlassian.getJiraIssue", map[string]any{"issueIdOrKey": intent.TargetKey})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: refetch jira parent issue %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Normalized struct {
			Subtasks []struct {
				Key     string `json:"key"`
				Summary string `json:"summary"`
			} `json:"subtasks"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode jira parent issue: %w", err)
	}
	var effects []outbox.Effect
	var externalID string
	for _, st := range result.Normalized.Subtasks {
		if wanted[normalizeSummary(st.Summary)] {
			if externalID == "" {
				externalID = st.Key
			}
			effects = append(effects, outbox.Effect{IntentID: intent.ID, EffectKey: st.Key})
		}
	}
	if len(effects) > 0 {
		return ReconcileResult{State: ReconcileApplied, ExternalID: externalID, Effects: effects}, nil
	}
	return ReconcileResult{State: ReconcileNotApplied}, nil
}
