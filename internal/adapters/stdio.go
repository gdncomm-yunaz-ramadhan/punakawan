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
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("adapter error %d: %s", e.Code, e.Message)
}

// Client talks JSON-RPC 2.0 over stdio to a single spawned adapter process.
type Client struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan response

	done chan struct{}
}

// Start spawns the adapter process and begins reading its responses.
func Start(ctx context.Context, name string, args ...string) (*Client, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = os.Stderr

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
		pending: make(map[int64]chan response),
		done:    make(chan struct{}),
	}
	go c.readLoop(stdout)
	return c, nil
}

func (c *Client) readLoop(r io.Reader) {
	defer close(c.done)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
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
		ch, ok := c.pending[*resp.ID]
		if ok {
			delete(c.pending, *resp.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp
		}
	}
}

// Call sends a JSON-RPC request and blocks for its response. If ctx is
// cancelled or times out first, Call sends a best-effort "cancel"
// notification for the in-flight request and returns ctx.Err().
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan response, 1)
	c.pending[id] = ch
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
