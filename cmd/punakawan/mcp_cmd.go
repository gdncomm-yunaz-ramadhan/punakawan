package main

import (
	"log/slog"
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
			a, err := loadApp()
			if err != nil {
				return err
			}
			defer a.Close()

			primeModelRates(cmd.Context(), slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil)))

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

// loadApp wires up the app against whatever the current directory turns
// out to be. Starting outside any project is ordinary here: the MCP server
// is one project-independent process a client attaches to before naming
// any project.
func loadApp() (*app.App, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return app.Load(cwd)
}
