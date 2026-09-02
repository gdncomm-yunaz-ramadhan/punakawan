package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ygrip/punakawan/internal/app"
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
	var httpAddr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve Punakawan's focused project/workflow/plan/delivery tools over stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := loadAppOptional()
			if err != nil {
				return err
			}
			defer a.Close()

			if httpAddr == "" {
				return mcpserver.Serve(cmd.Context(), a)
			}
			return mcpserver.ServeHTTP(cmd.Context(), a, normalizeHTTPAddr(httpAddr))
		},
	}
	cmd.Flags().StringVar(&httpAddr, "http", "", "serve over Streamable HTTP at this address instead of stdio (e.g. 127.0.0.1:7777, or a bare port to bind loopback-only); this slice adds no authentication layer, so binding beyond loopback without a reverse proxy/auth in front is your own risk to accept")
	return cmd
}

// normalizeHTTPAddr defaults --http to loopback-only when the caller
// supplied a bare port (e.g. "7777"): every other form (already carrying a
// host, or an empty host as in ":7777" - an explicit choice to bind every
// interface) is passed through unchanged.
func normalizeHTTPAddr(addr string) string {
	if _, err := strconv.Atoi(strings.TrimSpace(addr)); err == nil {
		return "127.0.0.1:" + strings.TrimSpace(addr)
	}
	return addr
}

// loadAppOptional is loadApp, except that starting outside any project is
// not an error - the MCP server is meant to be one project-independent
// process a client attaches to before naming any project, unlike every
// other CLI command here, which is inherently scoped to a checkout and
// should keep failing fast via loadApp.
func loadAppOptional() (*app.App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return app.LoadOptional(cwd)
}
