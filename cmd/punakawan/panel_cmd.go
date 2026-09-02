package main

import (
	"context"
	"fmt"
	"net"
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
	var foreground bool

	cmd := &cobra.Command{
		Use:   "panel",
		Short: "Start the Punakawan Panel: a local, loopback-only web dashboard",
		Long: "Start the Punakawan Panel, a local web dashboard served from this binary. " +
			"It binds to loopback only, auto-registers the current workspace, and protects " +
			"mutations with an authenticated session.\n\n" +
			"The panel starts in the background and this command returns as soon as it is " +
			"answering, printing the address and opening it in your browser. Read what it has " +
			"printed with `punakawan panel logs`, and stop it with `punakawan panel stop`.\n\n" +
			"Use --foreground to run the server in this terminal instead, which is what the " +
			"background service and any external supervisor do.\n\n" +
			"To have the panel start at login and be restarted if it crashes, register it with the " +
			"operating system instead:\n\n" +
			"  punakawan panel service install\n" +
			"  punakawan panel service status\n\n" +
			"See `punakawan panel service --help` for the full set of subcommands.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !foreground {
				return runPanelDetached(cmd, serve, openBrowser)
			}
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
	cmd.Flags().BoolVar(&foreground, "foreground", false, "run the server in this terminal instead of starting it in the background")
	cmd.AddCommand(newPanelServiceCmd())
	cmd.AddCommand(newPanelLogsCmd())
	cmd.AddCommand(newPanelStopCmd())
	cmd.AddCommand(newPanelStatusCmd())
	return cmd
}

// runPanelDetached is the default `punakawan panel`: leave a panel
// running, say where it is, and give the terminal back.
func runPanelDetached(cmd *cobra.Command, serve panelServeFlags, openBrowser bool) error {
	out := cmd.OutOrStdout()

	// Something already on the port is the honest signal that a panel is
	// up, whoever owns it - this session's, the login service's, or one
	// started from another checkout. Starting a second would only fail to
	// bind, so point at the one that exists.
	if addressAnswers(serve.host, serve.port) {
		address := "http://" + net.JoinHostPort(serve.host, serve.port)
		fmt.Fprintf(out, "Punakawan Panel already running at %s\n", address)
		if openBrowser {
			openInBrowser(address)
		}
		return nil
	}

	record, err := startPanelDetached(serve)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Punakawan Panel running at %s\n", record.address())
	fmt.Fprintf(out, "  workspace: %s\n", record.Workspace)
	fmt.Fprintf(out, "  logs:      punakawan panel logs\n")
	fmt.Fprintf(out, "  stop:      punakawan panel stop\n")
	if openBrowser {
		openInBrowser(record.address())
	}
	return nil
}

func newPanelLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show what the background panel has printed",
		Long: "Show the background panel's output. The log is appended to across restarts, so a " +
			"panel that failed to start still has its reason here.",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := panelLogPath()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(cmd.OutOrStdout(), "No panel log yet. Start one with `punakawan panel`.")
					return nil
				}
				return fmt.Errorf("panel logs: read %s: %w", path, err)
			}
			tail := tailLines(string(data), lines)
			if tail != "" {
				fmt.Fprintln(cmd.OutOrStdout(), tail)
			}
			if !follow {
				return nil
			}
			return followPanelLog(cmd.OutOrStdout(), int64(len(data)))
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new output until interrupted")
	cmd.Flags().IntVarP(&lines, "lines", "n", 200, "how many trailing lines to show")
	return cmd
}

func newPanelStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the background panel started by `punakawan panel`",
		Long: "Stop the background panel this command started. It does not stop the login service - " +
			"use `punakawan panel service stop` for that.",
		RunE: func(cmd *cobra.Command, args []string) error {
			record, wasRunning, err := stopPanel(10 * time.Second)
			if err != nil {
				return err
			}
			if !wasRunning {
				fmt.Fprintln(cmd.OutOrStdout(), "No background panel is running.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Stopped the panel at %s (pid %d)\n", record.address(), record.PID)
			return nil
		},
	}
}

func newPanelStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether a background panel is running, and where",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			record, running, err := readPanelRecord()
			if err != nil {
				return err
			}
			if !running {
				fmt.Fprintln(out, "running:   no")
				fmt.Fprintln(out, "Start one with `punakawan panel`.")
				return nil
			}
			fmt.Fprintf(out, "running:   yes (pid %d)\n", record.PID)
			fmt.Fprintf(out, "address:   %s\n", record.address())
			fmt.Fprintf(out, "workspace: %s\n", record.Workspace)
			fmt.Fprintf(out, "started:   %s\n", record.StartedAt.Local().Format(time.RFC3339))
			fmt.Fprintf(out, "log:       %s\n", record.LogPath)
			return nil
		},
	}
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
