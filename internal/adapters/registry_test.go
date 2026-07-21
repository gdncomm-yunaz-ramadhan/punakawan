package adapters

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/approvals"
)

const prototypeAdapterPath = "../../packages/adapter-sdk/dist/prototypeAdapter.js"

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	if _, err := os.Stat(prototypeAdapterPath); err != nil {
		t.Skipf("prototype adapter not built (%s): %v; run `pnpm --filter @punakawan/adapter-sdk build` first", prototypeAdapterPath, err)
	}
	store, err := approvals.Open(t.TempDir())
	if err != nil {
		t.Fatalf("approvals.Open: %v", err)
	}
	specs := map[string]AdapterSpec{
		"prototype": {Command: "node", Args: []string{prototypeAdapterPath}},
	}
	return NewRegistry(specs, store)
}

func TestRegistryGateStartsAndFetchesManifest(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	g, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if g.adapterID != "prototype" {
		t.Fatalf("adapterID = %q, want prototype", g.adapterID)
	}
	if g.manifest.Id != "prototype" {
		t.Fatalf("manifest.Id = %q, want prototype", g.manifest.Id)
	}
	if _, ok := g.manifest.Operations["sleep"]; !ok {
		t.Fatalf("manifest.Operations = %+v, want sleep present", g.manifest.Operations)
	}

	if _, err := g.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep: %v", err)
	}
}

func TestRegistryGateMemoizes(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	g1, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	g2, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate (second call): %v", err)
	}
	if g1 != g2 {
		t.Fatal("expected second Gate call to return the same memoized instance")
	}
	if len(r.clients) != 1 {
		t.Fatalf("clients = %d, want 1 (no duplicate process spawned)", len(r.clients))
	}
}

func TestRegistryGateUnknownAdapterID(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	if _, err := r.Gate(ctx, "does-not-exist"); err == nil {
		t.Fatal("expected error for unknown adapter id")
	}
}

// TestRegistryGateRespawnsAfterCrash guards against the bug reported in
// practice: once an adapter process crashes, every future call to that
// adapter used to fail identically ("broken pipe") until the whole
// punakawan process restarted, since Gate memoized the now-dead Client
// forever. Gate must instead detect the death and transparently respawn.
func TestRegistryGateRespawnsAfterCrash(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer r.Close(ctx)

	g1, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if _, err := g1.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep before crash: %v", err)
	}

	client := r.clients["prototype"]
	if err := client.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitUntilDead(t, client)

	g2, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate after crash: %v", err)
	}
	if g1 == g2 {
		t.Fatal("expected a fresh Gate after the previous process crashed, got the same memoized instance")
	}
	if _, err := g2.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep on respawned process: %v", err)
	}
}

func waitUntilDead(t *testing.T, c *Client) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !c.Dead() {
		if time.Now().After(deadline) {
			t.Fatal("client not marked Dead within 2s of Kill")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRegistryCloseShutsDownProcess(t *testing.T) {
	r := newTestRegistry(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	g, err := r.Gate(ctx, "prototype")
	if err != nil {
		t.Fatalf("Gate: %v", err)
	}
	if _, err := g.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err != nil {
		t.Fatalf("Call sleep before close: %v", err)
	}

	if err := r.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := g.Call(ctx, "run-1", "sleep", map[string]any{"ms": 0}); err == nil {
		t.Fatal("expected Call to fail after Close shut down the adapter process")
	}
}
