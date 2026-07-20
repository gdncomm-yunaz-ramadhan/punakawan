# ADR-0010: Atlassian MCP is reused for Jira and Confluence Cloud

## Status
Accepted

## Context
Jira and Confluence are the primary sources of requirements and documentation context, and the guiding principle of orchestrating rather than reimplementing calls out the Atlassian Rovo MCP specifically for Jira and Confluence Cloud (§2.1 Orchestrate rather than reimplement; §13.2 Jira and Confluence).

## Decision
Atlassian MCP is reused for Jira and Confluence Cloud.

## Consequences
Punakawan uses the official Atlassian MCP for Cloud where possible, normalizing Jira issues and Confluence pages while preserving external IDs and versions, drafting clarification comments, applying policy before writes, linking external records to internal knowledge, and detecting stale cached content (§13.2). Fallback adapters may be needed for Data Center deployments, and all external writes go through the approval model (§13.2; §16 Approval Model), which also supports the mitigation for external MCP tool behavior changes via capability probing, provider version recording, and contract tests (§24 Risk: External MCP tool behavior changes).
