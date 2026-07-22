package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/prreview"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestReviewPrHandlerRefusesWithoutExplicitTrigger(t *testing.T) {
	a := &app.App{}
	_, _, err := reviewPrHandler(a)(context.Background(), nil, ReviewPrInput{RunId: "run-1", RepoId: "repo-1", PullRequestNumber: 42})
	if err == nil {
		t.Fatal("expected an error when explicit_trigger is false")
	}
}

func TestFetchPrContextAssemblesBundle(t *testing.T) {
	gate, fc := newCreatePrTestGate(t)
	fc.responses = map[string]string{
		"github.getPullRequest": `{"normalized":{
			"number":42,"title":"Fix refund rounding","body":"Fixes it.","state":"open",
			"draft":false,"merged":false,"baseRef":"main","headRef":"punakawan/fix-refund",
			"headSha":"abc123","author":"petruk","url":"https://github.com/acme/widgets/pull/42"
		}}`,
		"github.getPullRequestFiles": `{"normalized":[
			{"path":"src/refund.ts","status":"modified","additions":5,"deletions":1,"changes":6,"patch":"@@ ... @@"}
		]}`,
		"github.getPullRequestChecks": `{"normalized":[
			{"name":"ci/test","status":"completed","conclusion":"success"}
		]}`,
		"github.listPullRequestComments": `{"normalized":[
			{"id":"c1","kind":"review","author":"gareng","body":"nit: rename this","path":"src/refund.ts","line":10}
		]}`,
	}

	out, err := fetchPrContext(context.Background(), nil, gate, "acme/widgets", "run-1", 42, true, protocol.ApprovalRecordRequestedByPetruk)
	if err != nil {
		t.Fatalf("fetchPrContext: %v", err)
	}
	if out.PullRequest.Number != 42 || out.PullRequest.HeadSha != "abc123" {
		t.Fatalf("PullRequest = %+v, want number=42 headSha=abc123", out.PullRequest)
	}
	if len(out.Files) != 1 || out.Files[0].Path != "src/refund.ts" {
		t.Fatalf("Files = %+v, want one file src/refund.ts", out.Files)
	}
	if len(out.Checks) != 1 || out.Checks[0].Name != "ci/test" {
		t.Fatalf("Checks = %+v, want one check ci/test", out.Checks)
	}
	if len(out.Comments) != 1 || out.Comments[0].Id != "c1" {
		t.Fatalf("Comments = %+v, want one comment c1", out.Comments)
	}

	var sawChecksRef bool
	for _, c := range fc.calls {
		if c["op"] == "github.getPullRequestChecks" {
			if c["ref"] != "abc123" {
				t.Errorf("getPullRequestChecks ref = %v, want the PR's headSha abc123", c["ref"])
			}
			sawChecksRef = true
		}
	}
	if !sawChecksRef {
		t.Fatal("expected a github.getPullRequestChecks call")
	}
}

func TestFetchPrContextOmitsCommentsWhenNotRequested(t *testing.T) {
	gate, fc := newCreatePrTestGate(t)
	fc.responses = map[string]string{
		"github.getPullRequest": `{"normalized":{"number":42,"headSha":"abc123"}}`,
	}

	out, err := fetchPrContext(context.Background(), nil, gate, "acme/widgets", "run-1", 42, false, protocol.ApprovalRecordRequestedByPetruk)
	if err != nil {
		t.Fatalf("fetchPrContext: %v", err)
	}
	if out.Comments != nil {
		t.Fatalf("Comments = %+v, want nil when include_existing_comments is false", out.Comments)
	}
	for _, c := range fc.calls {
		if c["op"] == "github.listPullRequestComments" {
			t.Fatal("did not expect a listPullRequestComments call when comments were not requested")
		}
	}
}

func TestSubmitPrReviewFindingsHandlerPersistsAndReturnsFindings(t *testing.T) {
	store, err := prreview.OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("prreview.OpenStore: %v", err)
	}
	a := &app.App{PrReviews: store}

	in := SubmitPrReviewFindingsInput{
		RunId:             "run-1",
		RepoId:            "repo-1",
		PullRequestNumber: 42,
		Findings: []protocol.ReviewFinding{
			{
				Id: "f1", Severity: protocol.ReviewFindingSeverityMajor, Category: "correctness",
				Title: "Off-by-one", Explanation: "loop bound wrong",
				Confidence: 0.8,
			},
		},
	}
	_, out, err := submitPrReviewFindingsHandler(a)(context.Background(), nil, in)
	if err != nil {
		t.Fatalf("submitPrReviewFindingsHandler: %v", err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Id != "f1" {
		t.Fatalf("out.Findings = %+v, want one finding f1", out.Findings)
	}

	recs, err := store.ForPullRequest("repo-1", 42)
	if err != nil {
		t.Fatalf("ForPullRequest: %v", err)
	}
	if len(recs) != 1 || len(recs[0].Findings) != 1 {
		t.Fatalf("recs = %+v, want one persisted record with one finding", recs)
	}
}

func TestSubmitPrReviewFindingsHandlerNormalizesNilFindings(t *testing.T) {
	store, err := prreview.OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("prreview.OpenStore: %v", err)
	}
	a := &app.App{PrReviews: store}

	_, out, err := submitPrReviewFindingsHandler(a)(context.Background(), nil, SubmitPrReviewFindingsInput{RunId: "run-1", RepoId: "repo-1", PullRequestNumber: 42})
	if err != nil {
		t.Fatalf("submitPrReviewFindingsHandler: %v", err)
	}
	if out.Findings == nil {
		t.Fatal("out.Findings = nil, want an empty slice")
	}
}
