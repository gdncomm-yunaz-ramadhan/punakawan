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

func TestAdapterWriteAcceptsLegacyPayloadAliases(t *testing.T) {
	tests := []struct {
		name    string
		action  string
		payload map[string]any
		wantOp  string
		wantKey string
		want    string
	}{
		{"comment", "add_comment", map[string]any{"comment": "legacy comment"}, "atlassian.addJiraComment", "commentBody", "legacy comment"},
		{"description", "update_description", map[string]any{"description_body": "legacy description"}, "atlassian.editJiraIssue", "description", "legacy description"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op, params, err := adapterWrite(&delivery.JiraWriteIntent{JiraIssueKey: "TRF-19272", Action: tt.action, Payload: tt.payload})
			if err != nil || op != tt.wantOp || params[tt.wantKey] != tt.want {
				t.Fatalf("adapterWrite = %q, %#v, %v", op, params, err)
			}
		})
	}
}
