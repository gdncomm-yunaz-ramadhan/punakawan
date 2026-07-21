# ADR-0017: Direct Atlassian REST replaces Rovo MCP

## Status
Accepted

## Context
The Jira-first MVP needs predictable issue reads, JQL search, issue creation and edits, comments, worklogs, metadata, and transitions. Routing those operations through the hosted Rovo MCP adds a separate organization-level permission layer and makes official Jira tools disappear from `tools/list` when that layer is unavailable. Jira and Confluence already expose supported REST APIs for these operations using Atlassian API tokens.

## Decision
The Atlassian adapter calls Jira Cloud REST API v3 and Confluence REST directly. Unscoped personal tokens use the site URL with Basic `email:token` authentication. Scoped personal tokens and service-account tokens use the Atlassian API gateway with the resolved cloud ID. The adapter's Punakawan operation names and approval declarations remain stable.

## Consequences
Rovo MCP configuration and tool discovery are no longer required. Access is controlled by token scopes, product access, and the Jira/Confluence permissions of the token account. The adapter owns explicit endpoint and Atlassian Document Format mapping, which is covered by request-level contract tests. All writes continue through Punakawan's approval gate.
