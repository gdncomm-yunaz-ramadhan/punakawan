package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Inspect the discovered workspace",
	}
	cmd.AddCommand(newWorkspaceShowCmd())
	return cmd
}

func newWorkspaceShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the discovered workspace and its repositories",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			a, err := app.Load(cwd)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "workspace: %s (%s)\n", a.Workspace.ID, a.Workspace.Name)
			fmt.Fprintf(out, "root: %s\n", a.Workspace.Root)
			fmt.Fprintln(out, "repositories:")
			for _, r := range a.Workspace.Repositories {
				fmt.Fprintf(out, "  - %s (%s) roles=%v\n", r.ID, r.Path, r.Roles)
			}
			if len(a.Workspace.Relations) > 0 {
				fmt.Fprintln(out, "relations:")
				for _, rel := range a.Workspace.Relations {
					fmt.Fprintf(out, "  - %s %s %s\n", rel.From, rel.Type, rel.To)
				}
			}
			return nil
		},
	}
}
