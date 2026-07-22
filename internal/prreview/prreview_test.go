package prreview

import (
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestAppendAndListRoundTrip(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	rec := Record{
		RunId: "run-1", RepoId: "repo-1", PullRequestNumber: 42,
		Findings: []protocol.ReviewFinding{
			{Id: "f1", Severity: protocol.ReviewFindingSeverityMinor, Category: "style", Title: "nit", Explanation: "e", Confidence: 0.5},
		},
	}
	if err := store.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	recs, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(recs) != 1 || recs[0].RunId != "run-1" || len(recs[0].Findings) != 1 {
		t.Fatalf("recs = %+v, want one record with one finding", recs)
	}
}

func TestForPullRequestFiltersByRepoAndNumber(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	must := func(rec Record) {
		if err := store.Append(rec); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	must(Record{RunId: "run-1", RepoId: "repo-1", PullRequestNumber: 42})
	must(Record{RunId: "run-2", RepoId: "repo-1", PullRequestNumber: 43})
	must(Record{RunId: "run-3", RepoId: "repo-2", PullRequestNumber: 42})

	recs, err := store.ForPullRequest("repo-1", 42)
	if err != nil {
		t.Fatalf("ForPullRequest: %v", err)
	}
	if len(recs) != 1 || recs[0].RunId != "run-1" {
		t.Fatalf("recs = %+v, want only run-1's record for repo-1 PR 42", recs)
	}
}

func TestMultipleReviewRoundsForSamePrAllPersist(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	for _, runID := range []string{"run-1", "run-2"} {
		if err := store.Append(Record{RunId: runID, RepoId: "repo-1", PullRequestNumber: 42}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	recs, err := store.ForPullRequest("repo-1", 42)
	if err != nil {
		t.Fatalf("ForPullRequest: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("recs = %+v, want both review rounds to persist independently (no fold-latest)", recs)
	}
}
