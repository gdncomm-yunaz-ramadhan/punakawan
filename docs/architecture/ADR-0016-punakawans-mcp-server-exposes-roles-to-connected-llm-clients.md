# ADR-0016: Punakawan's own MCP server exposes roles to connected LLM clients

## Status
Accepted

## Context
§8 defines role contracts (Semar, Gareng, Petruk, Bagong) but the plan never specified how a role's reasoning is actually performed — there is no LLM API client anywhere in the architecture (§3.1, §3.2, §13). Building a direct model-API integration would make Punakawan responsible for model selection, keys, and unattended operation; building on a host-agent convention without a protocol boundary would tie Punakawan to one specific client. §5.1 already justifies JSON-RPC-over-stdio for the adapter protocol as easy to inspect, test, and isolate — MCP is a JSON-RPC 2.0 protocol with its own method names, so the same reasoning extends naturally to Punakawan's own outward-facing server.

## Decision
Punakawan runs its own MCP server (`punakawan mcp serve`, Go, official `github.com/modelcontextprotocol/go-sdk`). Any MCP-compatible client connects, fetches a role's prompt, reasons with its own model, and submits structured output back through an MCP tool call that Punakawan validates and persists. Punakawan never calls an LLM API itself.

## Consequences
Evidence and provenance (§2.3, §7.3) stay enforced server-side regardless of which client or model performed the reasoning. Punakawan requires a connected MCP client to do anything role-related — it cannot run role reasoning unattended; that would be a distinct future decision. See §28 for the full design (division of responsibility, why Go rather than the TypeScript adapter layer, and the exposed prompt/tool surface).
