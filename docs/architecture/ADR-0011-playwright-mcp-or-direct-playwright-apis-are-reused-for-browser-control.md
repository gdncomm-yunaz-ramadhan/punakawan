# ADR-0011: Playwright MCP or direct Playwright APIs are reused for browser control

## Status
Accepted

## Context
Punakawan needs to drive real browser flows to support human-guided demonstration and E2E test generation, and the guiding principle of orchestrating rather than reimplementing names Playwright MCP and Playwright Test specifically for browser automation, rather than building a bespoke browser driver (§2.1 Orchestrate rather than reimplement; §12.3 Recorder architecture).

## Decision
Playwright MCP or direct Playwright APIs are reused for browser control.

## Consequences
The recorder architecture routes from the Go core through the TypeScript Playwright adapter into Playwright MCP or the direct Playwright API, which drives an injected recorder script against real browser pages and frames (§12.3). This underlying browser control layer is reused as-is; Punakawan's own value-add is the custom recorder, semantic locator generation, and test generation built on top of it (§12.4–§12.9), and Punakawan must tolerate and adapt to upstream Playwright/MCP behavior changes through adapter normalization and contract tests (§24 Risk: External MCP tool behavior changes).
