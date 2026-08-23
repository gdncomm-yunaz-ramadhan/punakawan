package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newPanelServiceCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "service",
		Short: "Manage the panel as a background service that starts at login",
		Long: "Manage the Punakawan Panel as a background service.\n\n" +
			"`punakawan panel` runs in the foreground and dies with its terminal. Installing the " +
			"service instead registers the panel with the operating system so it starts at login, " +
			"is restarted if it crashes, and keeps serving with no terminal open.\n\n" +
			"On macOS this is a launchd user LaunchAgent under ~/Library/LaunchAgents. Other " +
			"platforms are not implemented yet and say so rather than silently doing nothing.",
	}
	root.AddCommand(newPanelServiceInstallCmd())
	root.AddCommand(newPanelServiceUninstallCmd())
	root.AddCommand(newPanelServiceStatusCmd())
	root.AddCommand(newPanelServiceStartCmd())
	root.AddCommand(newPanelServiceStopCmd())
	return root
}

func newPanelServiceInstallCmd() *cobra.Command {
	var serve panelServeFlags

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register the panel as a background service that starts at login",
		Long: "Register the panel as a background service that starts at login and is restarted " +
			"if it exits.\n\n" +
			"The bind flags are the same ones `punakawan panel` accepts, and whatever you pass is " +
			"baked into the service definition: `panel service install --port 1234` registers a " +
			"service that serves on port 1234. The workspace and the binary path are resolved to " +
			"absolute paths at install time, because a service started at login has neither your " +
			"shell's PATH nor your working directory.\n\n" +
			"Re-running install is safe: it rewrites the definition and reloads it, which is also " +
			"how you change the port or workspace of an already-installed service.",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newPanelServiceManager()
			if err != nil {
				return err
			}
			spec, err := panelServiceSpecFrom(serve)
			if err != nil {
				return err
			}
			if err := manager.Install(spec); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "panel service installed: %s\n", panelServiceLabel)
			fmt.Fprintf(out, "  binary:    %s\n", spec.BinaryPath)
			fmt.Fprintf(out, "  workspace: %s\n", spec.WorkspacePath)
			fmt.Fprintf(out, "  address:   %s\n", spec.address())
			fmt.Fprintf(out, "  stdout:    %s\n", spec.StdoutPath)
			fmt.Fprintf(out, "  stderr:    %s\n", spec.StderrPath)
			return nil
		},
	}
	serve.register(cmd)
	return cmd
}

func newPanelServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the panel service and remove its registration",
		Long: "Stop the panel service and remove its registration.\n\n" +
			"Safe to run when the service is already stopped, already removed, or was never " +
			"installed - a missing registration is treated as the desired end state, not an error.",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newPanelServiceManager()
			if err != nil {
				return err
			}
			if err := manager.Uninstall(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "panel service removed: %s\n", panelServiceLabel)
			return nil
		},
	}
}

func newPanelServiceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the already-registered panel service",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newPanelServiceManager()
			if err != nil {
				return err
			}
			if err := manager.Start(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "panel service started: %s\n", panelServiceLabel)
			return nil
		},
	}
}

func newPanelServiceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the panel service without removing its registration",
		Long: "Stop the panel service without removing its registration, so `panel service start` " +
			"brings it back with the same settings and it still starts at the next login.",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newPanelServiceManager()
			if err != nil {
				return err
			}
			if err := manager.Stop(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "panel service stopped: %s\n", panelServiceLabel)
			return nil
		},
	}
}

func newPanelServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether the panel service is registered and running",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := newPanelServiceManager()
			if err != nil {
				return err
			}
			status, err := manager.Status()
			if err != nil {
				return err
			}
			writePanelServiceStatus(cmd.OutOrStdout(), status)
			return nil
		},
	}
}

// panelServiceSpecFrom turns the flags a user typed into an absolute,
// login-time-safe service definition.
func panelServiceSpecFrom(serve panelServeFlags) (panelServiceSpec, error) {
	binary, err := resolvePanelServiceBinary()
	if err != nil {
		return panelServiceSpec{}, err
	}

	dir := serve.workspacePath
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return panelServiceSpec{}, err
		}
		dir = cwd
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return panelServiceSpec{}, fmt.Errorf("panel service: absolute path of workspace %s: %w", dir, err)
	}
	// Catching a bad workspace now beats registering a service that
	// starts at every login only to exit immediately.
	if info, err := os.Stat(dir); err != nil {
		return panelServiceSpec{}, fmt.Errorf("panel service: workspace %s: %w", dir, err)
	} else if !info.IsDir() {
		return panelServiceSpec{}, fmt.Errorf("panel service: workspace %s is not a directory", dir)
	}

	stdoutPath, stderrPath, err := panelServiceLogPaths()
	if err != nil {
		return panelServiceSpec{}, err
	}

	return panelServiceSpec{
		BinaryPath:    binary,
		Host:          serve.host,
		Port:          serve.port,
		WorkspacePath: dir,
		StdoutPath:    stdoutPath,
		StderrPath:    stderrPath,
	}, nil
}

func writePanelServiceStatus(out interface{ Write([]byte) (int, error) }, status panelServiceStatus) {
	fmt.Fprintf(out, "label:      %s\n", panelServiceLabel)
	if status.DefinitionPath != "" {
		fmt.Fprintf(out, "definition: %s\n", status.DefinitionPath)
	}
	fmt.Fprintf(out, "registered: %s\n", yesNo(status.Registered))
	if status.Running {
		fmt.Fprintf(out, "running:    yes (pid %d)\n", status.PID)
	} else {
		fmt.Fprintln(out, "running:    no")
	}
	if status.Running && status.Host != "" && status.Port != "" {
		fmt.Fprintf(out, "address:    http://%s:%s\n", status.Host, status.Port)
	}
	if status.Detail != "" {
		fmt.Fprintf(out, "detail:     %s\n", status.Detail)
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
