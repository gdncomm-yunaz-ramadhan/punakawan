# ADR-0002: TypeScript owns adapters, MCP integrations, and browser automation

## Status
Accepted

## Context
Rich document conversion, Atlassian Cloud connectivity, and browser automation are already mature in the Node/TypeScript ecosystem (Docling MCP, Atlassian Rovo MCP, Playwright), and the plan's guiding principle is to orchestrate rather than reimplement these providers (§2.1 Orchestrate rather than reimplement). TypeScript is therefore designated as the integration and browser ecosystem (§3.2 TypeScript responsibilities).

## Decision
TypeScript owns adapters, MCP integrations, and browser automation.

## Consequences
TypeScript owns the adapter SDK, MCP client/server integration, Jira/Confluence normalization, Docling result normalization, the Playwright MCP integration and human-guided browser recorder, locator and semantic path generation, Playwright test generation, OpenAPI parsing helpers, and GitHub/GitLab/CI-provider adapters (§3.2). This isolates volatile third-party integration surface from the trusted Go core, and keeps the Go core from re-implementing domain-specific integration logic (§24 Risk: Go core becomes a duplicate business-logic implementation).
