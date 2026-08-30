// reconcile.go implements operation-specific reconciliation: before an
// intent whose most recent attempt was ambiguous is ever retried, the
// worker (see Worker.reconcile in worker.go) tries to positively determine
// whether the remote write already applied, by re-reading current remote
// state rather than by replaying the write.
//
// Every operation the outbox executes (see executors in worker.go) has a
// registered reconciler here, now that both adapters expose a read capable
// of distinguishing "applied" from "not applied" for each one: Jira's
// worklog list, and GitHub's exact head/base pull request search, review
// list, enriched label/reviewer PR fields, comment thread listing, and
// review-thread resolution state.
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

// reconcilers covers every operation executors (worker.go) knows how to
// execute. atlassian.editJiraIssue is the one deliberate exception: a field
// edit's "applied" state cannot be distinguished from "not applied" by
// re-reading the issue without knowing which prior value each field held,
// which the payload does not capture - re-reading only ever shows the
// field's current value, not whether this exact intent is what set it.
var reconcilers = map[string]reconcileFunc{
	"atlassian.addJiraComment":       ReconcileJiraComment,
	"atlassian.transitionJiraIssue":  ReconcileJiraTransition,
	"atlassian.createJiraSubtask":    ReconcileJiraCreateSubtask,
	"atlassian.addWorklog":           ReconcileJiraWorklog,
	"github.createPullRequest":       ReconcileGitHubCreatePR,
	"github.createPullRequestReview": ReconcileGitHubReview,
	"github.addLabels":               ReconcileGitHubLabels,
	"github.requestReviewers":        ReconcileGitHubReviewers,
	"github.replyToReviewComment":    ReconcileGitHubReply,
	"github.resolveReviewThread":     ReconcileGitHubResolveThread,
}

