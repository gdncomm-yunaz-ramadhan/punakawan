package providerwrite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
)

// SuccessObserver is notified, best-effort, after an intent succeeds -
// whether resolved by a fresh write or by reconciliation confirming one
// already applied. It exists so domain-specific bookkeeping that must react
// to a provider write actually landing (e.g. marking a worklog ledger entry
// synced once its jira.worklog intent succeeds) can run without this
// package depending on any concrete domain store. An observer error is
// logged and never allowed to retry, undo, or otherwise revisit the
// already-succeeded write.
type SuccessObserver interface {
	Observe(ctx context.Context, intent outbox.Intent, externalID string, effects []outbox.Effect) error
}

func notify(ctx context.Context, observer SuccessObserver, intent outbox.Intent, externalID string, effects []outbox.Effect) {
	if observer == nil {
		return
	}
	if err := observer.Observe(ctx, intent, externalID, effects); err != nil {
		slog.Warn("providerwrite: success observer failed; the provider write itself already landed and is unaffected",
			"intent_id", intent.ID, "operation", intent.Operation, "error", err)
	}
}

// ErrResponseLost signals that a provider call's response could not be
// read after the request may already have been sent (a dropped connection,
// a client-side read timeout, ...): the remote side may or may not have
// applied the write. A caller/adapter transport that can distinguish this
// from an ordinary rejection should wrap it with this sentinel so the
// worker marks the attempt ambiguous instead of blindly retrying it.
var ErrResponseLost = errors.New("providerwrite: response lost after the request may have been applied")

// gateResolver is the subset of *adapters.Registry's behavior the worker
// depends on, so tests can substitute a fake instead of spawning a real
// adapter subprocess.
type gateResolver interface {
	Gate(ctx context.Context, adapterID string) (*adapters.Gate, error)
}

// executeFunc performs one write given a claimed intent and its resolved
// Gate. It must call gate.ExecuteWrite - the one seam allowed to actually
// perform a side-effecting adapter call - and never gate.Call, which
// rejects side-effecting operations outright.
type executeFunc func(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (externalID string, effects []outbox.Effect, err error)

// executors dispatches a claimed intent's concrete adapter operation to the
// function that knows how to decode its payload and perform the write.
// Registering an operation here is what makes it executable; an intent
// naming any other operation (including a migrated pre-outbox row this
// worker does not understand) is retried with a clear diagnostic rather
// than silently misapplied.
var executors = map[string]executeFunc{
	"atlassian.addJiraComment":       executeJiraComment,
	"atlassian.transitionJiraIssue":  executeJiraTransition,
	"atlassian.addWorklog":           executeJiraWorklog,
	"atlassian.createJiraSubtask":    executeJiraCreateSubtask,
	"atlassian.editJiraIssue":        executeJiraEditFields,
	"github.createPullRequest":       executeGitHubCreatePR,
	"github.createPullRequestReview": executeGitHubReview,
	"github.addLabels":               executeGitHubLabels,
	"github.requestReviewers":        executeGitHubReviewers,
	"github.replyToReviewComment":    executeGitHubReply,
	"github.resolveReviewThread":     executeGitHubResolveThread,
}

func decodePayload(intent outbox.Intent) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(intent.PayloadJSON), &payload); err != nil {
		return nil, fmt.Errorf("providerwrite: decode payload for %s: %w", intent.ID, err)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return payload, nil
}

// jiraCommentMarker is embedded in every comment this package posts, so
// reconcileJiraComment can recognize a comment this exact intent already
// created after an ambiguous attempt, without depending on comparing free
// text bodies that a retried enqueue might phrase slightly differently.
// Deliberately not an HTML comment (`<!-- ... -->`): the Atlassian
// adapter's Markdown-to-ADF conversion (marklassian) treats a raw HTML
// comment as an HTML block and drops it entirely, so a marker embedded
// that way would never actually reach Jira - it must be plain text.
func jiraCommentMarker(intentID string) string {
	return "punakawan:intent:" + intentID
}

// jiraWorklogMarker is embedded in every worklog comment this package
// posts, so reconcileJiraWorklog can recognize a worklog this exact intent
// already created after an ambiguous attempt.
func jiraWorklogMarker(intentID string) string {
	return "punakawan:intent:" + intentID
}

