package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/deliverysummary"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func githubTestManifest() protocol.AdapterManifest {
	m := atlassianTestManifest()
	approval := approvalRequired()
	m.Id = "github"
	m.Operations = protocol.AdapterManifestOperations{
		"github.getPullRequest":              {SideEffect: false},
		"github.getPullRequestFiles":         {SideEffect: false},
		"github.getPullRequestChecks":        {SideEffect: false},
		"github.listPullRequestComments":     {SideEffect: false},
		"github.listUnresolvedReviewThreads": {SideEffect: false},
		"github.createPullRequest":           {SideEffect: true, Approval: approval},
		"github.addLabels":                   {SideEffect: true, Approval: approval},
		"github.requestReviewers":            {SideEffect: true, Approval: approval},
		"github.resolveReviewThread":         {SideEffect: true, Approval: approval},
	}
	return m
}

// fakeGitHubCaller mirrors fakeAtlassianCaller's pattern for a GitHub-shaped
// fake response set, so create_pr/review_pr/fix_pr_review's adapter-call
// logic can be exercised without spawning the real
// packages/github-adapter process.
type fakeGitHubCaller struct {
	calls     []map[string]any
	responses map[string]string // raw JSON per op name, defaulting to {"ok":true}
}

func (f *fakeGitHubCaller) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	args, _ := params.(map[string]any)
	f.calls = append(f.calls, args)
	if op, _ := args["op"].(string); op != "" {
		if resp, ok := f.responses[op]; ok {
			return json.RawMessage(resp), nil
		}
	}
	return json.RawMessage(`{"ok":true}`), nil
}

// fakeGateProvider implements adapterGateProvider, returning a fixed Gate
// for adapterID "github" and an error for anything else, mirroring what
// adapters.Registry.Gate does when an adapter id has no configured spec.
type fakeGateProvider struct {
	gate *adapters.Gate
}

func (f *fakeGateProvider) Gate(ctx context.Context, adapterID string) (*adapters.Gate, error) {
	if adapterID != "github" || f.gate == nil {
		return nil, fmt.Errorf("unknown adapter id %q", adapterID)
	}
	return f.gate, nil
}

func newCreatePrTestGate(t *testing.T) (*adapters.Gate, *fakeGitHubCaller) {
	t.Helper()
	store := newTestApprovalStore(t)
	fc := &fakeGitHubCaller{responses: map[string]string{
		"github.createPullRequest": `{"normalized":{"number":43,"url":"https://github.com/acme/widgets/pull/43"}}`,
	}}
	return adapters.NewGate("github", githubTestManifest(), fc, store), fc
}

func githubCapabilities(push bool) protocol.GitCapabilities {
	provider := protocol.GitCapabilitiesProviderGithub
	return protocol.GitCapabilities{
		Detected: true,
		Remotes:  []protocol.GitCapabilitiesRemotesElem{{Name: "origin", FetchUrl: "git@github.com:acme/widgets.git"}},
		Provider: &provider,
		Capabilities: protocol.GitCapabilitiesCapabilities{
			Push: push,
		},
		Limitations: []string{"push rejected"},
	}
}

func baseCreatePrInput() CreatePrInput {
	return CreatePrInput{
		RunId:        "run-1",
		BaseBranch:   "main",
		HeadBranch:   "punakawan/fix-refund",
		Title:        "Fix refund rounding",
		Summary:      "Fixes rounding.",
		Requirements: "REQ-1",
		Changes:      "src/refund.ts",
		Verification: "go test ./...",
		TaskIds:      []string{"bd-task-1"},
		RequestedBy:  "petruk",
	}
}

