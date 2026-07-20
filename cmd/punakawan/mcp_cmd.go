package main

import (
	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/mcpserver"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run Punakawan's MCP server",
	}
	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve Semar/Gareng/Petruk/Bagong as MCP prompts and tools over stdio (§28)",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadApp()
			if err != nil {
				return err
			}
			defer a.Close()

			return mcpserver.Serve(cmd.Context(), a)
		},
	}
}
