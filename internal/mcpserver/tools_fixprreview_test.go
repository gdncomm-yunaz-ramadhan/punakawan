package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestFetchUnresolvedPrCommentsHandlerRefusesWithoutExplicitTrigger(t *testing.T) {
	_, _, err := fetchUnresolvedPrCommentsHandler(nil)(context.Background(), nil, FetchUnresolvedPrCommentsInput{RunId: "run-1", RepoId: "repo-1", PullRequestNumber: 42})
	if err == nil {
		t.Fatal("expected an error when explicit_trigger is false")
	}
}

func TestResolveReviewThreadHandlerRefusesWithoutAllow(t *testing.T) {
	_, out, err := resolveReviewThreadHandler(nil)(context.Background(), nil, ResolveReviewThreadInput{RunId: "run-1", ThreadId: "thread-1", Allow: false})
	if err != nil {
		t.Fatalf("resolveReviewThreadHandler: %v", err)
	}
	if out.Resolved {
		t.Fatal("Resolved = true, want false when allow is false")
	}
	if out.Reason == "" {
		t.Fatal("expected a reason explaining why the thread was not resolved")
	}
}

func TestFetchUnresolvedPrCommentsFetchesThreadsViaGate(t *testing.T) {
	gate, fc := newCreatePrTestGate(t)
	fc.responses = map[string]string{
		"github.listUnresolvedReviewThreads": `{"normalized":[
			{"id":"thread-1","comments":[{"id":"c1","kind":"review","author":"gareng","body":"please fix"}]}
		]}`,
	}

	out, err := fetchUnresolvedPrCommentsViaGate(context.Background(), nil, gate, "acme/widgets", "run-1", 42, "petruk")
	if err != nil {
		t.Fatalf("fetchUnresolvedPrCommentsViaGate: %v", err)
	}
	if len(out.Threads) != 1 || out.Threads[0].Id != "thread-1" || len(out.Threads[0].Comments) != 1 {
		t.Fatalf("out = %+v, want one thread with one comment", out)
	}

	var sawCall bool
	for _, c := range fc.calls {
		if c["op"] == "github.listUnresolvedReviewThreads" {
			sawCall = true
			if c["repository"] != "acme/widgets" || c["pullRequestNumber"] != 42 {
				t.Errorf("call params = %+v, want repository=acme/widgets pullRequestNumber=42", c)
			}
		}
	}
	if !sawCall {
		t.Fatal("expected a github.listUnresolvedReviewThreads call")
	}
}

func TestResolveReviewThreadHandlerResolvesWhenAllowed(t *testing.T) {
	gate, fc := newCreatePrTestGate(t)
	if _, err := gate.RequestApproval("run-1", "github.resolveReviewThread", protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := gate.Approve("run-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	fc.responses = map[string]string{
		"github.resolveReviewThread": `{"normalized":{"resolved":true}}`,
	}

	out, err := resolveReviewThreadViaGate(context.Background(), nil, gate, "run-1", "thread-1", "petruk")
	if err != nil {
		t.Fatalf("resolveReviewThreadViaGate: %v", err)
	}
	if !out.Resolved {
		t.Fatal("Resolved = false, want true once approved and allowed")
	}

	var sawCall bool
	for _, c := range fc.calls {
		if c["op"] == "github.resolveReviewThread" {
			sawCall = true
			if c["threadId"] != "thread-1" {
				t.Errorf("threadId = %v, want thread-1", c["threadId"])
			}
		}
	}
	if !sawCall {
		t.Fatal("expected a github.resolveReviewThread call")
	}
}
