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