// githubReviewMarker is embedded (as an HTML comment, invisible when
// rendered) in every pull request review body this package submits, so
// ReconcileGitHubReview can recognize a review this exact intent already
// submitted after an ambiguous attempt. Unlike Jira's markdown-to-ADF
// pipeline, GitHub's own Markdown rendering does not strip HTML comments.
func githubReviewMarker(intentID string) string {
	return "<!-- punakawan:intent:" + intentID + " -->"
}

// githubReplyMarker is embedded the same way in every review comment reply
// this package posts, so ReconcileGitHubReply can recognize a reply this
// exact intent already posted.
func githubReplyMarker(intentID string) string {
	return "<!-- punakawan:intent:" + intentID + " -->"
}

func executeJiraComment(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	body, _ := payload["comment_body"].(string)
	if strings.TrimSpace(body) == "" {
		return "", nil, fmt.Errorf("providerwrite: jira comment intent %s has no comment_body", intent.ID)
	}
	full := body + "\n\n[" + jiraCommentMarker(intent.ID) + "]"
	raw, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"issueIdOrKey": intent.TargetKey, "commentBody": full,
	})
	if err != nil {
		return "", nil, err
	}
	var result struct {
		CommentID string `json:"commentId"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("providerwrite: decode jira comment result: %w", err)
	}
	return result.CommentID, nil, nil
}

func executeJiraTransition(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	targetStatus, _ := payload["target_status"].(string)
	if strings.TrimSpace(targetStatus) == "" {
		return "", nil, fmt.Errorf("providerwrite: jira transition intent %s has no target_status", intent.ID)
	}
	raw, err := gate.Call(ctx, intent.ID, "atlassian.getTransitionsForJiraIssue", map[string]any{"issueIdOrKey": intent.TargetKey})
	if err != nil {
		return "", nil, fmt.Errorf("providerwrite: list Jira transitions for %s: %w", intent.TargetKey, err)
	}
	transitions, err := adapters.DecodeJiraTransitions(raw)
	if err != nil {
		return "", nil, err
	}
	match, available, ok := adapters.MatchJiraTransition(transitions, targetStatus)
	if !ok {
		return "", nil, fmt.Errorf("providerwrite: Jira transition to %q unavailable for %s; available targets: %s", targetStatus, intent.TargetKey, strings.Join(available, ", "))
	}
	if _, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"issueIdOrKey": intent.TargetKey, "transitionId": match.ID,
	}); err != nil {
		return "", nil, err
	}
	return match.ID, nil, nil
}

func executeJiraWorklog(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	seconds, ok := jsonNumber(payload["time_spent_seconds"])
	if !ok || seconds <= 0 {
		return "", nil, fmt.Errorf("providerwrite: jira worklog intent %s requires positive time_spent_seconds", intent.ID)
	}
	comment, _ := payload["comment"].(string)
	// jiraWorklogMarker below is what reconcileJiraWorklog searches for
	// after an ambiguous attempt; it must be embedded even when the caller
	// supplied no comment, since a worklog with only a marker is still
	// positively identifiable, while one with no comment at all is not.
	comment = strings.TrimSpace(comment + "\n\n[" + jiraWorklogMarker(intent.ID) + "]")
	params := map[string]any{"issueIdOrKey": intent.TargetKey, "timeSpentSeconds": int(seconds), "comment": comment}
	raw, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, params)
	if err != nil {
		return "", nil, err
	}
	var result struct {
		WorklogID string `json:"worklogId"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("providerwrite: decode jira worklog result: %w", err)
	}
	return result.WorklogID, nil, nil
}

