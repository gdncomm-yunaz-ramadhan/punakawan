package adapters

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// oversizedEchoScript is a minimal JSON-RPC responder (not a real adapter -
// no capabilities/initialize/shutdown handshake) used only to control
// exactly how large a single response line is, independent of what any
// real operation happens to return. A request whose method is "oversized"
// gets a response line larger than sizeBytes; every other method gets a
// small, well under the limit response.
const oversizedEchoScript = `
process.stdin.setEncoding('utf8');
let buf = '';
process.stdin.on('data', (chunk) => {
  buf += chunk;
  let idx;
  while ((idx = buf.indexOf('\n')) >= 0) {
    const line = buf.slice(0, idx);
    buf = buf.slice(idx + 1);
    if (!line.trim()) continue;
    const req = JSON.parse(line);
    if (req.method === 'cancel') continue;
    if (req.method === 'oversized') {
      // node -e has no script file, so argv[0] is the node binary and
      // argv[1] is the first "-- arg" (unlike a normal script invocation,
      // where that argument would land at argv[2]).
      const filler = 'x'.repeat(Number(process.argv[1]));
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: req.id, result: { filler } }) + '\n');
    } else {
      process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: req.id, result: { ok: true } }) + '\n');
    }
  }
});
`

func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found on PATH")
	}
}

// TestClientResponseJustBelowTheLimitSucceeds guards the "just below 32 MiB
// succeeds" half of the response-size ceiling: raising the scanner buffer
// from 1 MiB to maxAdapterResponseBytes must not have shrunk anything
// legitimate operations already relied on returning.
func TestClientResponseJustBelowTheLimitSucceeds(t *testing.T) {
	requireNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Comfortably under the limit once JSON framing overhead is added.
	fillerBytes := maxAdapterResponseBytes - 4096
	client, err := Start(ctx, "node", "-e", oversizedEchoScript, "--", fillerString(fillerBytes))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Kill()

	if _, err := client.Call(ctx, "oversized", nil); err != nil {
		t.Fatalf("expected a response just under the limit to succeed, got: %v", err)
	}
}

// TestClientResponseOverTheLimitReturnsBoundedError guards the "larger
// returns a bounded protocol error" half: previously, a response exceeding
// the scanner's buffer silently killed the whole read loop (marking the
// Client permanently dead) and every in-flight call surfaced only a
// generic "adapter process exited" message with no indication why.
func TestClientResponseOverTheLimitReturnsBoundedError(t *testing.T) {
	requireNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := Start(ctx, "node", "-e", oversizedEchoScript, "--", fillerString(maxAdapterResponseBytes+1024*1024))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Kill()

	_, err = client.Call(ctx, "oversized", nil)
	if err == nil {
		t.Fatal("expected an error for a response exceeding the size limit")
	}
	var tooLarge *ErrResponseTooLarge
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected *ErrResponseTooLarge, got %T: %v", err, err)
	}
	if tooLarge.Operation != "oversized" {
		t.Fatalf("Operation = %q, want %q", tooLarge.Operation, "oversized")
	}
	if tooLarge.LimitByte != maxAdapterResponseBytes {
		t.Fatalf("LimitByte = %d, want %d", tooLarge.LimitByte, maxAdapterResponseBytes)
	}
}

// fillerString renders n as a decimal string, passed as an argv to the
// spawned script for its own `'x'.repeat(N)` call - the filler bytes are
// generated inside the Node process itself, not in Go, so a 32+MiB string
// is only ever allocated once.
func fillerString(n int) string {
	if n < 0 {
		n = 0
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

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
