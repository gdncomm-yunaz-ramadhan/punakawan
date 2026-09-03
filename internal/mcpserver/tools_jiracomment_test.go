package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/internal/delivery"
)

const fakeJiraCommentAdapterEnv = "PUNAKAWAN_TEST_JIRA_COMMENT_ADAPTER"

// jiraAddCommentOperation names the adapter operation
// jiraintegration.Service.PostComment enqueues - kept here as a test
// fixture constant since this test's fake adapter needs to name it too.
const jiraAddCommentOperation = "atlassian.addJiraComment"

func TestPostJiraCommentHandler_MissingFieldsIsAValidationError(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	if _, _, err := postJiraCommentHandler(a)(ctx, nil, PostJiraCommentInput{}); err == nil {
		t.Fatal("expected a validation error for a fully empty input")
	}
	if _, _, err := postJiraCommentHandler(a)(ctx, nil, PostJiraCommentInput{
		OrchestrationID: "delivery-1", JiraIssueKey: "PAY-1",
	}); err == nil {
		t.Fatal("expected a validation error when comment_body and idempotency_key are missing")
	}
}

func TestPostJiraCommentHandler_PostsSuccessfully(t *testing.T) {
	a := newTestApp(t)
	a.AdapterRegistry = adapters.NewRegistry(map[string]adapters.AdapterSpec{
		"atlassian": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestPostJiraCommentFakeAdapter"},
			Env:     []string{fakeJiraCommentAdapterEnv + "=1"},
		},
	})
	ctx := context.Background()

	store, err := OpenDeliveryStore(ctx, a)
	if err != nil {
		t.Fatalf("OpenDeliveryStore: %v", err)
	}
	orch, err := store.CreateOrchestration(ctx, "create-1", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if _, err := store.CaptureRequirement(ctx, "cap-1", orch.Id, delivery.SourceInput{
		Provider: "jira", ExternalID: "PAY-1", Title: "Refund API",
	}); err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}

	_, out, err := postJiraCommentHandler(a)(ctx, nil, PostJiraCommentInput{
		OrchestrationID: orch.Id,
		JiraIssueKey:    "PAY-1",
		CommentBody:     "This is an agent-posted comment.",
		IdempotencyKey:  "idem-1",
	})
	if err != nil {
		t.Fatalf("postJiraCommentHandler: %v", err)
	}
	if out.Status != "posted" || out.CommentID != "c-701" || out.IssueKey != "PAY-1" {
		t.Fatalf("output = %+v, want posted with external comment id c-701", out)
	}
}

func TestPostJiraCommentFakeAdapter(t *testing.T) {
	if os.Getenv(fakeJiraCommentAdapterEnv) != "1" {
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
			writeFakeJiraCommentAdapterResult(output, request.ID, map[string]any{
				"id":       "atlassian",
				"name":     "Fake Atlassian",
				"version":  "0.0.0",
				"protocol": "punakawan.adapter/v1",
				"runtime":  "node",
				"provides": []string{"atlassian"},
				"permissions": map[string]any{
					"network":    map[string]any{"hosts": []string{}},
					"filesystem": map[string]any{"read": []string{}, "write": []string{}},
					"secrets":    []string{},
				},
				"operations": map[string]any{
					jiraAddCommentOperation: map[string]any{
						"side_effect":  true,
						"description":  "test fixture operation",
						"input_schema": map[string]any{"type": "object"},
					},
				},
			})
		case "initialize":
			writeFakeJiraCommentAdapterResult(output, request.ID, map[string]any{"ok": true})
		case "execute":
			var params map[string]any
			if err := json.Unmarshal(request.Params, &params); err != nil {
				writeFakeJiraCommentAdapterError(output, request.ID, "invalid execute params")
				continue
			}
			if params["op"] != jiraAddCommentOperation || params["issueIdOrKey"] != "PAY-1" {
				writeFakeJiraCommentAdapterError(output, request.ID, "unexpected comment submission")
				continue
			}
			writeFakeJiraCommentAdapterResult(output, request.ID, map[string]any{"ok": true, "commentId": "c-701"})
		case "shutdown":
			writeFakeJiraCommentAdapterResult(output, request.ID, map[string]any{"ok": true})
			return
		default:
			writeFakeJiraCommentAdapterError(output, request.ID, "unexpected method")
		}
	}
}

func writeFakeJiraCommentAdapterResult(output *json.Encoder, id int64, result any) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeFakeJiraCommentAdapterError(output *json.Encoder, id int64, message string) {
	_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -1, "message": message}})
}
