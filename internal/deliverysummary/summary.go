// Package deliverysummary computes Summary: the deterministic, factual
// block (test counts, commits, evidence, risks, links) a PR body, a Jira
// comment, a record_work_outcome summary, and a panel session summary all
// render identically, without any caller restating those facts in prose.
//
// This is deliberately not internal/delivery: that package is the
// DeliveryLane-based orchestration control plane, with its own leases,
// idempotency keys, and event-sourced
// VerificationMatrix/ReviewConclusion/TestReportSummary builders for
// lanes it owns end to end. This package instead serves the older,
// still-live ad hoc MCP surface (create_pr, update_jira_task_progress,
// record_work_outcome, and the panel's session summary) that a run
// identified only by a run id passes through today - it has no
// DeliveryLane to compute a matrix over, only whatever run-scoped
// evidence/test/review/git records already exist.
//
// Unlike internal/dossier's ChangeDossier, nothing here is populated by a
// caller filling in an MCP "ceremony" tool (add_dossier_claim,
// set_dossier_impact, ...) - that MCP ceremony surface has since been
// removed precisely because it asked a caller to restate facts a store
// already held. Every field here is instead read straight from records ordinary
// tool use already produced: internal/testrun's evidence bundle,
// internal/evidence's per-run ledger, internal/prreview's PR review
// findings, and git itself via internal/gitops. Build is a pure function
// over that data (mirroring internal/panel/sessionsummary.Build's own
// contract); Gather (gather.go) is the I/O half that assembles Input from
// a run id.
package deliverysummary