func executeJiraCreateSubtask(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	projectKey, _ := payload["project_key"].(string)
	issueTypeName, _ := payload["issue_type_name"].(string)
	candidates, _ := payload["candidates"].([]any)
	if projectKey == "" || issueTypeName == "" || len(candidates) == 0 {
		return "", nil, fmt.Errorf("providerwrite: jira create-subtask intent %s requires project_key, issue_type_name, and candidates", intent.ID)
	}
	raw, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"parentKey": intent.TargetKey, "projectKey": projectKey, "issueTypeName": issueTypeName, "candidates": candidates,
	})
	if err != nil {
		return "", nil, err
	}
	var result struct {
		Created []struct {
			Key string `json:"key"`
		} `json:"created"`
		Skipped []struct {
			ExistingKey string `json:"existingKey"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("providerwrite: decode jira create-subtask result: %w", err)
	}
	var externalID string
	var effects []outbox.Effect
	for _, c := range result.Created {
		if c.Key == "" {
			continue
		}
		if externalID == "" {
			externalID = c.Key
		}
		effects = append(effects, outbox.Effect{IntentID: intent.ID, EffectKey: c.Key})
	}
	for _, s := range result.Skipped {
		if s.ExistingKey == "" {
			continue
		}
		if externalID == "" {
			externalID = s.ExistingKey
		}
		effects = append(effects, outbox.Effect{IntentID: intent.ID, EffectKey: s.ExistingKey})
	}
	return externalID, effects, nil
}

func executeJiraEditFields(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	fields, _ := payload["fields"].(map[string]any)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("providerwrite: jira edit-fields intent %s has no fields", intent.ID)
	}
	params := map[string]any{"issueIdOrKey": intent.TargetKey}
	for k, v := range fields {
		params[k] = v
	}
	if _, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, params); err != nil {
		return "", nil, err
	}
	return "", nil, nil
}

func executeGitHubCreatePR(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	raw, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"repository": intent.TargetKey,
		"baseBranch": payload["base_branch"],
		"headBranch": payload["head_branch"],
		"title":      payload["title"],
		"body":       payload["body"],
	})
	if err != nil {
		return "", nil, err
	}
	var result struct {
		Normalized struct {
			Number int    `json:"number"`
			Url    string `json:"url"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("providerwrite: decode github create-pr result: %w", err)
	}
	var effects []outbox.Effect
	if result.Normalized.Url != "" {
		// The pull request's URL has no other durable home on the intent
		// itself (ExternalID carries the number); recording it as a named
		// effect lets a synchronous caller (delivery.GitHubPRProvider) read
		// it straight back via Store.ListEffects.
		effects = append(effects, outbox.Effect{IntentID: intent.ID, EffectKey: "url", ExternalID: result.Normalized.Url})
	}
	if result.Normalized.Number == 0 {
		return result.Normalized.Url, effects, nil
	}
	return fmt.Sprintf("%d", result.Normalized.Number), effects, nil
}

func executeGitHubReview(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	body, _ := payload["body"].(string)
	body = strings.TrimSpace(body + "\n\n" + githubReviewMarker(intent.ID))
	params := map[string]any{
		"repository":        intent.TargetKey,
		"pullRequestNumber": payload["pull_request_number"],
		"body":              body,
		"event":             payload["event"],
		"commitId":          payload["head_sha"],
	}
	if comments, ok := payload["comments"]; ok {
		params["comments"] = comments
	}
	raw, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, params)
	if err != nil {
		return "", nil, err
	}
	var result struct {
		OK       bool   `json:"ok"`
		ReviewID string `json:"reviewId"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("providerwrite: decode github review result: %w", err)
	}
	if !result.OK || strings.TrimSpace(result.ReviewID) == "" {
		return "", nil, fmt.Errorf("providerwrite: github review adapter reported an unsuccessful result")
	}
	return result.ReviewID, nil, nil
}

func executeGitHubLabels(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	if _, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"repository": intent.TargetKey, "pullRequestNumber": payload["pull_request_number"], "labels": payload["labels"],
	}); err != nil {
		return "", nil, err
	}
	return "", nil, nil
}

func executeGitHubReviewers(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	if _, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"repository": intent.TargetKey, "pullRequestNumber": payload["pull_request_number"], "reviewers": payload["reviewers"],
	}); err != nil {
		return "", nil, err
	}
	return "", nil, nil
}

func executeGitHubReply(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	payload, err := decodePayload(intent)
	if err != nil {
		return "", nil, err
	}
	body, _ := payload["body"].(string)
	body = strings.TrimSpace(body + "\n\n" + githubReplyMarker(intent.ID))
	raw, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"repository": intent.TargetKey, "pullRequestNumber": payload["pull_request_number"],
		"commentId": payload["comment_id"], "body": body,
	})
	if err != nil {
		return "", nil, err
	}
	var result struct {
		Normalized struct {
			ID string `json:"id"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, fmt.Errorf("providerwrite: decode github reply result: %w", err)
	}
	return result.Normalized.ID, nil, nil
}

func executeGitHubResolveThread(ctx context.Context, gate *adapters.Gate, intent outbox.Intent) (string, []outbox.Effect, error) {
	if _, err := gate.ExecuteWrite(ctx, intent.ID, intent.Operation, map[string]any{
		"threadId": intent.TargetKey,
	}); err != nil {
		return "", nil, err
	}
	return "", nil, nil
}

func jsonNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// redact reduces err to a short, operator-useful diagnostic that never
// echoes back full request payloads (which may carry secrets or free-text
// business content) - only the error's own message, truncated defensively.
func redact(err error) string {
	msg := err.Error()
	const max = 500
	if len(msg) > max {
		msg = msg[:max] + "...(truncated)"
	}
	return msg
}

// retryBackoff is a capped exponential backoff keyed by how many attempts
// an intent has already had.
func retryBackoff(attemptCount int) time.Duration {
	if attemptCount < 1 {
		attemptCount = 1
	}
	seconds := math.Min(math.Pow(2, float64(attemptCount)), 300)
	return time.Duration(seconds) * time.Second
}

// classify decides what a failed execute attempt means: ambiguous (the
// remote call may have applied - ctx was cancelled/timed out mid-call, or
// the transport explicitly could not read a response) versus retryable
// (a clear rejection - safe to try again later).
func classify(ctx context.Context, err error) (ambiguous bool) {
	if ctx.Err() != nil {
		return true
	}
	if errors.Is(err, ErrResponseLost) {
		return true
	}
	// An adapter-reported cancellation (packages/adapter-sdk/src/stdio.ts's
	// serveStdio attaches data.code:"cancelled" when a handler's own
	// AbortSignal fired) means the adapter's own fetch may have already
	// reached the provider before the abort landed - the same "may have
	// applied" uncertainty as a local ctx cancellation, just observed on the
	// adapter side instead.
	var rpcErr *adapters.RPCError
	if errors.As(err, &rpcErr) && rpcErr.Cancelled() {
		return true
	}
	return false
}

// Worker claims and executes provider write intents one at a time. Pool
// runs a bounded number of Workers concurrently; a Worker is also usable
// directly (e.g. by ExecuteNow) for a single synchronous claim/execute
// cycle.
type Worker struct {
	ID        string
	Store     *outbox.Store
	Adapters  gateResolver
	LeaseTime time.Duration
	// Observer, if set, is notified after every intent this Worker resolves
	// to succeeded.
	Observer SuccessObserver
}

func (w *Worker) lease() time.Duration {
	if w.LeaseTime <= 0 {
		return 30 * time.Second
	}
	return w.LeaseTime
}

// RunOnce claims at most one intent and resolves it, returning whether it
// found any work to do.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	claimed, err := w.Store.Claim(ctx, w.ID, time.Now(), w.lease())
	if err != nil {
		return false, err
	}
	if claimed == nil {
		return false, nil
	}
	w.resolve(ctx, *claimed)
	return true, nil
}

func (w *Worker) resolve(ctx context.Context, intent outbox.Intent) {
	outcome, found, err := w.Store.LastAttemptOutcome(ctx, intent.ID)
	if err == nil && found && outcome == "ambiguous" {
		w.reconcile(ctx, intent)
		return
	}
	w.execute(ctx, intent)
}

func (w *Worker) execute(ctx context.Context, intent outbox.Intent) {
	fn, ok := executors[intent.Operation]
	if !ok {
		w.retry(ctx, intent, "no_executor", fmt.Sprintf("no executor registered for operation %q", intent.Operation))
		return
	}
	gate, err := w.Adapters.Gate(ctx, intent.AdapterID)
	if err != nil {
		w.retry(ctx, intent, "adapter_unavailable", redact(err))
		return
	}
	externalID, effects, err := fn(ctx, gate, intent)
	if err == nil {
		if _, err := w.Store.Succeed(ctx, intent.ID, w.ID, externalID, intent.ID, effects); err != nil {
			// Nothing more this Worker can do about a failure to record an
			// already-applied external effect; the next claim (this Worker's
			// or another's) will observe the intent still claimed/expired and
			// try again, and reconciliation guards against a duplicate.
			_ = err
		} else {
			notify(ctx, w.Observer, intent, externalID, effects)
		}
		return
	}
	if classify(ctx, err) {
		if _, merr := w.Store.MarkAmbiguous(ctx, intent.ID, w.ID, "", redact(err)); merr != nil {
			_ = merr
		}
		return
	}
	w.retry(ctx, intent, "adapter_error", redact(err))
}

