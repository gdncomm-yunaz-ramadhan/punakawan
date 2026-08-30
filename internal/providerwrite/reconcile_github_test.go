package providerwrite

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func githubManifest() protocol.AdapterManifest {
	return protocol.AdapterManifest{
		Id: "github", Name: "github", Version: "0.1.0", Protocol: "punakawan.adapter/v1",
		Runtime: protocol.AdapterManifestRuntimeNode, Provides: []string{"github"},
		Permissions: protocol.AdapterManifestPermissions{
			Network:    protocol.AdapterManifestPermissionsNetwork{Hosts: []string{"api.github.com"}},
			Filesystem: protocol.AdapterManifestPermissionsFilesystem{Read: []string{}, Write: []string{}},
			Secrets:    []string{},
		},
		Operations: protocol.AdapterManifestOperations{
			"github.findPullRequest":         {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.getPullRequest":          {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.listPullRequestReviews":  {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.listPullRequestComments": {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.getReviewThread":         {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
		},
	}
}

// fakeGitHubReadProvider answers exactly one GitHub read operation with a
// fixed, pre-marshaled JSON result, so each reconciler can be exercised in
// isolation without a real subprocess.
type fakeGitHubReadProvider struct {
	op     string
	result json.RawMessage
}

func (f fakeGitHubReadProvider) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	op, _ := args["op"].(string)
	if op != f.op {
		return nil, fmt.Errorf("fakeGitHubReadProvider: unhandled op %q, want %q", op, f.op)
	}
	return f.result, nil
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestReconcileGitHubCreatePR_AppliedOnExactHeadBaseMatch(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": map[string]any{"number": 42, "url": "https://github.com/acme/widgets/pull/42"}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.findPullRequest", result: result})
	payload, _ := json.Marshal(map[string]any{"head_branch": "feature-x", "base_branch": "main"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubCreatePR(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubCreatePR: %v", err)
	}
	if got.State != ReconcileApplied || got.ExternalID != "42" {
		t.Fatalf("result = %+v, want ReconcileApplied with external id 42", got)
	}
	if len(got.Effects) != 1 || got.Effects[0].ExternalID != "https://github.com/acme/widgets/pull/42" {
		t.Fatalf("effects = %+v, want the pull request URL recorded", got.Effects)
	}
}

func TestReconcileGitHubCreatePR_NotAppliedWhenNoMatch(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": nil})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.findPullRequest", result: result})
	payload, _ := json.Marshal(map[string]any{"head_branch": "feature-x", "base_branch": "main"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubCreatePR(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubCreatePR: %v", err)
	}
	if got.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", got.State)
	}
}

func TestReconcileGitHubReview_AppliedOnMarkerAndCommitMatch(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": []map[string]any{
		{"id": "rev-1", "body": "Looks good\n\n" + githubReviewMarker("intent-1"), "commitId": "abc123"},
	}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.listPullRequestReviews", result: result})
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "head_sha": "abc123"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubReview(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubReview: %v", err)
	}
	if got.State != ReconcileApplied || got.ExternalID != "rev-1" {
		t.Fatalf("result = %+v, want ReconcileApplied with rev-1", got)
	}
}

func TestReconcileGitHubReview_NotAppliedWhenCommitDiffers(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": []map[string]any{
		{"id": "rev-1", "body": "Looks good\n\n" + githubReviewMarker("intent-1"), "commitId": "different-sha"},
	}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.listPullRequestReviews", result: result})
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "head_sha": "abc123"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubReview(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubReview: %v", err)
	}
	if got.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", got.State)
	}
}

func TestReconcileGitHubLabels_AppliedWhenEveryLabelPresent(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": map[string]any{"labels": []string{"needs-review", "bug"}}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.getPullRequest", result: result})
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "labels": []string{"bug"}})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubLabels(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubLabels: %v", err)
	}
	if got.State != ReconcileApplied {
		t.Fatalf("state = %v, want ReconcileApplied", got.State)
	}
}

func TestReconcileGitHubLabels_NotAppliedWhenLabelMissing(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": map[string]any{"labels": []string{"needs-review"}}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.getPullRequest", result: result})
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "labels": []string{"bug"}})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubLabels(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubLabels: %v", err)
	}
	if got.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", got.State)
	}
}

func TestReconcileGitHubReviewers_AppliedWhenEveryReviewerPresent(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": map[string]any{"requestedReviewers": []string{"reviewer1", "reviewer2"}}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.getPullRequest", result: result})
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "reviewers": []string{"reviewer1"}})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubReviewers(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubReviewers: %v", err)
	}
	if got.State != ReconcileApplied {
		t.Fatalf("state = %v, want ReconcileApplied", got.State)
	}
}

