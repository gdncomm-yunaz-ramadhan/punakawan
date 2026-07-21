package adapters

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestCallFailsFastWhenProcessDiesInFlight guards against the other half of
// the crash-resilience bug reported in practice: a request already in
// flight when the adapter process crashes used to block Call forever
// (waiting on a response channel that readLoop can never deliver to),
// instead of surfacing the failure.
func TestCallFailsFastWhenProcessDiesInFlight(t *testing.T) {
	if _, err := os.Stat(prototypeAdapterPath); err != nil {
		t.Skipf("prototype adapter not built (%s): %v; run `pnpm --filter @punakawan/adapter-sdk build` first", prototypeAdapterPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := Start(ctx, "node", prototypeAdapterPath)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Kill()

	if _, err := client.Call(ctx, "capabilities", nil); err != nil {
		t.Fatalf("capabilities: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		// Long enough that Kill (issued right after) reliably lands while
		// this call is still in flight, not after it already completed.
		_, err := client.Call(ctx, "execute", map[string]any{"op": "sleep", "ms": 5000})
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)
	if err := client.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected Call to fail once the process was killed mid-request")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return within 2s of the process being killed - it hung instead of failing fast")
	}
}
