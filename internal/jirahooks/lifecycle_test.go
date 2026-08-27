package jirahooks

import (
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
)

func TestAdapterWriteAddComment(t *testing.T) {
	op, params, err := adapterWrite(&delivery.JiraWriteIntent{
		JiraIssueKey: "TRF-19272",
		Action:       "add_comment",
		Payload:      map[string]any{"comment_body": "Assessment complete"},
	})
	if err != nil {
		t.Fatalf("adapterWrite(add_comment): %v", err)
	}
	if op != "atlassian.addJiraComment" {
		t.Fatalf("operation = %q, want atlassian.addJiraComment", op)
	}
	if params["issueIdOrKey"] != "TRF-19272" || params["commentBody"] != "Assessment complete" {
		t.Fatalf("params = %#v", params)
	}
}
