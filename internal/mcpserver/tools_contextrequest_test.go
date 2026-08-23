package mcpserver

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMissingContextRequestRoundTrip(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var submitted map[string]any
	callTool(t, cs, "submit_missing_context_request", map[string]any{
		"capsule_id": "cap-1",
		"query":      "refund SLA policy",
		"reason":     "prior submission did not include the SLA requirement",
		"blocking":   true,
	}, &submitted)

	reqID, _ := submitted["id"].(string)
	if reqID == "" {
		t.Fatalf("submitted = %+v, want a non-empty id", submitted)
	}
	if submitted["status"] != "pending" {
		t.Fatalf("submitted status = %v, want pending", submitted["status"])
	}

	var listed map[string]any
	callTool(t, cs, "list_missing_context_requests", map[string]any{}, &listed)
	requests, _ := listed["requests"].([]any)
	var found bool
	for _, r := range requests {
		if r.(map[string]any)["id"] == reqID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list_missing_context_requests = %+v, want %s listed as pending", listed, reqID)
	}

	revisedCapsuleID := "cap-2"

	var resolved map[string]any
	callTool(t, cs, "resolve_missing_context_request", map[string]any{
		"id":                 reqID,
		"resolution":         "added_to_revision",
		"revised_capsule_id": revisedCapsuleID,
		"note":               "found the SLA requirement and built a new capsule",
	}, &resolved)

	if resolved["status"] != "added_to_revision" {
		t.Fatalf("resolved status = %v, want added_to_revision", resolved["status"])
	}
	if resolved["revised_capsule_id"] != revisedCapsuleID {
		t.Fatalf("resolved revised_capsule_id = %v, want %s", resolved["revised_capsule_id"], revisedCapsuleID)
	}

	var listedAfter map[string]any
	callTool(t, cs, "list_missing_context_requests", map[string]any{}, &listedAfter)
	pendingAfter, _ := listedAfter["requests"].([]any)
	for _, r := range pendingAfter {
		if r.(map[string]any)["id"] == reqID {
			t.Fatalf("listedAfter = %+v, want %s no longer pending after resolution", listedAfter, reqID)
		}
	}
}

func TestResolveMissingContextRequestRejectsAddedToRevisionWithoutCapsuleId(t *testing.T) {
	requireDolt(t)
	a := newTestApp(t)
	cs := connect(t, a)

	var submitted map[string]any
	callTool(t, cs, "submit_missing_context_request", map[string]any{
		"capsule_id": "cap-1",
		"query":      "x",
		"reason":     "y",
		"blocking":   false,
	}, &submitted)

	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "resolve_missing_context_request",
		Arguments: map[string]any{
			"id":         submitted["id"],
			"resolution": "added_to_revision",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected an error resolving added_to_revision without revised_capsule_id")
	}
}
