package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/adapters"
)

const fakeAdapterOperationsEnv = "PUNAKAWAN_TEST_ADAPTER_OPERATIONS"

func TestAdapterOperationHandlersDiscoverAndInvokeLiveOperations(t *testing.T) {
	a := newTestApp(t)
	a.AdapterRegistry = adapters.NewRegistry(map[string]adapters.AdapterSpec{
		"demo": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestAdapterOperationsFakeAdapter"},
			Env:     []string{fakeAdapterOperationsEnv + "=1"},
		},
	})

	ctx := context.Background()
	_, listed, err := listAdapterOperationsHandler(a)(ctx, nil, ListAdapterOperationsInput{AdapterID: "demo"})
	if err != nil {
		t.Fatalf("list operations: %v", err)
	}
	if len(listed.Adapters) != 1 || listed.Adapters[0].ID != "demo" {
		t.Fatalf("listed adapters = %+v, want demo", listed.Adapters)
	}
	if len(listed.Adapters[0].Operations) != 1 {
		t.Fatalf("operations = %+v, want one", listed.Adapters[0].Operations)
	}
	op := listed.Adapters[0].Operations[0]
	if op.Name != "demo.echo" || op.Description != "Echo a message." || op.InputSchema["type"] != "object" {
		t.Fatalf("operation = %+v, want documented demo.echo", op)
	}

	_, called, err := callAdapterOperationHandler(a)(ctx, nil, CallAdapterOperationInput{
		AdapterID:  "demo",
		Operation:  "demo.echo",
		RunID:      "run-1",
		Parameters: map[string]any{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("call operation: %v", err)
	}
	if string(called.Result) != `{"echo":"hello"}` {
		t.Fatalf("result = %s, want echo response", called.Result)
	}

	_, _, err = callAdapterOperationHandler(a)(ctx, nil, CallAdapterOperationInput{
		AdapterID: "demo", Operation: "demo.unknown", RunID: "run-1",
	})
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("undeclared operation error = %v, want manifest rejection", err)
	}
}

func TestAdapterOperationsFakeAdapter(t *testing.T) {
	if os.Getenv(fakeAdapterOperationsEnv) != "1" {
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
			_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{
				"id": "demo", "name": "Demo", "version": "0.0.0", "protocol": "punakawan.adapter/v1", "runtime": "node", "provides": []string{"demo"},
				"permissions": map[string]any{"network": map[string]any{"hosts": []string{}}, "filesystem": map[string]any{"read": []string{}, "write": []string{}}, "secrets": []string{}},
				"operations":  map[string]any{"demo.echo": map[string]any{"side_effect": false, "description": "Echo a message.", "input_schema": map[string]any{"type": "object", "required": []string{"message"}, "properties": map[string]any{"message": map[string]any{"type": "string"}}}}},
			}})
		case "initialize":
			_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"ok": true}})
		case "execute":
			var params map[string]any
			if err := json.Unmarshal(request.Params, &params); err != nil {
				return
			}
			if params["op"] != "demo.echo" {
				_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -1, "message": "unexpected operation"}})
				continue
			}
			_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"echo": params["message"]}})
		case "shutdown":
			_ = output.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"ok": true}})
			return
		}
	}
}
