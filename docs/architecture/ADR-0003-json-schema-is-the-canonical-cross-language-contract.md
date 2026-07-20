# ADR-0003: JSON Schema is the canonical cross-language contract

## Status
Accepted

## Context
Because Go owns the trusted runtime and TypeScript owns adapters (ADR-0001, ADR-0002), the two languages must agree on a language-neutral protocol boundary; the plan calls for this protocol to be generated from JSON Schema rather than hand-maintained in either language (§5 Protocol Boundary Between Go and TypeScript, §5.5 Schema generation).

## Decision
JSON Schema is the canonical cross-language contract.

## Consequences
Go structs and validation helpers, TypeScript interfaces and Zod validators, protocol documentation, compatibility tests, and example payloads are all generated from the JSON Schema files, and CI must reject changes where generated code is stale (§5.5). This directly mitigates the protocol-churn risk between Go and TypeScript by keeping types generated, versioned, and compatibility-tested rather than duplicated by hand (§24 Risk: Protocol churn between Go and TypeScript).