import (
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/testrun"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Input is the canonical, already-persisted data Build renders into a
// Summary.
type Input struct {
	RunId string

	// TestReports is every testrun.Report gathered for this run (run_tests
	// may be called more than once, e.g. compile then targeted tests).
	// Counts are commands, not individual test cases: Go has no general way
	// to parse arbitrary test-runner output into per-test results (see
	// internal/testrun's package doc), so a command is the smallest unit
	// this package can honestly count.
	TestReports []testrun.Report

	// Evidence is every EvidenceRecord this run's ledger has accumulated.
	Evidence []protocol.EvidenceRecord

	// Commits is this change's commit history (e.g. a PR's base..head
	// range), most-recent first.
	Commits []gitops.Commit

	// Risks is every ReviewFinding from this run's PR reviews
	// (internal/prreview) - the canonical signal for what a review found.
	// Only blocker/major severity findings surface as risks; minor findings
	// and suggestions are review feedback, not delivery risk.
	Risks []protocol.ReviewFinding

	PrUrl   string
	JiraUrl string
}

// Summary is the deterministic factual block Build produces from an Input.
type Summary struct {
	RunId string

	CommandsRun    int
	CommandsPassed int
	CommandsFailed int

	// TotalDurationMs sums every recorded command's DurationMs, passed or
	// failed - all of it is time actually spent running verification, per
	// internal/worklogalloc's proposed-worklog derivation, which needs a
	// single honest "verified work" duration rather than a fabricated
	// per-stage breakdown testrun's data does not support (see
	// internal/testrun's own doc: a command is the smallest unit it can
	// honestly attribute time to, with no build/test/review distinction).
	TotalDurationMs int64

	Commits       []CommitLine
	EvidenceCount int
	Risks         []RiskLine

	PrUrl   string
	JiraUrl string
}

// CommitLine is one commit as rendered in a Summary.
type CommitLine struct {
	SHA     string
	Subject string
}

// RiskLine is one at-risk finding as rendered in a Summary.
type RiskLine struct {
	Severity string
	Summary  string
	Location string
}

// riskSeverities are the ReviewFindingSeverity values counted as a delivery
// risk; minor findings and suggestions are ordinary review feedback, not
// something a PR/Jira/outcome reader needs surfaced as a risk.
var riskSeverities = map[protocol.ReviewFindingSeverity]bool{
	protocol.ReviewFindingSeverityBlocker: true,
	protocol.ReviewFindingSeverityMajor:   true,
}

// Build computes a Summary from in. It performs no I/O - callers gather
// TestReports/Evidence/Commits/Risks from their respective stores (directly,
// or via Gather) and pass them in, keeping this function pure and trivially
// testable, mirroring internal/panel/sessionsummary.Build's own contract.
func Build(in Input) Summary {
	s := Summary{
		RunId:         in.RunId,
		EvidenceCount: len(in.Evidence),
		PrUrl:         in.PrUrl,
		JiraUrl:       in.JiraUrl,
	}

	for _, report := range in.TestReports {
		for _, res := range report.Results {
			s.CommandsRun++
			if res.ExitCode == 0 {
				s.CommandsPassed++
			} else {
				s.CommandsFailed++
			}
			s.TotalDurationMs += res.DurationMs
		}
	}

	for _, c := range in.Commits {
		s.Commits = append(s.Commits, CommitLine{SHA: shortSHA(c.SHA), Subject: c.Subject})
	}

	for _, f := range in.Risks {
		if !riskSeverities[f.Severity] {
			continue
		}
		loc := ""
		if f.File != nil {
			loc = *f.File
		}
		s.Risks = append(s.Risks, RiskLine{Severity: string(f.Severity), Summary: f.Explanation, Location: loc})
	}

	return s
}

// shortSHA returns sha's first 12 characters (git's own default abbreviation
// length is 7, but 12 stays unambiguous across larger repositories while
// still being far shorter than a full 40-char SHA).
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// VerifiedHours converts TotalDurationMs to hours - the single input
// internal/worklogalloc.Allocate derives a proposed dev/test/review
// worklog split from. It is deliberately named "verified" rather than
// e.g. "worked" hours: it measures time spent
// running commands whose outcome is recorded (pass or fail), not a
// human's actual time-on-task, which nothing in this data captures.
func (s Summary) VerifiedHours() float64 {
	return float64(s.TotalDurationMs) / 1000 / 3600
}

// HasContent reports whether s carries anything worth rendering: an empty
// Summary (no evidence, tests, commits, risks, or links) means canonical
// data has not accumulated for this run yet, not that everything passed
// with zero findings - rendering "0 / 0" indicators in that case would
// misstate silence as a clean result, so callers check this before
// appending Section to a caller-authored body.
func (s Summary) HasContent() bool {
	return s.CommandsRun > 0 || s.EvidenceCount > 0 || len(s.Commits) > 0 || len(s.Risks) > 0 || s.PrUrl != "" || s.JiraUrl != ""
}

// Indicators renders the at-a-glance counters, mirroring
// internal/dossier's summaryIndicators shape for the same class of output.
func (s Summary) Indicators() []string {
	return []string{
		fmt.Sprintf("Commands run: %d / %d passed", s.CommandsPassed, s.CommandsRun),
		fmt.Sprintf("Commits: %d", len(s.Commits)),
		fmt.Sprintf("Evidence records: %d", s.EvidenceCount),
		fmt.Sprintf("Risks: %d", len(s.Risks)),
	}
}

// Section renders the canonical block as a markdown section: verification
// counts, commits, risks, and links, or "" if s has nothing to say
// (HasContent is false). heading is the markdown heading marker to use
// ("##" for a PR body, "###" for a tighter-nested Jira comment or outcome
// summary).
func (s Summary) Section(heading string) string {
	if !s.HasContent() {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s Verification (canonical)\n\n", heading)
	for _, line := range s.Indicators() {
		fmt.Fprintf(&b, "- %s\n", line)
	}

	if len(s.Commits) > 0 {
		fmt.Fprintf(&b, "\n%s Commits\n\n", heading)
		for _, c := range s.Commits {
			fmt.Fprintf(&b, "- %s %s\n", c.SHA, c.Subject)
		}
	}

	if len(s.Risks) > 0 {
		fmt.Fprintf(&b, "\n%s Known risks (canonical)\n\n", heading)
		for _, r := range s.Risks {
			if r.Location != "" {
				fmt.Fprintf(&b, "- [%s] %s (%s)\n", r.Severity, r.Summary, r.Location)
			} else {
				fmt.Fprintf(&b, "- [%s] %s\n", r.Severity, r.Summary)
			}
		}
	}

	if s.PrUrl != "" || s.JiraUrl != "" {
		fmt.Fprintf(&b, "\n%s Links\n\n", heading)
		if s.PrUrl != "" {
			fmt.Fprintf(&b, "- Pull request: %s\n", s.PrUrl)
		}
		if s.JiraUrl != "" {
			fmt.Fprintf(&b, "- Jira: %s\n", s.JiraUrl)
		}
	}

	return b.String()
}
