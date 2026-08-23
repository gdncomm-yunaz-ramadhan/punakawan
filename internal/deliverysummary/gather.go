package deliverysummary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ygrip/punakawan/internal/evidence"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/testrun"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// GatherInput bundles what Gather needs to assemble an Input for one run, so
// call sites do not need to know the evidence ledger's layout or how a
// run_tests report is persisted.
type GatherInput struct {
	WorkspaceRoot string
	RunId         string

	// Inspector/RepoPath/BaseBranch/HeadBranch, when all set, are used to
	// look up this change's commit range via git log. Commits are left
	// empty (not an error) when any is missing - a caller without a single
	// repository/branch pair in view (e.g. a workflow-level, possibly
	// multi-repo run) simply has no range to compute.
	Inspector  *gitops.Inspector
	RepoPath   string
	BaseBranch string
	HeadBranch string

	// Risks is passed through unchanged onto the resulting Input: which PR
	// review findings apply to this run is the caller's own lookup
	// (internal/prreview.Store.ForRun), not something Gather can derive
	// from a run id and workspace root alone.
	Risks []protocol.ReviewFinding

	PrUrl   string
	JiraUrl string
}

// Gather reads this run's evidence ledger (and, for every test-report
// entry, the tests.json it points to) and, when a repository/branch range
// is given, its commit history, and assembles them into an Input for Build.
//
// A failure to read one test-report file is not fatal to the whole call:
// that evidence record still counts toward Evidence, it simply does not
// contribute to the test counts, matching this package's
// silence-is-not-success stance (see Summary.HasContent) applied the other
// way - a record that exists but cannot be decoded is not silently dropped
// from the count either.
func Gather(ctx context.Context, in GatherInput) (Input, error) {
	out := Input{RunId: in.RunId, Risks: in.Risks, PrUrl: in.PrUrl, JiraUrl: in.JiraUrl}

	ledger, err := evidence.OpenLedger(in.WorkspaceRoot, in.RunId)
	if err != nil {
		return Input{}, fmt.Errorf("delivery: open evidence ledger for %q: %w", in.RunId, err)
	}
	records, err := ledger.List()
	if err != nil {
		return Input{}, fmt.Errorf("delivery: list evidence for %q: %w", in.RunId, err)
	}
	out.Evidence = records

	for _, rec := range records {
		if rec.Type != protocol.EvidenceRecordTypeTestReport || rec.Path == nil {
			continue
		}
		data, err := os.ReadFile(*rec.Path)
		if err != nil {
			continue
		}
		var report testrun.Report
		if err := json.Unmarshal(data, &report); err != nil {
			continue
		}
		out.TestReports = append(out.TestReports, report)
	}

	if in.Inspector != nil && in.RepoPath != "" && in.BaseBranch != "" && in.HeadBranch != "" {
		if commits, err := in.Inspector.LogRange(ctx, in.RepoPath, in.BaseBranch, in.HeadBranch); err == nil {
			out.Commits = commits
		}
	}

	return out, nil
}

// URLsFromRefs scans refs (e.g. a WorkflowRunOutcome's OutputRefs) for a
// pull request URL and a Jira issue URL, so a caller that already recorded
// those refs for an unrelated reason does not have to be given a second,
// dedicated field just to carry the same URL again.
func URLsFromRefs(refs []string) (prURL, jiraURL string) {
	for _, ref := range refs {
		switch {
		case prURL == "" && strings.Contains(ref, "/pull/"):
			prURL = ref
		case jiraURL == "" && strings.Contains(ref, "/browse/"):
			jiraURL = ref
		}
	}
	return prURL, jiraURL
}
