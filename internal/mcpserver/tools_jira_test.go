package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
)

const fakeJiraEstimateAdapterEnv = "PUNAKAWAN_TEST_JIRA_ESTIMATE_ADAPTER"

// TestAssessJiraDeliveryHandler_ReportsSubtaskBreakdown seeds a hydrated
// delivery for PAY-1 with two subtasks and asserts assess_jira_delivery's
// additive SubtaskBreakdown/SubtaskBreakdownNote fields populate without
// disturbing the existing Assessment/View return values.
func TestAssessJiraDeliveryHandler_ReportsSubtaskBreakdown(t *testing.T) {
	a := newTestApp(t)
	a.AdapterRegistry = adapters.NewRegistry(map[string]adapters.AdapterSpec{
		"atlassian": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestJiraEstimateFakeAdapter"},
			Env:     []string{fakeJiraEstimateAdapterEnv + "=1"},
		},
	})
	ctx := context.Background()

	store, err := OpenDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("OpenDeliveryStore: %v", err)
	}
	resolved, err := store.StartOrResolveExecution(ctx, "resolve", delivery.SourceIdentity{
		Kind: delivery.SourceKindJira, Provider: "jira", Tenant: "", Key: "PAY-1",
	}, delivery.OrchestrationOptions{})
	if err != nil {
		t.Fatalf("StartOrResolveExecution: %v", err)
	}
	orchestrationID := resolved.Execution.OrchestrationID
	if _, err := store.CaptureRequirement(ctx, "cap-parent", orchestrationID, delivery.SourceInput{
		Provider: "jira", ExternalID: "PAY-1", Title: "Parent story",
	}); err != nil {
		t.Fatalf("CaptureRequirement(parent): %v", err)
	}
	for key, title := range map[string]string{"PAY-2": "Subtask A", "PAY-3": "Subtask B"} {
		if _, err := store.CaptureRequirement(ctx, "cap-"+key, orchestrationID, delivery.SourceInput{
			Provider: "jira", ExternalID: key, ParentKey: "PAY-1", Title: title,
		}); err != nil {
			t.Fatalf("CaptureRequirement(%s): %v", key, err)
		}
	}

	_, out, err := assessJiraDeliveryHandler(a)(ctx, nil, AssessJiraDeliveryInput{
		ExecutionID: resolved.Execution.ID,
		Clarity:     "clear",
		Rationale:   "the issue is unambiguous",
	})
	if err != nil {
		t.Fatalf("assessJiraDeliveryHandler: %v", err)
	}

	if out.Assessment.Clarity != "clear" || out.Assessment.Rationale != "the issue is unambiguous" {
		t.Fatalf("Assessment = %+v, want the existing behavior untouched", out.Assessment)
	}
	if out.View.Orchestration == nil {
		t.Fatalf("View = %+v, want the existing view still populated", out.View)
	}

	if len(out.SubtaskBreakdown) != 2 {
		t.Fatalf("SubtaskBreakdown = %+v, want 2 subtasks", out.SubtaskBreakdown)
	}
	byKey := map[string]float64{}
	for _, e := range out.SubtaskBreakdown {
		if e.StoryPoints != nil {
			byKey[e.IssueKey] = *e.StoryPoints
		}
	}
	if byKey["PAY-2"] != 3 || byKey["PAY-3"] != 5 {
		t.Fatalf("story points by key = %+v, want PAY-2=3 PAY-3=5", byKey)
	}
	if !strings.Contains(out.SubtaskBreakdownNote, "PAY-2") || !strings.Contains(out.SubtaskBreakdownNote, "PAY-3") {
		t.Fatalf("SubtaskBreakdownNote = %q, want it to mention both subtasks as not yet covered by a work-item mapping", out.SubtaskBreakdownNote)
	}
}

func TestJiraEstimateFakeAdapter(t *testing.T) {
	if os.Getenv(fakeJiraEstimateAdapterEnv) != "1" {
		return
	}

	input := bufio.NewScanner(os.Stdin)
	output := json.NewEncoder(os.Stdout)
	for input.Scan() {
		var request struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(input.Bytes(), &request); err != nil {
			return
		}
		switch request.Method {
		case "capabilities":
			writeFakeJiraEstimateAdapterResult(output, request.ID, map[string]any{
				"id": "atlassian", "name": "Fake Atlassian", "version": "0.0.0",
				"protocol": "punakawan.adapter/v1", "runtime": "node", "provides": []string{"atlassian"},
				"permissions": map[string]any{
					"network":    map[string]any{"hosts": []string{}},
					"filesystem": map[string]any{"read": []string{}, "write": []string{}},
					"secrets":    []string{},
				},
				"operations": map[string]any{
					"atlassian.getJiraIssue":          map[string]any{"side_effect": false, "description": "test fixture operation", "input_schema": map[string]any{"type": "object"}},
					"atlassian.getIssueTypeFieldMeta": map[string]any{"side_effect": false, "description": "test fixture operation", "input_schema": map[string]any{"type": "object"}},
					"atlassian.searchJira":            map[string]any{"side_effect": false, "description": "test fixture operation", "input_schema": map[string]any{"type": "object"}},
				},
			})
		case "initialize":
			writeFakeJiraEstimateAdapterResult(output, request.ID, map[string]any{"ok": true})
		case "execute":
			var params map[string]any
			if err := json.Unmarshal(request.Params, &params); err != nil {
				writeFakeJiraEstimateAdapterError(output, request.ID, "invalid execute params")
				continue
			}
			switch params["op"] {
			case "atlassian.getJiraIssue":
				writeFakeJiraEstimateAdapterResult(output, request.ID, map[string]any{
					"normalized": map[string]any{
						"key": "PAY-1", "source": map[string]any{"uri": "jira://cloud-1/PAY-1"},
						"projectKey": "PAY", "issueTypeId": "10001",
					},
				})
			case "atlassian.getIssueTypeFieldMeta":
				writeFakeJiraEstimateAdapterResult(output, request.ID, map[string]any{
					"payload": map[string]any{"fields": map[string]any{"customfield_10016": map[string]any{"name": "Story Points"}}},
				})
			case "atlassian.searchJira":
				writeFakeJiraEstimateAdapterResult(output, request.ID, map[string]any{
					"normalized": []map[string]any{
						{"key": "PAY-2", "customFields": map[string]any{"customfield_10016": 3}},
						{"key": "PAY-3", "customFields": map[string]any{"customfield_10016": 5}},
					},
				})
			default:
				writeFakeJiraEstimateAdapterError(output, request.ID, "unexpected op")
			}
		case "shutdown":
			writeFakeJiraEstimateAdapterResult(output, request.ID, map[string]any{"ok": true})
			return
		default:
			writeFakeJiraEstimateAdapterError(output, request.ID, "unexpected method")
		}
	}
}

func writeFakeJiraEstimateAdapterResult(output *json.Encoder, id int64, result any) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeFakeJiraEstimateAdapterError(output *json.Encoder, id int64, message string) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -1, "message": message}})
}
