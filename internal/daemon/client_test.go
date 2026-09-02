package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverAndHealthyAgainstRunningDaemon(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	client, err := Discover(paths)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if err := client.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
	if err := client.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestDiscoverWithoutRunningDaemonFails(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{
		LockPath:  filepath.Join(dir, "daemon.lock"),
		TokenPath: filepath.Join(dir, "daemon.token"),
		PortPath:  filepath.Join(dir, "daemon.port"),
		DBPath:    filepath.Join(dir, "punakawan.db"),
	}
	if _, err := Discover(paths); err == nil {
		t.Fatal("expected Discover to fail with no daemon.port file present")
	}
}

func TestEnsureRunningConnectsToAlreadyRunningDaemon(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := EnsureRunning(ctx, paths)
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if err := client.Healthy(context.Background()); err != nil {
		t.Fatalf("Healthy: %v", err)
	}
}

func TestEnsureRunningRestoresDiscoveryForDaemonWithoutStateFiles(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	if err := os.Remove(paths.PortPath); err != nil {
		t.Fatalf("remove port file: %v", err)
	}
	if err := os.Remove(paths.LockPath); err != nil {
		t.Fatalf("remove lock file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := ensureRunning(ctx, paths, d.Addr())
	if err != nil {
		t.Fatalf("EnsureRunning: %v", err)
	}
	if err := client.Ready(ctx); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	port, err := os.ReadFile(paths.PortPath)
	if err != nil {
		t.Fatalf("read restored port file: %v", err)
	}
	if got, want := string(port), d.Addr(); got != want {
		t.Fatalf("restored address = %q, want %q", got, want)
	}
}
