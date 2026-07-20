# ADR-0004: JSON-RPC over stdio is the first adapter transport

## Status
Accepted

## Context
The protocol boundary must support multiple transports over time (stdio JSON-RPC, HTTP streaming, MCP, Unix domain sockets, and optionally gRPC), but the plan explicitly picks one to start with because it is easy to inspect, test, and isolate, keeping the initial protocol surface small (§5.1 Transport; §24 Risk: Protocol churn between Go and TypeScript, mitigation "small initial message surface").

## Decision
JSON-RPC over stdio is the first adapter transport.

## Consequences
The adapter lifecycle (initialize, health, capabilities, execute, cancel, shutdown, event, evidence, approval_request) is implemented first over stdio JSON-RPC between the Go core and locally spawned adapter processes (§5.2, §5.3). Other transports (HTTP streaming, MCP, Unix sockets, gRPC) remain future options to be added only if justified, which limits early protocol churn and keeps the message surface small while the vertical slice is being built (§5.1; §24 Risk: Too many integrations before a working vertical slice).