// ReconcileJiraComment searches issue comments for the marker
// executeJiraComment embeds in every comment it posts.
func ReconcileJiraComment(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
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

// ReconcileJiraTransition refetches the issue's current status and compares
// it against the intent's target status.
func ReconcileJiraTransition(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
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

// ReconcileJiraCreateSubtask refetches the parent issue and matches its
// current subtasks by normalized summary. A subtask summary cannot embed an
// invisible marker the way a comment body can without changing what is
// visibly created on Jira, so this matches on normalized summary equality
// alone - the same normalization JiraCreateSubtaskFingerprint itself
// applies, so a candidate that truly already exists always compares equal.
func ReconcileJiraCreateSubtask(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
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

// ReconcileJiraWorklog searches an issue's worklog entries for the marker
// executeJiraWorklog embeds in every worklog comment it posts.
func ReconcileJiraWorklog(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	marker := jiraWorklogMarker(intent.ID)
	raw, err := gate.Call(ctx, intent.ID, "atlassian.listJiraWorklogs", map[string]any{"issueIdOrKey": intent.TargetKey})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: list jira worklogs for %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Worklogs []struct {
			ID      string `json:"id"`
			Comment string `json:"comment"`
		} `json:"worklogs"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode jira worklogs: %w", err)
	}
	for _, w := range result.Worklogs {
		if strings.Contains(w.Comment, marker) {
			return ReconcileResult{State: ReconcileApplied, ExternalID: w.ID}, nil
		}
	}
	return ReconcileResult{State: ReconcileNotApplied}, nil
}

// ReconcileGitHubCreatePR searches for an exact head/base pull request
// match across both open and closed pull requests.
func ReconcileGitHubCreatePR(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return ReconcileResult{}, err
	}
	headBranch, _ := payload["head_branch"].(string)
	baseBranch, _ := payload["base_branch"].(string)
	raw, err := gate.Call(ctx, intent.ID, "github.findPullRequest", map[string]any{
		"repository": intent.TargetKey, "headBranch": headBranch, "baseBranch": baseBranch,
	})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: search github pull requests in %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Normalized *struct {
			Number int    `json:"number"`
			Url    string `json:"url"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode github pull request search: %w", err)
	}
	if result.Normalized == nil {
		return ReconcileResult{State: ReconcileNotApplied}, nil
	}
	var effects []outbox.Effect
	if result.Normalized.Url != "" {
		effects = append(effects, outbox.Effect{IntentID: intent.ID, EffectKey: "url", ExternalID: result.Normalized.Url})
	}
	return ReconcileResult{State: ReconcileApplied, ExternalID: fmt.Sprintf("%d", result.Normalized.Number), Effects: effects}, nil
}

// ReconcileGitHubReview matches the intent's own marker and target commit
// SHA against reviews already submitted on the pull request - a review
// submitted twice with different bodies (e.g. a retried enqueue that
// recomputed findings) still must never collide on a plain body-text
// comparison, but the marker (unique per intent) plus the exact commit it
// was proposed against together identify this intent's own submission
// unambiguously.
func ReconcileGitHubReview(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return ReconcileResult{}, err
	}
	prNumber, _ := jsonNumber(payload["pull_request_number"])
	headSHA, _ := payload["head_sha"].(string)
	marker := githubReviewMarker(intent.ID)
	raw, err := gate.Call(ctx, intent.ID, "github.listPullRequestReviews", map[string]any{
		"repository": intent.TargetKey, "pullRequestNumber": int(prNumber),
	})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: list github pull request reviews for %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Normalized []struct {
			ID       string `json:"id"`
			Body     string `json:"body"`
			CommitId string `json:"commitId"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode github pull request reviews: %w", err)
	}
	for _, review := range result.Normalized {
		if strings.Contains(review.Body, marker) && review.CommitId == headSHA {
			return ReconcileResult{State: ReconcileApplied, ExternalID: review.ID}, nil
		}
	}
	return ReconcileResult{State: ReconcileNotApplied}, nil
}

// ReconcileGitHubLabels refetches the pull request and checks that every
// requested label is already present - github.addLabels only ever adds
// labels (it never replaces the set), so "every requested label present"
// is exactly what "applied" means here, regardless of what else the PR
// happens to be labeled with.
func ReconcileGitHubLabels(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return ReconcileResult{}, err
	}
	wanted := decodeStringSlice(payload["labels"])
	if len(wanted) == 0 {
		return ReconcileResult{State: ReconcileUnknown, Diagnostic: "intent has no labels to check"}, nil
	}
	prNumber, _ := jsonNumber(payload["pull_request_number"])
	current, err := currentGitHubPullRequestLabels(ctx, gate, intent, int(prNumber))
	if err != nil {
		return ReconcileResult{}, err
	}
	for _, label := range wanted {
		if !current[label] {
			return ReconcileResult{State: ReconcileNotApplied}, nil
		}
	}
	return ReconcileResult{State: ReconcileApplied}, nil
}

// ReconcileGitHubReviewers refetches the pull request and checks that
// every requested reviewer is already listed as a requested reviewer.
func ReconcileGitHubReviewers(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return ReconcileResult{}, err
	}
	wanted := decodeStringSlice(payload["reviewers"])
	if len(wanted) == 0 {
		return ReconcileResult{State: ReconcileUnknown, Diagnostic: "intent has no reviewers to check"}, nil
	}
	prNumber, _ := jsonNumber(payload["pull_request_number"])
	raw, err := gate.Call(ctx, intent.ID, "github.getPullRequest", map[string]any{
		"repository": intent.TargetKey, "pullRequestNumber": int(prNumber),
	})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: refetch github pull request %s#%d: %w", intent.TargetKey, int(prNumber), err)
	}
	var result struct {
		Normalized struct {
			RequestedReviewers []string `json:"requestedReviewers"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode github pull request: %w", err)
	}
	current := make(map[string]bool, len(result.Normalized.RequestedReviewers))
	for _, r := range result.Normalized.RequestedReviewers {
		current[r] = true
	}
	for _, r := range wanted {
		if !current[r] {
			return ReconcileResult{State: ReconcileNotApplied}, nil
		}
	}
	return ReconcileResult{State: ReconcileApplied}, nil
}

// ReconcileGitHubReply matches the intent's own marker against every reply
// already posted under the target comment.
func ReconcileGitHubReply(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return ReconcileResult{}, err
	}
	commentID, _ := payload["comment_id"].(string)
	prNumber, _ := jsonNumber(payload["pull_request_number"])
	marker := githubReplyMarker(intent.ID)
	raw, err := gate.Call(ctx, intent.ID, "github.listPullRequestComments", map[string]any{
		"repository": intent.TargetKey, "pullRequestNumber": int(prNumber),
	})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: list github pull request comments for %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Normalized []struct {
			ID          string `json:"id"`
			Body        string `json:"body"`
			InReplyToId string `json:"inReplyToId"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode github pull request comments: %w", err)
	}
	for _, c := range result.Normalized {
		if c.InReplyToId == commentID && strings.Contains(c.Body, marker) {
			return ReconcileResult{State: ReconcileApplied, ExternalID: c.ID}, nil
		}
	}
	return ReconcileResult{State: ReconcileNotApplied}, nil
}

// ReconcileGitHubResolveThread refetches the review thread's current
// resolution state by node id.
func ReconcileGitHubResolveThread(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (ReconcileResult, error) {
	raw, err := gate.Call(ctx, intent.ID, "github.getReviewThread", map[string]any{"threadId": intent.TargetKey})
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: refetch github review thread %s: %w", intent.TargetKey, err)
	}
	var result struct {
		Normalized *struct {
			IsResolved bool `json:"isResolved"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return ReconcileResult{}, fmt.Errorf("providerwrite: decode github review thread: %w", err)
	}
	if result.Normalized == nil {
		return ReconcileResult{State: ReconcileUnknown, Diagnostic: "review thread not found"}, nil
	}
	if result.Normalized.IsResolved {
		return ReconcileResult{State: ReconcileApplied}, nil
	}
	return ReconcileResult{State: ReconcileNotApplied}, nil
}

// currentGitHubPullRequestLabels refetches the pull request and returns its
// current label set, shared by ReconcileGitHubLabels (kept as its own
// function since a future caller - e.g. a dry-run diagnostic - may want
// just the current set without the pass/fail check).
func currentGitHubPullRequestLabels(ctx context.Context, gate *adapters.Gate, intent outbox.Intent, prNumber int) (map[string]bool, error) {
	raw, err := gate.Call(ctx, intent.ID, "github.getPullRequest", map[string]any{
		"repository": intent.TargetKey, "pullRequestNumber": prNumber,
	})
	if err != nil {
		return nil, fmt.Errorf("providerwrite: refetch github pull request %s#%d: %w", intent.TargetKey, prNumber, err)
	}
	var result struct {
		Normalized struct {
			Labels []string `json:"labels"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("providerwrite: decode github pull request: %w", err)
	}
	current := make(map[string]bool, len(result.Normalized.Labels))
	for _, l := range result.Normalized.Labels {
		current[l] = true
	}
	return current, nil
}

// decodeStringSlice extracts a []string from a decoded JSON payload field,
// which JSON-decodes as []any of strings - anything else in the slice
// (should the payload be malformed) is silently skipped rather than
// erroring, since a reconciler is inherently best-effort diagnostics, not a
// path that must reject bad input strictly.
func decodeStringSlice(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
