package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
)

func loadAppForRepo(repoID string) (*app.App, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	a, err := app.Load(cwd)
	if err != nil {
		return nil, "", err
	}
	repoPath, err := a.RepoPath(repoID)
	if err != nil {
		return nil, "", err
	}
	return a, repoPath, nil
}

func newGitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git",
		Short: "Inspect a workspace repository without writing to it",
	}
	cmd.AddCommand(newGitStatusCmd())
	cmd.AddCommand(newGitLogCmd())
	cmd.AddCommand(newGitDiffCmd())
	cmd.AddCommand(newGitBranchCmd())
	return cmd
}

func newGitStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <repo-id>",
		Short: "Show a repository's status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, repoPath, err := loadAppForRepo(args[0])
			if err != nil {
				return err
			}
			status, err := a.Inspector.Status(cmd.Context(), repoPath)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "branch: %s\n", status.Branch)
			fmt.Fprintf(out, "clean: %v\n", status.Clean)
			for _, f := range status.ChangedFiles {
				fmt.Fprintf(out, "  %s\n", f)
			}
			return nil
		},
	}
}

func newGitLogCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "log <repo-id>",
		Short: "Show a repository's recent commits",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, repoPath, err := loadAppForRepo(args[0])
			if err != nil {
				return err
			}
			commits, err := a.Inspector.Log(cmd.Context(), repoPath, limit)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			for _, c := range commits {
				fmt.Fprintf(out, "%s %s %s %s\n", c.SHA[:min(8, len(c.SHA))], c.Date.Format("2006-01-02"), c.Author, c.Subject)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "maximum number of commits to show")
	return cmd
}

func newGitDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <repo-id> [ref]",
		Short: "Show a repository's diff against ref (default: working tree vs HEAD)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, repoPath, err := loadAppForRepo(args[0])
			if err != nil {
				return err
			}
			ref := ""
			if len(args) == 2 {
				ref = args[1]
			}
			diff, err := a.Inspector.Diff(cmd.Context(), repoPath, ref)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), diff)
			return nil
		},
	}
}

func newGitBranchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "branch <repo-id>",
		Short: "Show a repository's current branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, repoPath, err := loadAppForRepo(args[0])
			if err != nil {
				return err
			}
			branch, err := a.Inspector.CurrentBranch(cmd.Context(), repoPath)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), branch)
			return nil
		},
	}
}
