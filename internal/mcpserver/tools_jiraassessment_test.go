package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// assessmentTestManifest reuses atlassianTestManifest's approval-required
// addJiraComment entry's shape for createJiraSubtask too, same as
// syncSubtaskManifest in tools_jirasync_test.go.
func assessmentTestManifest() protocol.AdapterManifest {
	m := atlassianTestManifest()
	m.Operations["atlassian.createJiraSubtask"] = m.Operations["atlassian.addJiraComment"]
	return m
}

func TestSubmitJiraAssessmentPostsCommentAndCreatesTasksWithAiHoursEstimate(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, assessmentTestManifest())
	approveOp(t, gate, "run-1", "atlassian.addJiraComment")
	fc.responses = map[string]string{
		"atlassian.createJiraSubtask": `{"created":[{"key":"PAY-2","summary":"Add flag"}],"skipped":[]}`,
	}

	in := SubmitJiraAssessmentInput{
		RunId:         "run-1",
		IssueIdOrKey:  "PAY-1",
		ProjectKey:    "PAY",
		IssueTypeName: "Subtask",
		Summary:       "Share links lack a feature flag.",
		Findings: []JiraAssessmentFinding{
			{Title: "No flag today", Detail: "share-hub always enables link sharing."},
		},
		OpenQuestions: []JiraAssessmentOpenQuestion{
			{Question: "Should the flag default on or off?", Why: "affects rollout risk", Important: true},
		},
		Tasks: []JiraAssessmentTask{
			{Summary: "Add flag", Plan: "Introduce a feature flag guarding link sharing.", AiHours: 4, HumanHours: 10},
		},
		RequestedBy: "petruk",
	}

	out, err := submitJiraAssessment(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("submitJiraAssessment: %v", err)
	}
	if !out.CommentPosted {
		t.Fatal("CommentPosted = false, want true")
	}
	if len(out.TasksCreated) != 1 {
		t.Fatalf("TasksCreated = %+v, want exactly one", out.TasksCreated)
	}
	got := out.TasksCreated[0]
	if got.Key != "PAY-2" || got.AiHours != 4 || got.HumanHours != 10 || got.TimeSavedHours != 6 {
		t.Errorf("TasksCreated[0] = %+v, want Key=PAY-2 AiHours=4 HumanHours=10 TimeSavedHours=6", got)
	}

	var sawComment, sawSubtask bool
	for _, c := range fc.calls {
		switch c["op"] {
		case "atlassian.addJiraComment":
			sawComment = true
			body, _ := c["commentBody"].(string)
			if !strings.Contains(body, "## Findings") || !strings.Contains(body, "## Open Questions") || !strings.Contains(body, "## Planned Tasks") {
				t.Errorf("commentBody missing expected headings: %q", body)
			}
			if !strings.Contains(body, "[Needs stakeholder decision]") {
				t.Errorf("commentBody does not flag the important open question: %q", body)
			}
		case "atlassian.createJiraSubtask":
			sawSubtask = true
			candidates, _ := c["candidates"].([]map[string]any)
			if len(candidates) != 1 {
				t.Fatalf("candidates = %+v, want exactly one", candidates)
			}
			fields, _ := candidates[0]["additionalFields"].(map[string]any)
			tt, _ := fields["timetracking"].(map[string]any)
			if tt["originalEstimate"] != "4h" || tt["remainingEstimate"] != "4h" {
				t.Errorf("timetracking = %+v, want originalEstimate=remainingEstimate=4h (ai_hours)", tt)
			}
		}
	}
	if !sawComment || !sawSubtask {
		t.Fatalf("calls = %+v, want addJiraComment and createJiraSubtask", fc.calls)
	}
}

func TestSubmitJiraAssessmentSkipsTaskCreationWhenNoTasksGiven(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, assessmentTestManifest())
	approveOp(t, gate, "run-1", "atlassian.addJiraComment")

	in := SubmitJiraAssessmentInput{RunId: "run-1", IssueIdOrKey: "PAY-1", Summary: "Just an assessment, no tasks yet.", RequestedBy: "petruk"}

	out, err := submitJiraAssessment(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("submitJiraAssessment: %v", err)
	}
	if !out.CommentPosted {
		t.Error("CommentPosted = false, want true")
	}
	if len(out.TasksCreated) != 0 || len(out.TasksSkipped) != 0 {
		t.Errorf("out = %+v, want no created or skipped tasks", out)
	}
	if len(fc.calls) != 1 {
		t.Fatalf("calls = %+v, want exactly one (no createJiraSubtask call attempted)", fc.calls)
	}
}

