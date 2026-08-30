package githubintegration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

var permissiveInputSchema = protocol.AdapterManifestOperationsValueInputSchema{"type": "object"}

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
			"github.getPullRequest":              {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.getPullRequestFiles":         {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.listPullRequestComments":     {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.listUnresolvedReviewThreads": {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.getPullRequestChecks":        {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.getCommitStatus":             {SideEffect: false, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.createPullRequest":           {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
			"github.createPullRequestReview":     {SideEffect: true, Description: "test fixture operation", InputSchema: permissiveInputSchema},
		},
	}
}

// fakeGitHubCaller answers a fixed set of ops with fixed responses,
// recording every call made so a test can assert on it.
type fakeGitHubCaller struct {
	calls     []map[string]any
	responses map[string]string
	failOps   map[string]bool
}

func (f *fakeGitHubCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	f.calls = append(f.calls, args)
	op, _ := args["op"].(string)
	if f.failOps[op] {
		return nil, fmt.Errorf("simulated adapter failure for %q", op)
	}
	if resp, ok := f.responses[op]; ok {
		return json.RawMessage(resp), nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}

type fakeGateResolver struct{ gate *adapters.Gate }

func (f fakeGateResolver) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	return f.gate, nil
}

func newTestService(t *testing.T, fc *fakeGitHubCaller) (*Service, *outbox.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	gate := adapters.NewGate("github", githubManifest(), fc)
	ob := outbox.New(db)
	return NewService(fakeGateResolver{gate: gate}, ob), ob
}

func TestHydratePullRequest_AggregatesAllSections(t *testing.T) {
	fc := &fakeGitHubCaller{responses: map[string]string{
		"github.getPullRequest":              `{"normalized":{"number":42,"headSha":"abc123"}}`,
		"github.getPullRequestFiles":         `{"normalized":[{"path":"a.go"}],"page":{"returned":1,"complete":true,"pages":1}}`,
		"github.listPullRequestComments":     `{"normalized":[],"page":{"returned":0,"complete":true,"pages":1}}`,
		"github.listUnresolvedReviewThreads": `{"normalized":[],"page":{"returned":0,"complete":true,"pages":1}}`,
		"github.getPullRequestChecks":        `{"normalized":[{"name":"build"}],"page":{"returned":1,"complete":true,"pages":1}}`,
		"github.getCommitStatus":             `{"normalized":{"state":"success","statuses":[]}}`,
	}}
	svc, _ := newTestService(t, fc)

	out, err := svc.HydratePullRequest(context.Background(), "run-1", "acme/widgets", 42)
	if err != nil {
		t.Fatalf("HydratePullRequest: %v", err)
	}
	for _, key := range []string{"pull_request", "files", "comments", "unresolved_threads", "checks", "legacy_commit_status"} {
		if _, ok := out[key]; !ok {
			t.Errorf("result missing key %q: %+v", key, out)
		}
	}
	if complete, _ := out["complete"].(bool); !complete {
		t.Errorf("complete = %v, want true when every page reported complete", out["complete"])
	}
}

func TestHydratePullRequest_IncompleteWhenAnyPageIsTruncated(t *testing.T) {
	fc := &fakeGitHubCaller{responses: map[string]string{
		"github.getPullRequest":              `{"normalized":{"number":42,"headSha":"abc123"}}`,
		"github.getPullRequestFiles":         `{"normalized":[],"page":{"returned":1000,"complete":false,"pages":10,"truncated_reason":"hard_limit"}}`,
		"github.listPullRequestComments":     `{"normalized":[],"page":{"returned":0,"complete":true,"pages":1}}`,
		"github.listUnresolvedReviewThreads": `{"normalized":[],"page":{"returned":0,"complete":true,"pages":1}}`,
		"github.getPullRequestChecks":        `{"normalized":[],"page":{"returned":0,"complete":true,"pages":1}}`,
		"github.getCommitStatus":             `{"normalized":{"state":"success","statuses":[]}}`,
	}}
	svc, _ := newTestService(t, fc)

	out, err := svc.HydratePullRequest(context.Background(), "run-1", "acme/widgets", 42)
	if err != nil {
		t.Fatalf("HydratePullRequest: %v", err)
	}
	if complete, _ := out["complete"].(bool); complete {
		t.Error("complete = true, want false when a page was truncated")
	}
}

func TestSubmitReview_SucceedsWhenHeadMatches(t *testing.T) {
	fc := &fakeGitHubCaller{responses: map[string]string{
		"github.getPullRequest":          `{"normalized":{"number":42,"headSha":"abc123"}}`,
		"github.createPullRequestReview": `{"ok":true,"reviewId":"701"}`,
	}}
	svc, _ := newTestService(t, fc)

	externalID, err := svc.SubmitReview(context.Background(), SubmitReviewRequest{
		RunID: "run-1", Repository: "acme/widgets", PullRequestNumber: 42,
		HeadSHA: "abc123", Body: "Looks good", Event: "APPROVE", ReviewID: "review-1",
	})
	if err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}
	if externalID != "701" {
		t.Fatalf("externalID = %q, want 701", externalID)
	}
}

