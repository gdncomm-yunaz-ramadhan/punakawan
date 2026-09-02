package mcpserver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// freeLoopbackAddr picks an OS-assigned free loopback port and returns its
// address, closing the probe listener immediately so ServeHTTP can bind it.
// The race window between close and ServeHTTP's own bind is the standard,
// accepted pattern for this kind of test.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return addr
}

// TestServeHTTPEndToEnd starts ServeHTTP on a real loopback port, connects
// a real mcp.StreamableClientTransport client to it (the same transport a
// non-subprocess harness would use), confirms role_list/role_get are
// reachable exactly as they are over stdio, then cancels the context and
// confirms ServeHTTP shuts down cleanly.
func TestServeHTTPEndToEnd(t *testing.T) {
	a := newTestApp(t)
	addr := freeLoopbackAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- ServeHTTP(ctx, a, addr) }()

	url := "http://" + addr + "/"
	var cs *mcp.ClientSession
	deadline := time.Now().Add(5 * time.Second)
	for {
		client := mcp.NewClient(&mcp.Implementation{Name: "test-http-client", Version: "v0.0.1"}, nil)
		var err error
		cs, err = client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("client.Connect over Streamable HTTP never succeeded: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	res, err := cs.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		cancel()
		t.Fatalf("ListTools over Streamable HTTP: %v", err)
	}
	names := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"role_list", "role_get", "plan_get"} {
		if !names[want] {
			t.Errorf("Streamable HTTP tools/list missing %q, want it reachable identically to the stdio path", want)
		}
	}

	// Close the client connection before shutting the server down:
	// http.Server.Shutdown blocks until every active connection ends, and
	// this test's Streamable HTTP client holds a long-lived stream open
	// until explicitly closed.
	if err := cs.Close(); err != nil {
		t.Logf("client close: %v", err)
	}
	cancel()
	select {
	case err := <-serveErrCh:
		if err != nil {
			t.Fatalf("ServeHTTP after context cancellation = %v, want nil (clean shutdown)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHTTP did not return within 5s of context cancellation")
	}
}