func TestReconcileGitHubReply_AppliedOnMarkerAndInReplyToMatch(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": []map[string]any{
		{"id": "c-2", "body": "Fixed\n\n" + githubReplyMarker("intent-1"), "inReplyToId": "c-1"},
	}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.listPullRequestComments", result: result})
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "comment_id": "c-1"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", PayloadJSON: string(payload)}

	got, err := ReconcileGitHubReply(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubReply: %v", err)
	}
	if got.State != ReconcileApplied || got.ExternalID != "c-2" {
		t.Fatalf("result = %+v, want ReconcileApplied with c-2", got)
	}
}

func TestReconcileGitHubResolveThread_AppliedWhenResolved(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": map[string]any{"isResolved": true}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.getReviewThread", result: result})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "thread-1"}

	got, err := ReconcileGitHubResolveThread(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubResolveThread: %v", err)
	}
	if got.State != ReconcileApplied {
		t.Fatalf("state = %v, want ReconcileApplied", got.State)
	}
}

func TestReconcileGitHubResolveThread_NotAppliedWhenStillUnresolved(t *testing.T) {
	result := mustMarshal(t, map[string]any{"normalized": map[string]any{"isResolved": false}})
	gate := adapters.NewGate("github", githubManifest(), fakeGitHubReadProvider{op: "github.getReviewThread", result: result})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "thread-1"}

	got, err := ReconcileGitHubResolveThread(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("ReconcileGitHubResolveThread: %v", err)
	}
	if got.State != ReconcileNotApplied {
		t.Fatalf("state = %v, want ReconcileNotApplied", got.State)
	}
}

// fakeGitHubWriteProvider records the params of a single ExecuteWrite call
// and returns a fixed result, so an executor's exact outgoing params can be
// asserted directly.
type fakeGitHubWriteProvider struct {
	op     string
	params map[string]any
	result json.RawMessage
}

func (f *fakeGitHubWriteProvider) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	op, _ := args["op"].(string)
	if op != f.op {
		return nil, fmt.Errorf("fakeGitHubWriteProvider: unhandled op %q, want %q", op, f.op)
	}
	f.params = args
	return f.result, nil
}

func githubWriteManifest(op string) protocol.AdapterManifest {
	m := githubManifest()
	m.Operations = protocol.AdapterManifestOperations{
		op: {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
	}
	return m
}

// TestExecuteGitHubReply_SendsPullRequestNumberAndEmbedsMarker guards a real
// bug: the reply executor never sent pullRequestNumber at all, even though
// replying to a review comment requires it - every call would have failed
// against a real adapter (whose manifest declares it required) or produced
// a malformed request URL against GitHub's REST API directly. It also
// checks the reconciliation marker actually reaches the outgoing body.
func TestExecuteGitHubReply_SendsPullRequestNumberAndEmbedsMarker(t *testing.T) {
	remote := &fakeGitHubWriteProvider{
		op:     "github.replyToReviewComment",
		result: mustMarshal(t, map[string]any{"normalized": map[string]any{"id": "c-2"}}),
	}
	gate := adapters.NewGate("github", githubWriteManifest("github.replyToReviewComment"), remote)
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "comment_id": "c-1", "body": "Fixed"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", Operation: "github.replyToReviewComment", PayloadJSON: string(payload)}

	externalID, _, err := executeGitHubReply(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("executeGitHubReply: %v", err)
	}
	if externalID != "c-2" {
		t.Fatalf("externalID = %q, want c-2", externalID)
	}
	if remote.params["pullRequestNumber"] != float64(42) {
		t.Fatalf("pullRequestNumber = %v, want 42 to have been sent", remote.params["pullRequestNumber"])
	}
	body, _ := remote.params["body"].(string)
	if !strings.Contains(body, githubReplyMarker(intent.ID)) {
		t.Fatalf("body = %q, want it to contain the reconciliation marker", body)
	}
}

func TestExecuteGitHubReview_EmbedsMarkerInBody(t *testing.T) {
	remote := &fakeGitHubWriteProvider{
		op:     "github.createPullRequestReview",
		result: mustMarshal(t, map[string]any{"ok": true, "reviewId": "rev-1"}),
	}
	gate := adapters.NewGate("github", githubWriteManifest("github.createPullRequestReview"), remote)
	payload, _ := json.Marshal(map[string]any{"pull_request_number": 42, "body": "Looks good", "event": "APPROVE", "head_sha": "abc123"})
	intent := outbox.Intent{ID: "intent-1", TargetKey: "acme/widgets", Operation: "github.createPullRequestReview", PayloadJSON: string(payload)}

	externalID, _, err := executeGitHubReview(context.Background(), gate, intent)
	if err != nil {
		t.Fatalf("executeGitHubReview: %v", err)
	}
	if externalID != "rev-1" {
		t.Fatalf("externalID = %q, want rev-1", externalID)
	}
	body, _ := remote.params["body"].(string)
	if !strings.Contains(body, githubReviewMarker(intent.ID)) {
		t.Fatalf("body = %q, want it to contain the reconciliation marker", body)
	}
}