func TestSubmitJiraAssessmentFailsWithoutCommentApproval(t *testing.T) {
	gate, _ := newJiraClarifyTestGateWithManifest(t, assessmentTestManifest())
	in := SubmitJiraAssessmentInput{RunId: "run-1", IssueIdOrKey: "PAY-1", Summary: "hi", RequestedBy: "petruk"}

	if _, err := submitJiraAssessment(context.Background(), nil, gate, in); err == nil {
		t.Fatal("expected an error when addJiraComment has not been approved")
	}
}

func TestSubmitJiraAssessmentRejectsDuplicateSummaries(t *testing.T) {
	// punokawan-clq: identical task summaries collide when results are keyed
	// by summary, so they are rejected up front - before the comment is posted.
	gate, fc := newJiraClarifyTestGateWithManifest(t, assessmentTestManifest())
	approveOp(t, gate, "run-1", "atlassian.addJiraComment")

	in := SubmitJiraAssessmentInput{
		RunId:         "run-1",
		IssueIdOrKey:  "PAY-1",
		ProjectKey:    "PAY",
		IssueTypeName: "Subtask",
		Summary:       "assessment",
		Tasks: []JiraAssessmentTask{
			{Summary: "Same title", Plan: "a", AiHours: 1, HumanHours: 2},
			{Summary: "Same title", Plan: "b", AiHours: 3, HumanHours: 4},
		},
		RequestedBy: "petruk",
	}

	out, err := submitJiraAssessment(context.Background(), nil, gate, in)
	if err == nil {
		t.Fatal("expected an error for duplicate task summaries")
	}
	if out.CommentPosted {
		t.Error("CommentPosted = true, want false (rejected before any write)")
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want none (rejected before any adapter call)", fc.calls)
	}
}

func TestSubmitJiraAssessmentReturnsPartialSuccessWhenSubtasksFail(t *testing.T) {
	// punokawan-4tw: comment posts, then subtask creation fails - the call
	// returns a non-error result recording the comment succeeded and which
	// step failed, so the caller does not re-post the comment on retry.
	gate, caller := newJiraClarifyTestGateWithManifest(t, assessmentTestManifest())
	approveOp(t, gate, "run-1", "atlassian.addJiraComment")
	caller.failOps = map[string]bool{"atlassian.createJiraSubtask": true}

	in := SubmitJiraAssessmentInput{
		RunId:         "run-1",
		IssueIdOrKey:  "PAY-1",
		ProjectKey:    "PAY",
		IssueTypeName: "Subtask",
		Summary:       "assessment",
		Tasks:         []JiraAssessmentTask{{Summary: "Add flag", Plan: "p", AiHours: 4, HumanHours: 10}},
		RequestedBy:   "petruk",
	}

	out, err := submitJiraAssessment(context.Background(), nil, gate, in)
	if err != nil {
		t.Fatalf("submitJiraAssessment returned an error, want a non-error partial-success result: %v", err)
	}
	if !out.CommentPosted {
		t.Error("CommentPosted = false, want true")
	}
	if out.FailedStep != "subtasks" || out.FailedError == "" {
		t.Errorf("out = %+v, want FailedStep=subtasks with a non-empty FailedError", out)
	}
	if len(out.TasksCreated) != 0 {
		t.Errorf("TasksCreated = %+v, want none (creation failed)", out.TasksCreated)
	}
}

func TestRenderJiraAssessmentCommentHandlesNegativeTimeSaved(t *testing.T) {
	in := SubmitJiraAssessmentInput{
		Summary: "AI takes longer than a human here.",
		Tasks:   []JiraAssessmentTask{{Summary: "Tricky task", Plan: "...", AiHours: 5, HumanHours: 2}},
	}

	body := renderJiraAssessmentComment(in)
	if !strings.Contains(body, "-3h") {
		t.Errorf("body = %q, want it to contain -3h (time saved can go negative when AI is slower)", body)
	}
}
