# GitHub Adapter Installation Design

## Goal

Make installed MCP servers load `GITHUB_TOKEN` from the same global `.env` file as Atlassian credentials, and register the built GitHub adapter in the global adapter configuration.

## Root Cause

The registry only creates configured adapters. The macOS installer registers the binary directly with MCP clients and does not write a GitHub adapter entry or launch the server through an environment-loading wrapper. A token exported only in an interactive shell is unavailable to the MCP subprocess.

## Design

The installer builds both direct adapters, adds missing `atlassian` and `github` entries to the user global adapter configuration without replacing existing entries, and passes the global `.env` path to the agent configurator. The configurator creates a launcher that sources that file and forwards MCP arguments to the installed Punakawan binary. Agent registrations and the generic MCP config invoke this launcher.

`github` receives `GITHUB_TOKEN`, `GH_TOKEN`, `GITHUB_API_URL`, and `GITHUB_GRAPHQL_URL` through the adapter allowlist. The installer never logs a credential value.
