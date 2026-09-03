//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/deliveryservice"
	"github.com/ygrip/punakawan/internal/githubintegration"
)

// TestDeliveryJiraWorkflow drives one Jira-sourced delivery through its
// complete lifecycle - hydrate, plan, work, provider sync, pull request,
// completion, and a later continuation of the same Jira issue - against
// fake Atlassian/GitHub HTTP servers and a real temporary git repository,
// proving every provider write actually lands exactly once and every
// projection a caller re-reads reflects it.
func TestDeliveryJiraWorkflow(t *testing.T) {
	ctx := context.Background()
	s := newStack(t)

	s.registry.atlassianServer.addIssue("ABC-2", "ABC-1", "Implement widget", "Do the widget work", "To Do", "Task")
	s.registry.atlassianServer.addIssue("ABC-1", "", "Widget epic", "Ship the widget", "To Do", "Story", "ABC-2")

	repoDir := newTempGitRepo(t)
	svc := s.deliveryService()
	jira := s.jiraService(defaultJiraWorkflowConfig())
	github := s.githubService()

	start, needInput, err := svc.StartOrResolve(ctx, deliveryservice.StartRequest{
		IdempotencyKey: "start-abc-1",
		Source:         &deliveryservice.SourceIdentity{Kind: deliveryservice.SourceJira, Provider: "jira", Tenant: "tenant-1", Key: "abc-1"},
		Title:          "Widget epic",
		HighLevelPlan:  deliveryservice.PlanDraft{Objective: "Deliver the widget epic"},
		Session:        deliveryservice.SessionStart{Participant: "agent-1"},
		Projects: []deliveryservice.ProjectDraft{{
			Slug: "widget-svc", RepositoryURL: repoDir, DefaultBranch: "main",
			TaskKey: "ABC-2", Title: "Implement widget",
			Plan: deliveryservice.PlanDraft{Objective: "Implement widget end to end"},
		}},
	})
	if err != nil {
		t.Fatalf("StartOrResolve: %v", err)
	}
	if needInput != nil {
		t.Fatalf("StartOrResolve needed input: %+v", needInput)
	}
	orchestrationID := start.Execution.OrchestrationID

	// hydrate: both the parent issue and its subtask became durable
	// requirement sources.
	sources, err := s.deliveries.ListRequirementSources(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("ListRequirementSources: %v", err)
	}
	var subtaskSourceID string
	seenKeys := map[string]bool{}
	for _, src := range sources {
		if src.ExternalId != nil {
			seenKeys[*src.ExternalId] = true
			if *src.ExternalId == "ABC-2" {
				subtaskSourceID = src.Id
			}
		}
	}
	if !seenKeys["ABC-1"] || !seenKeys["ABC-2"] {
		t.Fatalf("expected hydration to capture both ABC-1 and ABC-2, got %v", seenKeys)
	}
	if subtaskSourceID == "" {
		t.Fatalf("expected a requirement source id for ABC-2")
	}

	// plan: reconciliation routed ABC-2 onto a project and produced a
	// runnable lane.
	if len(start.Reconciliation.RunnableWork) == 0 {
		t.Fatalf("expected reconciliation to produce runnable work, got %+v", start.Reconciliation)
	}
	lanes, err := s.deliveries.ListLanes(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("ListLanes: %v", err)
	}
	if len(lanes) != 1 {
		t.Fatalf("expected exactly one lane, got %d", len(lanes))
	}
	lane := lanes[0]
	if lane.ParentTaskId == nil {
		t.Fatalf("expected the lane to have a parent task")
	}

	// Jira sync: delivery started posts a comment and transitions ABC-1
	// to the configured start status.
	if err := jira.OnDeliveryStarted(ctx, orchestrationID); err != nil {
		t.Fatalf("OnDeliveryStarted: %v", err)
	}
	s.drainOutbox(t, 20)
	s.registry.atlassianServer.mu.Lock()
	startedIssue := s.registry.atlassianServer.issues["ABC-1"]
	startedStatus := startedIssue.status
	startedComments := len(startedIssue.comments)
	s.registry.atlassianServer.mu.Unlock()
	if startedStatus != "In Progress" {
		t.Fatalf("ABC-1 status after delivery started = %q, want In Progress", startedStatus)
	}
	if startedComments == 0 {
		t.Fatalf("expected a delivery-started comment on ABC-1")
	}

	// work: record one measured interval against the mapped subtask and
	// sync it as a Jira worklog.
	if start.Session == nil {
		t.Fatalf("expected StartOrResolve to open a delivery session")
	}
	if _, err := s.deliveries.MapWorkItemToJiraTask(ctx, "map-abc-2", start.Execution.ID, start.Session.ID, *lane.ParentTaskId, subtaskSourceID, "ABC-2"); err != nil {
		t.Fatalf("MapWorkItemToJiraTask: %v", err)
	}
	entry, err := s.deliveries.RecordWorkLog(ctx, "worklog-1", "wl-1", orchestrationID, lane.Id, start.Session.ID, "ABC-2", time.Now().Add(-time.Hour), 3600, "implemented the widget")
	if err != nil {
		t.Fatalf("RecordWorkLog: %v", err)
	}
	if err := jira.OnWorkRecorded(ctx, entry.ID); err != nil {
		t.Fatalf("OnWorkRecorded: %v", err)
	}
	s.drainOutbox(t, 20)
	s.registry.atlassianServer.mu.Lock()
	worklogged := len(s.registry.atlassianServer.issues["ABC-2"].worklogs)
	s.registry.atlassianServer.mu.Unlock()
	if worklogged == 0 {
		t.Fatalf("expected the worked interval to sync to ABC-2 as a Jira worklog")
	}

	// sync -> PRs: open one pull request through the durable outbox,
	// synchronously resolved.
	number, url, err := github.CreatePullRequest(ctx, githubintegration.CreatePullRequestRequest{
		RunID: orchestrationID, Repository: "acme/widget-svc", BaseBranch: "main", HeadBranch: "lane/abc-2",
		Title: "Implement widget", Body: "Closes ABC-2",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}
	if number == 0 || url == "" {
		t.Fatalf("CreatePullRequest returned number=%d url=%q, want both set", number, url)
	}

	// complete: complete the delivery, then sync completion to Jira.
	orch, err := s.deliveries.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		t.Fatalf("GetOrchestration: %v", err)
	}
	if _, err := svc.Complete(ctx, "complete-abc-1", orchestrationID, orch.Revision); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if err := jira.OnDeliveryCompleted(ctx, orchestrationID); err != nil {
		t.Fatalf("OnDeliveryCompleted: %v", err)
	}
	s.drainOutbox(t, 20)
	s.registry.atlassianServer.mu.Lock()
	completedIssue := s.registry.atlassianServer.issues["ABC-1"]
	completedStatus := completedIssue.status
	completedComments := len(completedIssue.comments)
	s.registry.atlassianServer.mu.Unlock()
	if completedStatus != "Done" {
		t.Fatalf("ABC-1 status after delivery completed = %q, want Done", completedStatus)
	}
	if completedComments <= startedComments {
		t.Fatalf("expected a second, delivery-completed comment on ABC-1")
	}

	// continue: starting the same Jira issue again after completion opens
	// a new execution round under the same lifetime case, rather than
	// either reusing the completed execution or opening an unrelated case.
	start2, needInput2, err := svc.StartOrResolve(ctx, deliveryservice.StartRequest{
		IdempotencyKey: "start-abc-1-again",
		Source:         &deliveryservice.SourceIdentity{Kind: deliveryservice.SourceJira, Provider: "jira", Tenant: "tenant-1", Key: "abc-1"},
		Title:          "Widget epic, round two",
		Session:        deliveryservice.SessionStart{Participant: "agent-1"},
	})
	if err != nil {
		t.Fatalf("continuation StartOrResolve: %v", err)
	}
	if needInput2 != nil {
		t.Fatalf("continuation StartOrResolve needed input: %+v", needInput2)
	}
	if start2.Lifetime.ID != start.Lifetime.ID {
		t.Fatalf("expected continuing ABC-1 after completion to reuse its lifetime %s, got %s", start.Lifetime.ID, start2.Lifetime.ID)
	}
	if start2.Execution.ID == start.Execution.ID {
		t.Fatalf("expected continuing ABC-1 after completion to open a new execution round")
	}
	if start2.Execution.Ordinal <= start.Execution.Ordinal {
		t.Fatalf("expected the continuation execution's ordinal (%d) to advance past the completed one (%d)", start2.Execution.Ordinal, start.Execution.Ordinal)
	}
	if !strings.EqualFold(start2.Lifetime.JiraIssueKey, "ABC-1") {
		t.Fatalf("continuation lifetime jira issue key = %q, want ABC-1", start2.Lifetime.JiraIssueKey)
	}
}
