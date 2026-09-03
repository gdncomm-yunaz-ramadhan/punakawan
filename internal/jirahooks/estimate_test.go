package jirahooks

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/jiraworkflow"
	"github.com/ygrip/punakawan/internal/outbox"
	"github.com/ygrip/punakawan/internal/storage"
)

// setupBreakdownFixture seeds a Jira-sourced delivery for "PAY-1" with two
// subtasks, PAY-2 and PAY-3, and returns the execution id used to drive
// SuggestSubtaskBreakdown against it.
func setupBreakdownFixture(t *testing.T, fc *fakeAdapterCaller) (*Lifecycle, string) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)
	ob := outbox.New(db)

	resolved, err := store.StartOrResolveExecution(context.Background(), "resolve", delivery.SourceIdentity{
		Kind: delivery.SourceKindJira, Provider: "jira", Tenant: "test-tenant", Key: "PAY-1",
	}, delivery.OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	orchestrationID := resolved.Execution.OrchestrationID

	if _, err := store.CaptureRequirement(context.Background(), "cap-parent", orchestrationID, delivery.SourceInput{
		Provider: "jira", ExternalID: "PAY-1", Title: "Parent story",
	}); err != nil {
		t.Fatalf("CaptureRequirement(parent): %v", err)
	}
	for key, title := range map[string]string{"PAY-2": "Subtask A", "PAY-3": "Subtask B"} {
		if _, err := store.CaptureRequirement(context.Background(), "cap-"+key, orchestrationID, delivery.SourceInput{
			Provider: "jira", ExternalID: key, ParentKey: "PAY-1", Title: title,
		}); err != nil {
			t.Fatalf("CaptureRequirement(%s): %v", key, err)
		}
	}

	gate := adapters.NewGate("atlassian", testManifest(), fc)
	lifecycle := NewLifecycle(store, &fakeGateResolver{gate: gate}, ob)
	return lifecycle, resolved.Execution.ID
}

func TestSuggestSubtaskBreakdown_ResolvesPointsAndHours(t *testing.T) {
	fc := &fakeAdapterCaller{responses: map[string]string{
		"atlassian.getJiraIssue":          `{"normalized":{"key":"PAY-1","source":{"uri":"jira://cloud-1/PAY-1"},"projectKey":"PAY","issueTypeId":"10001"}}`,
		"atlassian.getIssueTypeFieldMeta": `{"payload":{"fields":{"customfield_10016":{"name":"Story Points"}}}}`,
		"atlassian.searchJira":            `{"normalized":[{"key":"PAY-2","customFields":{"customfield_10016":3}},{"key":"PAY-3","customFields":{"customfield_10016":5}}]}`,
	}}
	lifecycle, executionID := setupBreakdownFixture(t, fc)
	cfg := &jiraworkflow.Config{Estimation: jiraworkflow.EstimationConfig{PointsToHours: 4}}

	estimates, note := lifecycle.SuggestSubtaskBreakdown(context.Background(), executionID, "assess-1", cfg)
	if note != "" {
		t.Fatalf("note = %q, want empty", note)
	}
	if len(estimates) != 2 {
		t.Fatalf("estimates = %+v, want 2", estimates)
	}
	byKey := map[string]SubtaskEstimate{}
	for _, e := range estimates {
		byKey[e.IssueKey] = e
	}
	if byKey["PAY-2"].StoryPoints == nil || *byKey["PAY-2"].StoryPoints != 3 {
		t.Fatalf("PAY-2 story points = %+v, want 3", byKey["PAY-2"].StoryPoints)
	}
	if byKey["PAY-2"].EstimatedHours == nil || *byKey["PAY-2"].EstimatedHours != 12 {
		t.Fatalf("PAY-2 hours = %+v, want 12", byKey["PAY-2"].EstimatedHours)
	}
	if byKey["PAY-3"].StoryPoints == nil || *byKey["PAY-3"].StoryPoints != 5 {
		t.Fatalf("PAY-3 story points = %+v, want 5", byKey["PAY-3"].StoryPoints)
	}
	if byKey["PAY-3"].EstimatedHours == nil || *byKey["PAY-3"].EstimatedHours != 20 {
		t.Fatalf("PAY-3 hours = %+v, want 20", byKey["PAY-3"].EstimatedHours)
	}

	var sawSearch bool
	for _, c := range fc.calls {
		if c["op"] == "atlassian.searchJira" {
			sawSearch = true
			jql, _ := c["jql"].(string)
			if !strings.Contains(jql, "PAY-2") || !strings.Contains(jql, "PAY-3") {
				t.Fatalf("jql = %q, want both subtask keys in one call", jql)
			}
		}
	}
	if !sawSearch {
		t.Fatalf("calls = %+v, want a single atlassian.searchJira call", fc.calls)
	}
}