func TestCreatePrFromCapabilitiesSucceeds(t *testing.T) {
	gate, fc := newCreatePrTestGate(t)
	if _, err := gate.RequestApproval("run-1", "github.createPullRequest", protocol.ApprovalRecordRequestedByPetruk); err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if err := gate.Approve("run-1", "ygrip"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	out, err := createPrFromCapabilities(context.Background(), nil, githubCapabilities(true), &fakeGateProvider{gate: gate}, deliverysummary.Summary{}, baseCreatePrInput())
	if err != nil {
		t.Fatalf("createPrFromCapabilities: %v", err)
	}
	if !out.Created || out.PrNumber != 43 || out.PrUrl == "" {
		t.Fatalf("out = %+v, want Created=true PrNumber=43 with a URL", out)
	}

	var sawBody bool
	for _, c := range fc.calls {
		if c["op"] == "github.createPullRequest" {
			body, _ := c["body"].(string)
			sawBody = body != ""
			if c["repository"] != "acme/widgets" {
				t.Errorf("repository = %v, want acme/widgets (parsed from the SSH remote)", c["repository"])
			}
		}
	}
	if !sawBody {
		t.Fatal("expected the createPullRequest call to carry a non-empty templated body")
	}
}

func TestCreatePrFromCapabilitiesRejectsWithoutPushAccess(t *testing.T) {
	out, err := createPrFromCapabilities(context.Background(), nil, githubCapabilities(false), &fakeGateProvider{}, deliverysummary.Summary{}, baseCreatePrInput())
	if err != nil {
		t.Fatalf("createPrFromCapabilities: %v", err)
	}
	if out.Created {
		t.Fatal("Created = true, want false without push access")
	}
	if out.Reason == "" {
		t.Fatal("Reason is empty, want the push-access limitation")
	}
}

func TestCreatePrFromCapabilitiesReportsUnsupportedProvider(t *testing.T) {
	caps := githubCapabilities(true)
	caps.Provider = nil
	out, err := createPrFromCapabilities(context.Background(), nil, caps, &fakeGateProvider{}, deliverysummary.Summary{}, baseCreatePrInput())
	if err != nil {
		t.Fatalf("createPrFromCapabilities: %v", err)
	}
	if out.Created || out.Reason == "" {
		t.Fatalf("out = %+v, want Created=false with an unsupported-provider reason", out)
	}
}

func TestCreatePrFromCapabilitiesReportsNoAdapterConfigured(t *testing.T) {
	out, err := createPrFromCapabilities(context.Background(), nil, githubCapabilities(true), &fakeGateProvider{}, deliverysummary.Summary{}, baseCreatePrInput())
	if err != nil {
		t.Fatalf("createPrFromCapabilities: %v", err)
	}
	if out.Created {
		t.Fatal("Created = true, want false with no github adapter configured")
	}
}

func TestCreatePrFromCapabilitiesRejectsWithoutApproval(t *testing.T) {
	gate, _ := newCreatePrTestGate(t)
	out, err := createPrFromCapabilities(context.Background(), nil, githubCapabilities(true), &fakeGateProvider{gate: gate}, deliverysummary.Summary{}, baseCreatePrInput())
	if err != nil {
		t.Fatalf("createPrFromCapabilities: %v", err)
	}
	if out.Created {
		t.Fatal("Created = true, want false when createPullRequest was never approved")
	}
}

func TestUnavailableReasonChecksInOrder(t *testing.T) {
	if reason := unavailableReason(protocol.GitCapabilities{Detected: false}); reason == "" {
		t.Fatal("expected a reason when git was not detected")
	}
	if reason := unavailableReason(protocol.GitCapabilities{Detected: true}); reason == "" {
		t.Fatal("expected a reason when there are no remotes")
	}
}

func TestBuildPrBodyOmitsJiraSectionWhenEmpty(t *testing.T) {
	body := buildPrBody(baseCreatePrInput(), deliverysummary.Summary{})
	if strings.Contains(body, "## Jira references") {
		t.Fatal("expected the Jira references section to be omitted when JiraKeys is empty")
	}

	in := baseCreatePrInput()
	in.JiraKeys = []string{"PAY-1"}
	body = buildPrBody(in, deliverysummary.Summary{})
	if !strings.Contains(body, "## Jira references") || !strings.Contains(body, "PAY-1") {
		t.Fatal("expected the Jira references section to be present and list PAY-1 when JiraKeys is set")
	}
}

// TestBuildPrBodyRendersCanonicalSummaryWithoutCallerRestatingCounts proves
// that when a run has canonical test/commit/risk data, the PR body carries
// it verbatim from deliverysummary, not from anything the caller typed into
// Verification/KnownRisks.
func TestBuildPrBodyRendersCanonicalSummaryWithoutCallerRestatingCounts(t *testing.T) {
	summary := deliverysummary.Build(deliverysummary.Input{
		RunId: "run-1",
		Commits: []gitops.Commit{
			{SHA: "abc123def456", Subject: "fix refund rounding"},
		},
		Risks: []protocol.ReviewFinding{
			{Id: "f1", Severity: protocol.ReviewFindingSeverityBlocker, Explanation: "unchecked error"},
		},
	})

	body := buildPrBody(baseCreatePrInput(), summary)
	if !strings.Contains(body, "## Verification (canonical)") {
		t.Fatalf("expected a canonical verification section, got:\n%s", body)
	}
	if !strings.Contains(body, "fix refund rounding") {
		t.Fatalf("expected the canonical commit to appear, got:\n%s", body)
	}
	if !strings.Contains(body, "unchecked error") {
		t.Fatalf("expected the canonical risk to appear, got:\n%s", body)
	}
}

func TestBuildPrBodyOmitsCanonicalSectionWhenSummaryIsEmpty(t *testing.T) {
	body := buildPrBody(baseCreatePrInput(), deliverysummary.Summary{})
	if strings.Contains(body, "Verification (canonical)") {
		t.Fatalf("expected no canonical section for an empty summary, got:\n%s", body)
	}
}
