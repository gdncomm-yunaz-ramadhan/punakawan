package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
)

// readyPollInterval and startTimeout bound how long EnsureRunning waits
// for a freshly spawned daemon to answer /healthz before giving up.
const (
	readyPollInterval = 100 * time.Millisecond
	startTimeout      = 10 * time.Second
)

// Client talks to a running daemon over its authenticated loopback
// transport. It never opens storage itself - every CLI, MCP, and Panel
// call site should reach data through a Client rather than storage.Open
// directly, so every access goes through the one daemon-owned connection.
type Client struct {
	addr  string
	token string
	http  *http.Client
	// watchHTTP backs WatchDeliveryDetail's long-poll requests, which can
	// legitimately take up to maxDeliveryWaitSeconds - http's own fixed
	// Timeout would cut those off, so long-poll calls use this client
	// (unbounded Timeout; bounded instead via the request's own context
	// deadline) instead of http.
	watchHTTP *http.Client
}

// Discover builds a Client from an already-running daemon's published
// port and token files. It does not check liveness itself - callers
// that need "start it if not running" semantics should use EnsureRunning.
func Discover(paths Paths) (*Client, error) {
	addr, err := os.ReadFile(paths.PortPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: read port file %s: %w", paths.PortPath, err)
	}
	token, err := os.ReadFile(paths.TokenPath)
	if err != nil {
		return nil, fmt.Errorf("daemon: read token file %s: %w", paths.TokenPath, err)
	}
	return &Client{
		addr:      strings.TrimSpace(string(addr)),
		token:     strings.TrimSpace(string(token)),
		http:      &http.Client{Timeout: 5 * time.Second},
		watchHTTP: &http.Client{},
	}, nil
}

// Healthy calls the unauthenticated liveness endpoint.
func (c *Client) Healthy(ctx context.Context) error { return c.get(ctx, "/healthz", false) }

// Ready calls the authenticated readiness endpoint.
func (c *Client) Ready(ctx context.Context) error { return c.get(ctx, "/readyz", true) }

func (c *Client) get(ctx context.Context, path string, authed bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.addr+path, nil)
	if err != nil {
		return err
	}
	if authed {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon: %s %s", path, resp.Status)
	}
	return nil
}

// EnsureRunning returns a Client for a live, ready daemon: if one is
// already running it connects to it, otherwise it spawns punakawand as
// a detached background process and waits for it to become healthy.
//
// This is the interim "first use" path: spawning a plain background
// process, not yet installing a persistent OS-managed service
// (LaunchAgent/systemd/Windows Scheduled Task) that
// survives a reboot - that registration is tracked separately in the
// issue's notes, since it cannot be verified end-to-end for every
// platform from a single development machine.
func EnsureRunning(ctx context.Context, paths Paths) (*Client, error) {
	if running, _, err := Status(paths.LockPath); err != nil {
		return nil, err
	} else if running {
		return waitForHealthy(ctx, paths)
	}

	exe, err := daemonExecutable()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(paths.LockPath)
	sup := tools.New(dir)
	if _, err := sup.StartBackground(tools.Spec{Name: exe, Args: []string{"run"}, Dir: dir}, filepath.Join(dir, "daemon.log")); err != nil {
		return nil, fmt.Errorf("daemon: start %s: %w", exe, err)
	}
	return waitForHealthy(ctx, paths)
}

func waitForHealthy(ctx context.Context, paths Paths) (*Client, error) {
	deadline := time.Now().Add(startTimeout)
	var lastErr error
	for {
		client, err := Discover(paths)
		if err == nil {
			if lastErr = client.Healthy(ctx); lastErr == nil {
				return client, nil
			}
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("daemon: not healthy after %s: %w", startTimeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(readyPollInterval):
		}
	}
}

// daemonExecutable locates the punakawand binary: first on PATH, then
// beside the currently running executable (the layout a packaged
// install ships both binaries in).
func daemonExecutable() (string, error) {
	if path, err := exec.LookPath("punakawand"); err == nil {
		return path, nil
	}
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("daemon: locate punakawand: %w", err)
	}
	name := "punakawand"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidate := filepath.Join(filepath.Dir(self), name)
	if _, err := os.Stat(candidate); err != nil {
		return "", fmt.Errorf("daemon: punakawand not found on PATH or beside %s", self)
	}
	return candidate, nil
}

// DiscoverDefault is a convenience wrapper combining DefaultPaths and
// EnsureRunning for the common case of connecting to this machine's one
// canonical daemon.
func DiscoverDefault(ctx context.Context) (*Client, error) {
	paths, err := DefaultPaths()
	if err != nil {
		return nil, err
	}
	return EnsureRunning(ctx, paths)
}
