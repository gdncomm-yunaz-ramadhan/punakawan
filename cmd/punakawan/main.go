// Command punakawan is the Punakawan CLI entrypoint, per
// punakawan-go-typescript-detailed-plan.md §3.1, §22 Milestone 1.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "punakawan",
		Short:         "Punakawan delivery orchestration",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newWorkspaceCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newPanelCmd())
	root.AddCommand(newDaemonCmd())
	return root
}
