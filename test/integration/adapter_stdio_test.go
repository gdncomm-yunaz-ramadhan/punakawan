//go:build integration

// Package integration exercises the JSON-RPC stdio prototype end to end:
// Go spawning the TypeScript prototype adapter and completing a handshake,
// per punakawan-go-typescript-detailed-plan.md §22 Milestone 0 acceptance
// criteria ("Go can start a TypeScript adapter", "cancellation and timeout
// work"). Requires `make build` (or `pnpm --filter @punakawan/adapter-sdk
// build`) to have produced packages/adapter-sdk/dist/prototypeAdapter.js.
package integration

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/adapters"
)

const prototypeAdapterPath = "../../packages/adapter-sdk/dist/prototypeAdapter.js"

func startPrototypeAdapter(t *testing.T, ctx context.Context) *adapters.Client {
	t.Helper()
	if _, err := os.Stat(prototypeAdapterPath); err != nil {
		t.Fatalf("prototype adapter not built (%s): %v; run `make build` first", prototypeAdapterPath, err)
	}
	client, err := adapters.Start(ctx, "node", prototypeAdapterPath)
	if err != nil {
		t.Fatalf("start adapter: %v", err)
	}
	return client
}

func TestInitializeHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := startPrototypeAdapter(t, ctx)
	defer client.Kill()

	manifest := map[string]any{
		"id":       "test-adapter",
		"name":     "Test Adapter",
		"version":  "0.1.0",
		"protocol": "punakawan.adapter/v1",
		"runtime":  "node",
		"provides": []string{"knowledge-source"},
		"permissions": map[string]any{
			"network":    map[string]any{"hosts": []string{}},
			"filesystem": map[string]any{"read": []string{}, "write": []string{}},
			"secrets":    []string{},
		},
		"operations": map[string]any{
			"noop": map[string]any{"side_effect": false},
		},
	}

	raw, err := client.Call(ctx, "initialize", manifest)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}

	var result struct {
		OK      bool   `json:"ok"`
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.OK || result.ID != "test-adapter" || result.Version != "0.1.0" {
		t.Fatalf("unexpected initialize result: %+v", result)
	}

	if err := client.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestExecuteCancellation(t *testing.T) {
	spawnCtx, cancelSpawn := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSpawn()

	client := startPrototypeAdapter(t, spawnCtx)
	defer client.Kill()

	callCtx, cancelCall := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelCall()

	start := time.Now()
	_, err := client.Call(callCtx, "execute", map[string]any{"op": "sleep", "ms": 5000})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Call did not return promptly on cancellation: took %s", elapsed)
	}
}
