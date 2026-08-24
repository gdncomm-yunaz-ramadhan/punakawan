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
	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/panel/registry"
	"github.com/ygrip/punakawan/internal/panel/server"
)

// panelServeFlags are the bind/workspace settings that decide what a
// panel process serves. They are shared by `panel`, which runs the
// server in the foreground, and `panel service install`, which bakes the
// same settings into a background service definition - keeping one
// definition here is what makes `panel service install --port 1234`
// register a service that serves on the port `panel --port 1234` would
// have used.
type panelServeFlags struct {
	host          string
	port          string
	workspacePath string
}

func (f *panelServeFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.host, "host", "127.0.0.1", "bind address (must be loopback)")
	cmd.Flags().StringVar(&f.port, "port", "7331", "bind port")
	cmd.Flags().StringVar(&f.workspacePath, "workspace", "", "workspace root (defaults to the current directory)")
}

func newPanelCmd() *cobra.Command {
	var serve panelServeFlags
	var openBrowser bool

	cmd := &cobra.Command{
		Use:   "panel",
		Short: "Start the Punakawan Panel: a local, loopback-only web dashboard",
		Long: "Start the Punakawan Panel, a local web dashboard served from this binary. " +
			"It binds to loopback only, auto-registers the current workspace, and protects " +
			"mutations with an authenticated session.\n\n" +
			"This command runs in the foreground and stops when its terminal goes away. To keep the " +
			"panel available without a terminal - started at login and restarted if it crashes - " +
			"register it as a background service instead:\n\n" +
			"  punakawan panel service install\n" +
			"  punakawan panel service status\n\n" +
			"See `punakawan panel service --help` for the full set of subcommands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			host, port, workspacePath := serve.host, serve.port, serve.workspacePath
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
			defer reg.Close()

			// Auto-register the current workspace, per §7: "Punakawan
			// automatically registers a workspace when it successfully
			// detects .punakawan/workspace.yaml." Re-running `panel` in
			// the same workspace is idempotent (Register updates
			// last_seen_at rather than erroring).
			if _, err := reg.Register(a.Workspace.ID, a.Workspace.Root, a.Workspace.Name, time.Now().UTC()); err != nil {
				return fmt.Errorf("panel: register workspace: %w", err)
			}

			// The daemon is optional for the panel as a whole - only the
			// Deliveries tab needs it - so a failure here is logged and
			// otherwise ignored rather than aborting the panel command;
			// DaemonClient nil leaves the delivery routes wired but
			// answering 503, same as any other missing subsystem.
			daemonClient, err := daemon.DiscoverDefault(cmd.Context())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "panel: daemon unavailable, delivery data will be disabled: %v\n", err)
			}

			s := server.New(a, reg, server.Options{Host: host, Port: port, DaemonClient: daemonClient})
			if err := s.Start(); err != nil {
				return fmt.Errorf("panel: start server: %w", err)
			}

			bootstrapURL := s.BootstrapURL()
			fmt.Fprintf(cmd.OutOrStdout(), "Punakawan Panel listening on %s\n", bootstrapURL)
			if openBrowser {
				openInBrowser(bootstrapURL)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			<-ctx.Done()

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return s.Shutdown(shutdownCtx)
		},
	}

	serve.register(cmd)
	cmd.Flags().BoolVar(&openBrowser, "open-browser", true, "open the panel in the default browser on startup")
	cmd.AddCommand(newPanelServiceCmd())
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
