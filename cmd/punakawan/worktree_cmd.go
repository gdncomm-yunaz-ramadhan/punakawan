package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func loadApp() (*app.App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return app.Load(cwd)
}

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage isolated git worktrees for tasks",
	}
	cmd.AddCommand(newWorktreeRequestCmd())
	cmd.AddCommand(newWorktreeApproveCmd())
	cmd.AddCommand(newWorktreeDenyCmd())
	cmd.AddCommand(newWorktreeCreateCmd())
	cmd.AddCommand(newWorktreeRemoveCmd())
	return cmd
}

func newWorktreeRequestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "request <repo-id> <task-id>",
		Short: "Request approval to create a worktree for a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			rec, err := a.Worktrees.RequestApproval("cli", args[0], args[1], protocol.ApprovalRecordRequestedByPetruk)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "approval %s: %s\n", rec.Id, rec.Status)
			return nil
		},
	}
}

func newWorktreeApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <repo-id> <task-id>",
		Short: "Approve a pending worktree-creation request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			return a.Worktrees.Approve(args[0], args[1], "cli-user")
		},
	}
}

func newWorktreeDenyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "deny <repo-id> <task-id>",
		Short: "Deny a pending worktree-creation request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			return a.Worktrees.Deny(args[0], args[1], "cli-user")
		},
	}
}

func newWorktreeCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <repo-id> <task-id>",
		Short: "Create an isolated worktree for an approved task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, repoPath, err := loadAppForRepo(args[0])
			if err != nil {
				return err
			}
			wt, err := a.Worktrees.Create(cmd.Context(), a.Workspace.Root, repoPath, args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "worktree: %s\nbranch: %s\n", wt.Path, wt.Branch)
			return nil
		},
	}
}

func newWorktreeRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <repo-id> <task-id>",
		Short: "Remove a task's worktree",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, repoPath, err := loadAppForRepo(args[0])
			if err != nil {
				return err
			}
			wt := &gitops.Worktree{
				Path:   filepath.Join(a.Workspace.Root, ".punakawan", "worktrees", args[0], args[1]),
				Branch: "punakawan/" + args[1],
			}
			return a.Worktrees.Remove(cmd.Context(), repoPath, wt)
		},
	}
}
