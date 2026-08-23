package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{
		LockPath:  filepath.Join(dir, "daemon.lock"),
		TokenPath: filepath.Join(dir, "daemon.token"),
		PortPath:  filepath.Join(dir, "daemon.port"),
		DBPath:    filepath.Join(dir, "punakawan.db"),
	}
}

func TestRunServeShutdownLifecycle(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()

	if _, err := os.Stat(paths.PortPath); err != nil {
		t.Fatalf("expected port file to be published: %v", err)
	}
	resp, err := http.Get("http://" + d.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	running, pid, err := Status(paths.LockPath)
	if err != nil || !running || pid != os.Getpid() {
		t.Fatalf("expected Status to report this process running, got running=%v pid=%d err=%v", running, pid, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if running, _, _ := Status(paths.LockPath); running {
		t.Fatal("expected lock released after Shutdown")
	}
	if _, err := os.Stat(paths.PortPath); !os.IsNotExist(err) {
		t.Fatalf("expected port file removed after Shutdown, stat err=%v", err)
	}
}

func TestRunSecondCallerSeesAlreadyRunning(t *testing.T) {
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

	_, err = Run(context.Background(), "127.0.0.1", "0", paths)
	var already *ErrAlreadyRunning
	if !errors.As(err, &already) {
		t.Fatalf("expected ErrAlreadyRunning for a second Run against the same paths, got %v", err)
	}
}

func TestReadyzReflectsStorageOpen(t *testing.T) {
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

	token, err := LoadOrCreateToken(paths.TokenPath)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+d.Addr()+"/readyz", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 once storage is open, got %d", resp.StatusCode)
	}
}
