// Command punakawand is the Punakawan daemon: the one process per
// install that owns the SQLite storage kernel and serves the
// authenticated loopback transport every CLI, MCP, and Panel client
// connects through (punokawan-14yn.17).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/procreg"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: punakawand run|status|stop")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = runDaemon()
	case "status":
		err = statusDaemon()
	case "stop":
		err = stopDaemon()
	default:
		err = fmt.Errorf("unknown command %q (want run|status|stop)", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "punakawand:", err)
		os.Exit(1)
	}
}

func runDaemon() error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	d, err := daemon.Run(ctx, "127.0.0.1", daemon.DefaultPort, paths)
	if err != nil {
		var already *daemon.ErrAlreadyRunning
		if errors.As(err, &already) {
			fmt.Fprintln(os.Stdout, "punakawand: already running, pid", already.PID)
			return nil
		}
		return err
	}
	fmt.Fprintln(os.Stdout, "punakawand: listening on", d.Addr())
	reportReconciliation(d.Reconciled())

	serveErr := make(chan error, 1)
	go func() { serveErr <- d.Serve() }()

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return d.Shutdown(shutdownCtx)
}

// reportReconciliation surfaces what startup reconciliation found. A
// non-empty Preserved list is an operator-visible anomaly per AC4: a
// pid this daemon previously owned now belongs to a different process
// and was deliberately left untouched rather than killed.
func reportReconciliation(r procreg.Result) {
	if len(r.Killed) > 0 {
		fmt.Fprintln(os.Stdout, "punakawand: reconciled", len(r.Killed), "orphaned process(es) from a previous instance")
	}
	if len(r.Preserved) > 0 {
		fmt.Fprintln(os.Stderr, "punakawand: WARNING - pid identity mismatch for", len(r.Preserved), "record(s):", r.Preserved, "- these processes were left running, not killed")
	}
}

func statusDaemon() error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}
	running, pid, err := daemon.Status(paths.LockPath)
	if err != nil {
		return err
	}
	if !running {
		fmt.Println("not running")
		os.Exit(1)
	}
	fmt.Println("running, pid", pid)
	return nil
}

func stopDaemon() error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}
	running, pid, err := daemon.Status(paths.LockPath)
	if err != nil {
		return err
	}
	if !running {
		fmt.Println("not running")
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}
	fmt.Println("sent stop signal to pid", pid)
	return nil
}
