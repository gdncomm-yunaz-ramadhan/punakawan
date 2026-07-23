package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/panel/registry"
)

func newWorkspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Inspect the discovered workspace, and manage the panel's workspace registry",
	}
	cmd.AddCommand(newWorkspaceShowCmd())
	cmd.AddCommand(newWorkspaceRegisterCmd())
	cmd.AddCommand(newWorkspaceListCmd())
	cmd.AddCommand(newWorkspaceRemoveCmd())
	cmd.AddCommand(newWorkspacePinCmd())
	cmd.AddCommand(newWorkspaceUnpinCmd())
	return cmd
}

// newWorkspaceRegisterCmd implements §7's `punakawan workspace register
// [path]`: registers path (default ".") in the global panel workspace
// registry, using the workspace's own id/name from .punakawan/workspace.yaml.
func newWorkspaceRegisterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "register [path]",
		Short: "Register a workspace with the panel's global workspace registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			a, err := app.Load(path)
			if err != nil {
				return err
			}
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			entry, err := reg.Register(a.Workspace.ID, a.Workspace.Root, a.Workspace.Name, time.Now().UTC())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "registered %s at %s\n", entry.Id, entry.Path)
			return nil
		},
	}
}

func newWorkspaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workspaces registered with the panel",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			entries, err := reg.List()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintln(out, "no workspaces are registered")
				return nil
			}
			for _, e := range entries {
				pinned := ""
				if e.Pinned != nil && *e.Pinned {
					pinned = " (pinned)"
				}
				name := e.Id
				if e.DisplayName != nil && *e.DisplayName != "" {
					name = *e.DisplayName
				}
				fmt.Fprintf(out, "%s\t%s\t%s%s\n", e.Id, e.Path, name, pinned)
			}
			return nil
		},
	}
}

func newWorkspaceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a workspace from the panel's registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			return reg.Remove(args[0])
		},
	}
}

func newWorkspacePinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pin <id>",
		Short: "Pin a registered workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			return reg.SetPinned(args[0], true)
		},
	}
}

func newWorkspaceUnpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <id>",
		Short: "Unpin a registered workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := registry.Open()
			if err != nil {
				return err
			}
			return reg.SetPinned(args[0], false)
		},
	}
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
