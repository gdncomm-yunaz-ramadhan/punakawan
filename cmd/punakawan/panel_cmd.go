package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/server"
)

func newPanelCmd() *cobra.Command {
	var host, port, workspacePath string
	var openBrowser bool

	cmd := &cobra.Command{
		Use:   "panel",
		Short: "Start the Punakawan Panel: a local, loopback-only web dashboard",
		Long: "Start the Punakawan Panel, per punakawan-panel-implementation-plan.md: a read-only " +
			"local web dashboard served from this binary. It binds to loopback only, auto-registers " +
			"the current workspace, and never writes to any workspace state.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := workspacePath
			if dir == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				dir = cwd
			}

			a, err := app.Load(dir)
			if err != nil {
				return fmt.Errorf("panel: load workspace: %w", err)
			}
			defer a.Close()

			reg, err := registry.Open()
			if err != nil {
				return fmt.Errorf("panel: open workspace registry: %w", err)
			}

			// Auto-register the current workspace, per §7: "Punakawan
			// automatically registers a workspace when it successfully
			// detects .punakawan/workspace.yaml." Re-running `panel` in
			// the same workspace is idempotent (Register updates
			// last_seen_at rather than erroring).
			if _, err := reg.Register(a.Workspace.ID, a.Workspace.Root, a.Workspace.Name, time.Now().UTC()); err != nil {
				return fmt.Errorf("panel: register workspace: %w", err)
			}

			s := server.New(a, reg, server.Options{Host: host, Port: port})
			if err := s.Start(); err != nil {
				return fmt.Errorf("panel: start server: %w", err)
			}

			addr := s.Addr()
			fmt.Fprintf(cmd.OutOrStdout(), "Punakawan Panel listening on http://%s\n", addr)
			if openBrowser {
				openInBrowser("http://" + addr)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			<-ctx.Done()

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return s.Shutdown(shutdownCtx)
		},
	}

	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "bind address (must be loopback)")
	cmd.Flags().StringVar(&port, "port", "7331", "bind port")
	cmd.Flags().StringVar(&workspacePath, "workspace", "", "workspace root (defaults to the current directory)")
	cmd.Flags().BoolVar(&openBrowser, "open-browser", true, "open the panel in the default browser on startup")
	return cmd
}

// openInBrowser best-effort opens url in the OS default browser. Failure
// is not fatal to the panel command - the user can always navigate there
// manually, per the addr printed to stdout.
func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
