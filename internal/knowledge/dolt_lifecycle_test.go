package knowledge

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
)

// TestReusedServerStoppedByLastCloser is the regression guard for
// punokawan-q9r.6.1: a Dolt sql-server shared by two in-process Stores must
// survive the first Close and be stopped by the last, leaving no orphaned
// process and no lingering registry entry.
func TestReusedServerStoppedByLastCloser(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "knowledge")
	key := filepath.Clean(dataDir)
	sup := tools.New(dir)

	owner, err := Open(sup, dataDir)
	if err != nil {
		t.Fatalf("Open owner: %v", err)
	}
	proc := owner.server
	if proc == nil {
		t.Fatal("owner should have started the server")
	}
	if owner.dataDir != key {
		t.Fatalf("owner.dataDir = %q, want %q", owner.dataDir, key)
	}

	shared, err := Open(sup, dataDir)
	if err != nil {
		_ = owner.Close()
		t.Fatalf("Open shared: %v", err)
	}
	if shared.server != nil {
		t.Fatal("second store should reuse, not start, a server")
	}
	if shared.dataDir != key {
		t.Fatal("in-process reuser should join the refcount (dataDir set)")
	}
	assertRefs(t, key, 2)

	// First closer must NOT stop the server: the sharer is still connected.
	if err := owner.Close(); err != nil {
		t.Fatalf("Close owner: %v", err)
	}
	assertRefs(t, key, 1)
	if err := shared.db.Ping(); err != nil {
		t.Fatalf("shared connection died when owner closed: %v", err)
	}
	select {
	case <-proc.Done():
		t.Fatal("server exited after the first (non-last) Close")
	default:
	}

	// Last closer must stop the server and clear the registry.
	if err := shared.Close(); err != nil {
		t.Fatalf("Close shared: %v", err)
	}
	select {
	case <-proc.Done():
	case <-time.After(10 * time.Second):
		_ = proc.Stop()
		t.Fatal("server was not stopped by the last closer (leak)")
	}
	serverRegistry.mu.Lock()
	_, present := serverRegistry.servers[key]
	serverRegistry.mu.Unlock()
	if present {
		t.Fatal("registry still holds the server after the last Close")
	}
}

// TestCrossProcessClientKeepsServerAlive exercises the branch the whole
// refactor exists to get right (punokawan-q9r.6.1): when the last in-process
// Store closes but another client (standing in for a different OS process that
// reused the server via sql-server.info) is still connected, the server must be
// left running rather than reaped.
func TestCrossProcessClientKeepsServerAlive(t *testing.T) {
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "knowledge")
	dbName := filepath.Base(dataDir)
	sup := tools.New(dir)

	owner, err := Open(sup, dataDir)
	if err != nil {
		t.Fatalf("Open owner: %v", err)
	}
	proc := owner.server
	if proc == nil {
		t.Fatal("owner should have started the server")
	}

	// A raw connection that does NOT go through the in-process refcount -
	// exactly what a second OS process's reuse looks like from this process's
	// point of view. Ping to force a live connection onto the server.
	foreign, err := connectExistingServer(dataDir, dbName, 2*time.Second)
	if err != nil {
		_ = owner.Close()
		t.Fatalf("connect foreign client: %v", err)
	}
	if err := foreign.Ping(); err != nil {
		_ = foreign.Close()
		_ = owner.Close()
		t.Fatalf("ping foreign client: %v", err)
	}

	// owner is the last in-process holder, but the foreign client is connected,
	// so Close must orphan (not stop) the server.
	if err := owner.Close(); err != nil {
		t.Fatalf("Close owner: %v", err)
	}
	select {
	case <-proc.Done():
		t.Fatal("server was stopped while another client was still connected")
	case <-time.After(1 * time.Second):
		// still running, as required
	}
	if err := foreign.Ping(); err != nil {
		t.Fatalf("foreign client lost its still-running server: %v", err)
	}

	_ = foreign.Close()
	_ = proc.Stop()
}

func assertRefs(t *testing.T, key string, want int) {
	t.Helper()
	serverRegistry.mu.Lock()
	defer serverRegistry.mu.Unlock()
	ss, ok := serverRegistry.servers[key]
	if !ok {
		t.Fatalf("registry has no entry for %q", key)
	}
	if ss.refs != want {
		t.Fatalf("refs = %d, want %d", ss.refs, want)
	}
}
