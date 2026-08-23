package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/gitops"
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
	cmd.AddCommand(newWorktreeCreateCmd())
	cmd.AddCommand(newWorktreeRemoveCmd())
	return cmd
}

func newWorktreeCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <repo-id> <task-id>",
		Short: "Create an isolated worktree for a task",
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
			path, err := gitops.WorktreePath(args[0], args[1])
			if err != nil {
				return err
			}
			wt := &gitops.Worktree{
				Path:   path,
				Branch: "punakawan/" + args[1],
			}
			return a.Worktrees.Remove(cmd.Context(), repoPath, wt)
		},
	}
}