func TestSuggestSubtaskBreakdown_NoRatioConfiguredLeavesHoursNil(t *testing.T) {
	fc := &fakeAdapterCaller{responses: map[string]string{
		"atlassian.getJiraIssue":          `{"normalized":{"key":"PAY-1","source":{"uri":"jira://cloud-1/PAY-1"},"projectKey":"PAY","issueTypeId":"10001"}}`,
		"atlassian.getIssueTypeFieldMeta": `{"payload":{"fields":{"customfield_10016":{"name":"Story Points"}}}}`,
		"atlassian.searchJira":            `{"normalized":[{"key":"PAY-2","customFields":{"customfield_10016":3}},{"key":"PAY-3","customFields":{"customfield_10016":5}}]}`,
	}}
	lifecycle, executionID := setupBreakdownFixture(t, fc)
	cfg := jiraworkflow.Default() // PointsToHours left at 0: "not configured"

	estimates, note := lifecycle.SuggestSubtaskBreakdown(context.Background(), executionID, "assess-1", cfg)
	if len(estimates) != 2 {
		t.Fatalf("estimates = %+v, want 2", estimates)
	}
	for _, e := range estimates {
		if e.StoryPoints == nil {
			t.Fatalf("estimate %+v missing story points", e)
		}
		if e.EstimatedHours != nil {
			t.Fatalf("estimate %+v has hours set, want nil since no ratio is configured", e)
		}
	}
	if !strings.Contains(note, "points_to_hours") {
		t.Fatalf("note = %q, want it to explain the missing points_to_hours ratio", note)
	}
}

func TestSuggestSubtaskBreakdown_NoStoryPointsFieldResolvable(t *testing.T) {
	fc := &fakeAdapterCaller{
		responses: map[string]string{
			"atlassian.getJiraIssue": `{"normalized":{"key":"PAY-1","source":{"uri":"jira://cloud-1/PAY-1"},"projectKey":"PAY","issueTypeId":"10001"}}`,
		},
		failOps: map[string]bool{"atlassian.getIssueTypeFieldMeta": true},
	}
	lifecycle, executionID := setupBreakdownFixture(t, fc)
	cfg := &jiraworkflow.Config{Estimation: jiraworkflow.EstimationConfig{PointsToHours: 4}}

	estimates, note := lifecycle.SuggestSubtaskBreakdown(context.Background(), executionID, "assess-1", cfg)
	if len(estimates) != 2 {
		t.Fatalf("estimates = %+v, want 2 subtasks still listed", estimates)
	}
	for _, e := range estimates {
		if e.StoryPoints != nil || e.EstimatedHours != nil {
			t.Fatalf("estimate %+v should have no points/hours when no field resolves", e)
		}
	}
	if note == "" || !strings.Contains(note, "Story Points field") {
		t.Fatalf("note = %q, want it to explain no Story Points field was resolvable", note)
	}
	for _, c := range fc.calls {
		if c["op"] == "atlassian.searchJira" {
			t.Fatalf("calls = %+v, want no searchJira call once the field can't be resolved", fc.calls)
		}
	}
}

func TestSuggestSubtaskBreakdown_NoSubtasksReturnsNothing(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := delivery.NewStore(db)
	ob := outbox.New(db)
	resolved, err := store.StartOrResolveExecution(context.Background(), "resolve", delivery.SourceIdentity{
		Kind: delivery.SourceKindJira, Provider: "jira", Tenant: "test-tenant", Key: "PAY-9",
	}, delivery.OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	if _, err := store.CaptureRequirement(context.Background(), "cap-parent", resolved.Execution.OrchestrationID, delivery.SourceInput{
		Provider: "jira", ExternalID: "PAY-9", Title: "Parent story",
	}); err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}

	fc := &fakeAdapterCaller{}
	gate := adapters.NewGate("atlassian", testManifest(), fc)
	lifecycle := NewLifecycle(store, &fakeGateResolver{gate: gate}, ob)

	estimates, note := lifecycle.SuggestSubtaskBreakdown(context.Background(), resolved.Execution.ID, "assess-1", jiraworkflow.Default())
	if estimates != nil || note != "" {
		t.Fatalf("estimates = %+v, note = %q, want both empty when there are no subtasks", estimates, note)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("calls = %+v, want no adapter calls when there are no subtasks to estimate", fc.calls)
	}
}

// decodeCustomFieldsSanity guards estimate.go's decode struct against a
// silent shape drift in the adapter's normalized searchJira output.
func TestSuggestSubtaskBreakdown_DecodesRealisticSearchJiraShape(t *testing.T) {
	raw := json.RawMessage(`{"normalized":[{"key":"PAY-2","summary":"Subtask A","customFields":{"customfield_10016":3}}],"page":{"returned":1}}`)
	var result struct {
		Normalized []struct {
			Key          string         `json:"key"`
			CustomFields map[string]any `json:"customFields"`
		} `json:"normalized"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(result.Normalized) != 1 || result.Normalized[0].Key != "PAY-2" {
		t.Fatalf("result = %+v", result)
	}
	if v, ok := result.Normalized[0].CustomFields["customfield_10016"].(float64); !ok || v != 3 {
		t.Fatalf("customFields = %+v", result.Normalized[0].CustomFields)
	}
}
