package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/adapters"
	"github.com/ygrip/punakawan/pkg/protocol"
)

type approvalWriteInput struct {
	RunId string `json:"run_id"`
	Op    string `json:"op"`
}

func connectApprovalServer(
	t *testing.T,
	gate *adapters.Gate,
	clientOptions *mcp.ClientOptions,
) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "approval-test-server", Version: "v0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "write"}, func(ctx context.Context, req *mcp.CallToolRequest, in approvalWriteInput) (*mcp.CallToolResult, map[string]any, error) {
		_, err := invokeAdapterOperation(ctx, req, gate, in.RunId, in.Op, nil, protocol.ApprovalRecordRequestedByPetruk)
		return nil, map[string]any{"ok": err == nil}, err
	})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "approval-test-client", Version: "v0"}, clientOptions)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { clientSession.Close() })
	return clientSession
}

func callApprovalWrite(t *testing.T, cs *mcp.ClientSession, runID, op string) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "write",
		Arguments: map[string]any{"run_id": runID, "op": op},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return res
}

func TestInlineElicitationApprovesEveryAdapterWriteForRun(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	elicitations := 0
	cs := connectApprovalServer(t, gate, &mcp.ClientOptions{
		ElicitationHandler: func(_ context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			elicitations++
			for _, want := range []string{"atlassian", "run-1", "all configured adapters"} {
				if !strings.Contains(req.Params.Message, want) {
					t.Errorf("elicitation message %q does not contain %q", req.Params.Message, want)
				}
			}
			return &mcp.ElicitResult{Action: "accept"}, nil
		},
	})

	for _, op := range []string{"atlassian.addWorklog", "atlassian.addJiraComment"} {
		if res := callApprovalWrite(t, cs, "run-1", op); res.IsError {
			t.Fatalf("write %s returned error: %+v", op, res.Content)
		}
	}
	if elicitations != 1 {
		t.Fatalf("elicitation count = %d, want 1 for the whole adapter run", elicitations)
	}
	if len(fc.calls) != 2 {
		t.Fatalf("adapter calls = %+v, want both writes", fc.calls)
	}
}

func TestApprovalFallsBackToCLIWhenClientCannotElicit(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	cs := connectApprovalServer(t, gate, nil)

	res := callApprovalWrite(t, cs, "run-cli", "atlassian.addWorklog")
	if !res.IsError {
		t.Fatal("write succeeded without elicitation or CLI approval")
	}
	message := ""
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			message += text.Text
		} else {
			message += fmt.Sprint(content)
		}
	}
	for _, want := range []string{"does not support form elicitation", "punakawan approvals approve", "approval-adapter-run-run-cli"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}
	if len(fc.calls) != 0 {
		t.Fatalf("adapter calls = %+v, want none", fc.calls)
	}
}

func TestDeclinedElicitationDeniesRunWithoutWriting(t *testing.T) {
	gate, fc := newJiraClarifyTestGateWithManifest(t, progressTestManifest())
	elicitations := 0
	cs := connectApprovalServer(t, gate, &mcp.ClientOptions{
		ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
			elicitations++
			return &mcp.ElicitResult{Action: "decline"}, nil
		},
	})

	for range 2 {
		if res := callApprovalWrite(t, cs, "run-denied", "atlassian.addWorklog"); !res.IsError {
			t.Fatal("declined write unexpectedly succeeded")
		}
	}
	if elicitations != 1 {
		t.Fatalf("elicitation count = %d, want 1; a denied run must not re-prompt", elicitations)
	}
	if len(fc.calls) != 0 {
		t.Fatalf("adapter calls = %+v, want none", fc.calls)
	}
}
