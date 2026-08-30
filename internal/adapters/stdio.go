// Package adapters implements the Go-core side of the adapter lifecycle:
// spawning a TypeScript adapter process and exchanging JSON-RPC 2.0 messages
// with it over stdio, per punakawan-go-typescript-detailed-plan.md §5.1-§5.3.
//
// Framing is newline-delimited JSON (one message per line) for the same
// reason the plan picked stdio JSON-RPC over gRPC first: it is trivial to
// inspect, log, and test.
package adapters

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object returned by the adapter.
type RPCError struct {
	Code    int           `json:"code"`
	Message string        `json:"message"`
	Data    *RPCErrorData `json:"data,omitempty"`
}

// RPCErrorData carries the structured diagnostic data an adapter attaches
// to an error, currently just the cancellation marker
// packages/adapter-sdk/src/stdio.ts's serveStdio sets when a handler's
// promise rejects because its AbortSignal fired.
type RPCErrorData struct {
	Code string `json:"code"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("adapter error %d: %s", e.Code, e.Message)
}

// Cancelled reports whether the adapter reported this error as a
// cancelled/aborted operation (Data.Code == "cancelled") rather than an
// ordinary rejection. A cancelled call may have already reached the
// provider before the abort landed, so a caller resolving a side-effecting
// write must treat this as ambiguous, never as a plain retryable failure.
func (e *RPCError) Cancelled() bool {
	return e != nil && e.Data != nil && e.Data.Code == "cancelled"
}

// maxAdapterResponseBytes bounds a single newline-delimited JSON-RPC
// response line this process will buffer from an adapter's stdout. Adapter
// operations may return large paginated collections (e.g. a GitHub pull
// request's full file/comment/check history); 1 MiB was too tight for that
// and silently left every in-flight call hanging forever once a response
// exceeded it, since readLoop below never inspected scanner.Err() after its
// loop ended. 32 MiB is intentionally generous while still bounded: a
// caller returning more than this should stream large binary attachments
// to a file rather than inline them in JSON, not push for a larger buffer.
const maxAdapterResponseBytes = 32 * 1024 * 1024

// ErrResponseTooLarge is returned to every call still pending on a Client
// whose adapter process produced a single response line larger than
// maxAdapterResponseBytes. It carries enough metadata for a caller to
// surface a clear, bounded protocol error instead of hanging or panicking.
type ErrResponseTooLarge struct {
	AdapterID string
	Operation string
	LimitByte int
}

func (e *ErrResponseTooLarge) Error() string {
	return fmt.Sprintf(
		"adapters: response_too_large: adapter %q operation %q exceeded the %d byte response limit",
		e.AdapterID, e.Operation, e.LimitByte,
	)
}

// pendingCall is what Client tracks for one in-flight request: the channel
// its response is delivered on, and the method name - needed only so a
// readLoop failure that cannot be attributed to a specific response (e.g.
// ErrResponseTooLarge) can still report which operation was in flight.
type pendingCall struct {
	ch     chan response
	method string
}

// Client talks JSON-RPC 2.0 over stdio to a single spawned adapter process.
type Client struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]pendingCall

	// readErr is set once, immediately before done is closed, if readLoop
	// stopped because of a scan failure (as opposed to the adapter process
	// simply exiting/closing its stdout normally). Call's <-c.done case
	// reads it without a separate lock: readErr is only ever written before
	// done closes and only ever read after, so the close(c.done) itself is
	// the synchronization point (matching sync/atomic's "happens before a
	// receive that observes a close" guarantee for a plain channel close).
	readErr error

	done chan struct{}
}

// Start spawns the adapter process and begins reading its responses. The
// child inherits this process's full environment, matching exec.Cmd's
// default behavior when Env is nil.
func Start(ctx context.Context, name string, args ...string) (*Client, error) {
	return start(ctx, nil, name, args...)
}

// StartWithEnv spawns the adapter process with exactly env as its
// environment (not inheriting the parent process's environment), for
// callers that need to scope which variables - especially secrets - a
// spawned adapter can see, per §11.4/§15.2's secret-lease philosophy.
func StartWithEnv(ctx context.Context, env []string, name string, args ...string) (*Client, error) {
	return start(ctx, env, name, args...)
}

func start(ctx context.Context, env []string, name string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = os.Stderr
	if env != nil {
		cmd.Env = env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("adapters: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("adapters: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("adapters: start adapter process: %w", err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		pending: make(map[int64]pendingCall),
		done:    make(chan struct{}),
	}
	go c.readLoop(stdout)
	return c, nil
}

// Dead reports whether the adapter process's response stream has closed
// (readLoop exited, whether from a graceful shutdown or an unexpected
// crash). Registry uses this to evict and respawn a crashed adapter instead
// of memoizing a permanently-broken Client/Gate for the rest of this
// process's lifetime.
func (c *Client) Dead() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *Client) readLoop(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxAdapterResponseBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.ID == nil {
			continue
		}
		c.mu.Lock()
		call, ok := c.pending[*resp.ID]
		if ok {
			delete(c.pending, *resp.ID)
		}
		c.mu.Unlock()
		if ok {
			call.ch <- resp
		}
	}

	// scanner.Scan returned false either because the adapter's stdout
	// closed normally (process exit/shutdown - scanner.Err() is nil or
	// io.EOF) or because one response line exceeded maxAdapterResponseBytes
	// (bufio.ErrTooLong). Only the latter gets a distinct, bounded
	// diagnostic; every other in-flight call still falls back to Call's
	// generic "adapter process exited" message via the plain <-c.done case.
	if err := scanner.Err(); err != nil && errors.Is(err, bufio.ErrTooLong) {
		c.mu.Lock()
		method := ""
		for _, call := range c.pending {
			method = call.method
			break
		}
		c.mu.Unlock()
		c.readErr = &ErrResponseTooLarge{Operation: method, LimitByte: maxAdapterResponseBytes}
	}
	close(c.done)
}

// Call sends a JSON-RPC request and blocks for its response. If ctx is
// cancelled or times out first, Call sends a best-effort "cancel"
// notification for the in-flight request and returns ctx.Err().
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan response, 1)
	c.pending[id] = pendingCall{ch: ch, method: method}
	c.mu.Unlock()

	if err := c.writeLine(request{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		_ = c.writeLine(request{JSONRPC: "2.0", Method: "cancel", Params: map[string]any{"id": id}})
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		// The adapter process exited (crash or otherwise) while this call was
		// in flight: readLoop will never deliver a response for id, so without
		// this case Call would block forever instead of surfacing the failure.
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		if c.readErr != nil {
			return nil, c.readErr
		}
		return nil, fmt.Errorf("adapters: adapter process exited while waiting for a response")
	}
}

func (c *Client) writeLine(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("adapters: marshal request: %w", err)
	}
	b = append(b, '\n')
	if _, err := c.stdin.Write(b); err != nil {
		return fmt.Errorf("adapters: write request: %w", err)
	}
	return nil
}

// Shutdown asks the adapter to exit, then waits for the process to exit.
func (c *Client) Shutdown(ctx context.Context) error {
	if _, err := c.Call(ctx, "shutdown", nil); err != nil {
		return err
	}
	_ = c.stdin.Close()

	select {
	case <-c.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return c.cmd.Wait()
}

// Kill forcibly terminates the adapter process without a graceful shutdown.
func (c *Client) Kill() error {
	if c.cmd.Process == nil {
		return nil
	}
	return c.cmd.Process.Kill()
}
