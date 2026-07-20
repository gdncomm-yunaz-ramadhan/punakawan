package main

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that required tools are installed",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			ok := true
			for _, name := range []string{"git", "rg", "node", "pnpm"} {
				path, err := exec.LookPath(name)
				if err != nil {
					fmt.Fprintf(out, "%-6s MISSING\n", name)
					ok = false
					continue
				}
				fmt.Fprintf(out, "%-6s %s\n", name, path)
			}
			if _, err := exec.LookPath("rtk"); err == nil {
				fmt.Fprintln(out, "rtk    (optional, present)")
			} else {
				fmt.Fprintln(out, "rtk    (optional, not installed)")
			}
			if !ok {
				return fmt.Errorf("doctor: one or more required tools are missing")
			}
			return nil
		},
	}
}