// TestSubmitReview_StaleProposalBlocksSubmission guards the exact scenario
// Step 7 requires: the pull request's head has moved since the review was
// proposed, so no review POST should ever be sent.
func TestSubmitReview_StaleProposalBlocksSubmission(t *testing.T) {
	fc := &fakeGitHubCaller{responses: map[string]string{
		"github.getPullRequest":          `{"normalized":{"number":42,"headSha":"def456"}}`,
		"github.createPullRequestReview": `{"ok":true,"reviewId":"701"}`,
	}}
	svc, _ := newTestService(t, fc)

	_, err := svc.SubmitReview(context.Background(), SubmitReviewRequest{
		RunID: "run-1", Repository: "acme/widgets", PullRequestNumber: 42,
		HeadSHA: "abc123", Body: "Looks good", Event: "APPROVE", ReviewID: "review-1",
	})
	if err == nil {
		t.Fatal("expected an error when the proposal's head SHA is stale")
	}
	var stale *ErrReviewProposalStale
	if !errors.As(err, &stale) {
		t.Fatalf("error = %v, want *ErrReviewProposalStale", err)
	}
	if stale.ProposedHeadSHA != "abc123" || stale.CurrentHeadSHA != "def456" {
		t.Errorf("stale = %+v, want proposed=abc123 current=def456", stale)
	}
	for _, c := range fc.calls {
		if c["op"] == "github.createPullRequestReview" {
			t.Fatal("expected no review submission call when the proposal is stale")
		}
	}
}

func TestCreatePullRequest_ReturnsNumberAndURL(t *testing.T) {
	fc := &fakeGitHubCaller{responses: map[string]string{
		"github.createPullRequest": `{"normalized":{"number":42,"url":"https://github.com/acme/widgets/pull/42"}}`,
	}}
	svc, _ := newTestService(t, fc)

	number, url, err := svc.CreatePullRequest(context.Background(), CreatePullRequestRequest{
		RunID: "run-1", Repository: "acme/widgets", BaseBranch: "main", HeadBranch: "feature-x",
		Title: "Fix rounding", Body: "Fixes the bug.",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if number != 42 || url != "https://github.com/acme/widgets/pull/42" {
		t.Fatalf("number=%d url=%q, want 42 and the pull request URL", number, url)
	}
}

func TestCreatePullRequest_SurfacesAdapterFailure(t *testing.T) {
	fc := &fakeGitHubCaller{failOps: map[string]bool{"github.createPullRequest": true}}
	svc, _ := newTestService(t, fc)

	if _, _, err := svc.CreatePullRequest(context.Background(), CreatePullRequestRequest{
		RunID: "run-1", Repository: "acme/widgets", BaseBranch: "main", HeadBranch: "feature-x",
		Title: "Fix rounding", Body: "Fixes the bug.",
	}); err == nil {
		t.Fatal("expected an error when the adapter call fails")
	}
}
