// Package providerwrite is the one execution path allowed to perform a
// provider write: it validates a domain service's request, enqueues it into
// the durable outbox (internal/outbox) under a deterministic fingerprint,
// and - via Pool's bounded worker loop, or ExecuteNow's synchronous
// convenience wrapper over the same claim/execute/resolve sequence - is the
// only code that ever calls adapters.Gate.ExecuteWrite.
//
// Every fingerprint format below is fixed on purpose: two enqueue attempts
// describing the same logical effect (a retried MCP call, a redelivered
// lifecycle hook) must always collapse onto the same outbox row instead of
// racing a second attempt at the same remote mutation.
package providerwrite

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// JiraCommentFingerprint identifies posting one Jira comment for one
// delivery event against one target issue.
func JiraCommentFingerprint(deliveryID, eventType, target string) string {
	return fmt.Sprintf("jira.comment:%s:%s:%s", deliveryID, eventType, target)
}

// JiraTransitionFingerprint identifies transitioning target from fromStatus
// to toStatus for one delivery. fromStatus is captured by a live read at
// enqueue time (never guessed), so a transition attempted twice from two
// different observed starting statuses is never silently collapsed onto
// the same row.
func JiraTransitionFingerprint(deliveryID, target, fromStatus, toStatus string) string {
	return fmt.Sprintf("jira.transition:%s:%s:%s:%s", deliveryID, target, fromStatus, toStatus)
}

// JiraWorklogFingerprint identifies syncing one already-recorded, immutable
// worklog ledger entry to Jira. Using the ledger entry's own durable id
// means every path that might sync the same entry (an automatic hook, an
// explicit retry) converges on exactly one outbox row.
func JiraWorklogFingerprint(worklogEntryID string) string {
	return fmt.Sprintf("jira.worklog:%s", worklogEntryID)
}

// JiraCreateSubtaskFingerprint identifies creating one normalized subtask
// under parent for one delivery.
func JiraCreateSubtaskFingerprint(deliveryID, parent, normalizedSummary string) string {
	return fmt.Sprintf("jira.create-subtask:%s:%s:%s", deliveryID, parent, normalizeSummary(normalizedSummary))
}

// JiraEditFieldsFingerprint identifies one edit to target's fields for one
// delivery. fieldSetHash should be HashFields of the exact field set being
// written, so two different edits to the same issue never collide.
func JiraEditFieldsFingerprint(deliveryID, target, fieldSetHash string) string {
	return fmt.Sprintf("jira.edit-fields:%s:%s:%s", deliveryID, target, fieldSetHash)
}

// GitHubCreatePRFingerprint identifies opening one pull request from head
// onto base in repository.
func GitHubCreatePRFingerprint(repository, head, base string) string {
	return fmt.Sprintf("github.create-pr:%s:%s:%s", repository, head, base)
}

// GitHubReviewFingerprint identifies submitting one review for repository's
// pull request at headSHA. reviewID is the caller's own durable review
// proposal id (delivery.GitHubPRReview.ID), not GitHub's - it distinguishes
// two separate review proposals against the same PR/SHA.
func GitHubReviewFingerprint(repository string, prNumber int, headSHA, reviewID string) string {
	return fmt.Sprintf("github.review:%s:%d:%s:%s", repository, prNumber, headSHA, reviewID)
}

// GitHubLabelsFingerprint identifies replacing repository's pull request's
// label set with exactly labels.
func GitHubLabelsFingerprint(repository string, prNumber int, labels []string) string {
	return fmt.Sprintf("github.labels:%s:%d:%s", repository, prNumber, hashSorted(labels))
}

// GitHubReviewersFingerprint identifies requesting exactly reviewers on
// repository's pull request.
func GitHubReviewersFingerprint(repository string, prNumber int, reviewers []string) string {
	return fmt.Sprintf("github.reviewers:%s:%d:%s", repository, prNumber, hashSorted(reviewers))
}

// GitHubReplyFingerprint identifies replying with body to one existing
// review comment.
func GitHubReplyFingerprint(repository, commentID, body string) string {
	return fmt.Sprintf("github.reply:%s:%s:%s", repository, commentID, hashBody(body))
}

// GitHubResolveThreadFingerprint identifies resolving one review thread.
func GitHubResolveThreadFingerprint(threadID string) string {
	return fmt.Sprintf("github.resolve-thread:%s", threadID)
}

// HashFields is exported for callers building a Jira edit-fields
// fingerprint: it hashes the exact set of field names and values being
// written, so JiraEditFieldsFingerprint distinguishes two different edits
// to the same issue.
func HashFields(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v\x00", k, fields[k])
	}
	return hashString(b.String())
}

func hashSorted(values []string) string {
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return hashString(strings.Join(sorted, "\x00"))
}

func hashBody(body string) string {
	return hashString(body)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// normalizeSummary lower-cases and collapses whitespace in a subtask
// summary, so two enqueue attempts describing the same intended subtask
// (differing only by whitespace/case) fingerprint identically.
func normalizeSummary(summary string) string {
	fields := strings.Fields(strings.ToLower(summary))
	return strings.Join(fields, " ")
}
