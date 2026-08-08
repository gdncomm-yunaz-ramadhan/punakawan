package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/ygrip/punakawan/internal/daemon"
)

func newDaemonCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the Punakawan daemon (punokawan-14yn.17)",
	}
	root.AddCommand(newDaemonStartCmd())
	root.AddCommand(newDaemonStatusCmd())
	root.AddCommand(newDaemonStopCmd())
	return root
}

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon if it is not already running",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths()
			if err != nil {
				return err
			}
			client, err := daemon.EnsureRunning(cmd.Context(), paths)
			if err != nil {
				return err
			}
			if err := client.Healthy(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "daemon running")
			return nil
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether the daemon is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths()
			if err != nil {
				return err
			}
			running, pid, err := daemon.Status(paths.LockPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !running {
				fmt.Fprintln(out, "not running")
				os.Exit(1)
			}
			fmt.Fprintln(out, "running, pid", pid)
			return nil
		},
	}
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Gracefully stop the running daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := daemon.DefaultPaths()
			if err != nil {
				return err
			}
			running, pid, err := daemon.Status(paths.LockPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if !running {
				fmt.Fprintln(out, "not running")
				return nil
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("signal pid %d: %w", pid, err)
			}
			fmt.Fprintln(out, "sent stop signal to pid", pid)
			return nil
		},
	}
}