func (w *Worker) retry(ctx context.Context, intent outbox.Intent, code, redacted string) {
	at := time.Now().Add(retryBackoff(intent.AttemptCount))
	if _, err := w.Store.Retry(ctx, intent.ID, w.ID, code, redacted, at); err != nil {
		_ = err
	}
}

func (w *Worker) reconcile(ctx context.Context, intent outbox.Intent) {
	fn, ok := reconcilers[intent.Operation]
	if !ok {
		if _, err := w.Store.MarkAmbiguous(ctx, intent.ID, w.ID, "",
			fmt.Sprintf("no reconciliation capability for operation %q; remote state cannot be distinguished as applied or not, so this write is never replayed blindly", intent.Operation)); err != nil {
			_ = err
		}
		return
	}
	gate, err := w.Adapters.Gate(ctx, intent.AdapterID)
	if err != nil {
		if _, merr := w.Store.MarkAmbiguous(ctx, intent.ID, w.ID, "", redact(err)); merr != nil {
			_ = merr
		}
		return
	}
	result, err := fn(ctx, gate, intent)
	if err != nil {
		if _, merr := w.Store.MarkAmbiguous(ctx, intent.ID, w.ID, "", redact(err)); merr != nil {
			_ = merr
		}
		return
	}
	switch result.State {
	case ReconcileApplied:
		if _, err := w.Store.Succeed(ctx, intent.ID, w.ID, result.ExternalID, "", result.Effects); err != nil {
			_ = err
		} else {
			notify(ctx, w.Observer, intent, result.ExternalID, result.Effects)
		}
	case ReconcileNotApplied:
		w.retry(ctx, intent, "reconciled_not_applied", "reconciliation confirmed the write had not applied; safe to retry")
	default:
		if _, err := w.Store.MarkAmbiguous(ctx, intent.ID, w.ID, "", result.Diagnostic); err != nil {
			_ = err
		}
	}
}

// Pool runs a bounded number of Workers, each looping RunOnce until ctx is
// cancelled. It is the shape the daemon owns (default two workers): claims
// stop as soon as ctx is cancelled, and every in-flight adapter call
// receives that same cancellation, so a claimed write in progress at
// shutdown becomes ambiguous (per classify) rather than left to finish
// unobserved.
type Pool struct {
	store    *outbox.Store
	adapters gateResolver
	workers  int
	idle     time.Duration
	observer SuccessObserver

	stop chan struct{}
	done chan struct{}
}

// NewPool builds a Pool of workers workers (at least 1), each idling for
// idle between empty claim attempts. observer may be nil.
func NewPool(store *outbox.Store, adapters gateResolver, workers int, idle time.Duration, observer SuccessObserver) *Pool {
	if workers < 1 {
		workers = 1
	}
	if idle <= 0 {
		idle = time.Second
	}
	return &Pool{store: store, adapters: adapters, workers: workers, idle: idle, observer: observer}
}

// Start launches the bounded worker pool in the background. It returns
// immediately; call Stop to shut it down.
func (p *Pool) Start(ctx context.Context) {
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	runCtx, cancel := context.WithCancel(ctx)

	var pending int
	results := make(chan struct{}, p.workers)
	for i := 0; i < p.workers; i++ {
		pending++
		go func(id int) {
			defer func() { results <- struct{}{} }()
			w := &Worker{ID: fmt.Sprintf("pool-%d", id), Store: p.store, Adapters: p.adapters, Observer: p.observer}
			for {
				select {
				case <-p.stop:
					cancel()
					return
				default:
				}
				did, err := w.RunOnce(runCtx)
				if err != nil {
					select {
					case <-p.stop:
						cancel()
						return
					case <-time.After(p.idle):
					}
					continue
				}
				if !did {
					select {
					case <-p.stop:
						cancel()
						return
					case <-time.After(p.idle):
					}
				}
			}
		}(i)
	}

	go func() {
		for i := 0; i < pending; i++ {
			<-results
		}
		cancel()
		close(p.done)
	}()
}

// Stop stops claims and waits (bounded by ctx) for in-flight work to
// observe cancellation and return. Any claim left unfinished simply expires
// its lease naturally (Store.Claim already reclaims an expired claim), so
// Stop performs no separate "release" step.
func (p *Pool) Stop(ctx context.Context) error {
	if p.stop == nil {
		return nil
	}
	close(p.stop)
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
