package deliverysummary

import (
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/testrun"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func sampleInput() Input {
	return Input{
		RunId: "run-1",
		TestReports: []testrun.Report{
			{
				AllPassed: false,
				Results: []testrun.CommandResult{
					{Command: testrun.Command{Name: "go", Args: []string{"build", "./..."}}, ExitCode: 0, DurationMs: 3_600_000},
					{Command: testrun.Command{Name: "go", Args: []string{"test", "./..."}}, ExitCode: 1, DurationMs: 1_800_000},
				},
			},
		},
		Evidence: []protocol.EvidenceRecord{
			{Id: "ev-1", RunId: "run-1", Type: protocol.EvidenceRecordTypeTestReport},
			{Id: "ev-2", RunId: "run-1", Type: protocol.EvidenceRecordTypeGitDiff},
		},
		Commits: []gitops.Commit{
			{SHA: "abcdef0123456789", Subject: "fix refund rounding"},
		},
		Risks: []protocol.ReviewFinding{
			{Id: "f1", Severity: protocol.ReviewFindingSeverityBlocker, Explanation: "unchecked error", File: strPtr("refund.go")},
			{Id: "f2", Severity: protocol.ReviewFindingSeverityMinor, Explanation: "naming nit"},
		},
		PrUrl:   "https://github.com/acme/widgets/pull/43",
		JiraUrl: "https://acme.atlassian.net/browse/PAY-1",
	}
}

func strPtr(s string) *string { return &s }

// TestBuildProducesSameCountsAcrossRenderers is the AC1 test: a PR body, a
// Jira comment, a record_work_outcome summary, and a panel session summary
// all read from one Summary, so they cannot drift apart the way four
// independently-typed caller narratives could.
func TestBuildProducesSameCountsAcrossRenderers(t *testing.T) {
	summary := Build(sampleInput())

	if summary.CommandsRun != 2 || summary.CommandsPassed != 1 || summary.CommandsFailed != 1 {
		t.Fatalf("summary = %+v, want CommandsRun=2 CommandsPassed=1 CommandsFailed=1", summary)
	}
	if summary.TotalDurationMs != 5_400_000 {
		t.Fatalf("TotalDurationMs = %d, want 5400000 (sum of both commands, passed and failed alike)", summary.TotalDurationMs)
	}
	if got, want := summary.VerifiedHours(), 1.5; got != want {
		t.Fatalf("VerifiedHours() = %v, want %v", got, want)
	}
	if summary.EvidenceCount != 2 {
		t.Fatalf("EvidenceCount = %d, want 2", summary.EvidenceCount)
	}
	if len(summary.Commits) != 1 || summary.Commits[0].Subject != "fix refund rounding" {
		t.Fatalf("Commits = %+v, want one commit 'fix refund rounding'", summary.Commits)
	}
	// Only the blocker finding counts as a risk; the minor finding is
	// ordinary review feedback, not a delivery risk.
	if len(summary.Risks) != 1 || summary.Risks[0].Severity != "blocker" {
		t.Fatalf("Risks = %+v, want exactly the blocker finding", summary.Risks)
	}

	prBody := "## Summary\n\nFixes rounding.\n\n" + summary.Section("##")
	jiraComment := "Implementation done.\n\n" + summary.Section("###")
	outcomeSummary := "Completed the task.\n\n" + summary.Section("###")

	for name, rendered := range map[string]string{
		"pr body":         prBody,
		"jira comment":    jiraComment,
		"outcome summary": outcomeSummary,
	} {
		if !strings.Contains(rendered, "Commands run: 1 / 2 passed") {
			t.Errorf("%s missing canonical command counts: %s", name, rendered)
		}
		if !strings.Contains(rendered, "Commits: 1") {
			t.Errorf("%s missing canonical commit count: %s", name, rendered)
		}
		if !strings.Contains(rendered, "Evidence records: 2") {
			t.Errorf("%s missing canonical evidence count: %s", name, rendered)
		}
		if !strings.Contains(rendered, "Risks: 1") {
			t.Errorf("%s missing canonical risk count: %s", name, rendered)
		}
		if !strings.Contains(rendered, "fix refund rounding") {
			t.Errorf("%s missing canonical commit subject: %s", name, rendered)
		}
		if !strings.Contains(rendered, "unchecked error") {
			t.Errorf("%s missing canonical risk detail: %s", name, rendered)
		}
		if !strings.Contains(rendered, "https://github.com/acme/widgets/pull/43") {
			t.Errorf("%s missing canonical PR URL: %s", name, rendered)
		}
		if !strings.Contains(rendered, "https://acme.atlassian.net/browse/PAY-1") {
			t.Errorf("%s missing canonical Jira URL: %s", name, rendered)
		}
	}

	// A panel session summary has no markdown body, but it consumes the
	// same underlying counts the three markdown renderers above rendered as
	// text - proving there is one source of truth, not four.
	if summary.EvidenceCount != 2 || summary.CommandsFailed != 1 || len(summary.Risks) != 1 {
		t.Fatalf("panel-consumable counts diverged: EvidenceCount=%d CommandsFailed=%d Risks=%d",
			summary.EvidenceCount, summary.CommandsFailed, len(summary.Risks))
	}
}

func TestSectionEmptyWhenSummaryHasNoContent(t *testing.T) {
	summary := Build(Input{RunId: "run-empty"})
	if summary.HasContent() {
		t.Fatal("HasContent() = true, want false for an empty Input")
	}
	if got := summary.Section("##"); got != "" {
		t.Fatalf("Section() = %q, want empty so callers do not append noise to a narrative body", got)
	}
}

func TestBuildOnlyBlockerAndMajorFindingsCountAsRisks(t *testing.T) {
	summary := Build(Input{
		RunId: "run-1",
		Risks: []protocol.ReviewFinding{
			{Id: "f1", Severity: protocol.ReviewFindingSeverityBlocker, Explanation: "a"},
			{Id: "f2", Severity: protocol.ReviewFindingSeverityMajor, Explanation: "b"},
			{Id: "f3", Severity: protocol.ReviewFindingSeverityMinor, Explanation: "c"},
			{Id: "f4", Severity: protocol.ReviewFindingSeveritySuggestion, Explanation: "d"},
		},
	})
	if len(summary.Risks) != 2 {
		t.Fatalf("Risks = %+v, want exactly the blocker and major findings", summary.Risks)
	}
}
