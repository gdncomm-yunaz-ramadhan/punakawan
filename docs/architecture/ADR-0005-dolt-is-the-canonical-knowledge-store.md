# ADR-0005: Dolt is the canonical knowledge store

## Status
Accepted

## Context
Punakawan must turn documents, tickets, and observed flows into durable, versioned relational knowledge with strict provenance and validity tracking across many entity types (Requirement, Claim, Decision, APIContract, BrowserFlow, and more) and relations (`DERIVED_FROM`, `IMPLEMENTS`, `SUPERSEDES`, etc.) (§7 Durable Knowledge Model, §7.1 Core entities, §7.2 Core relations).

## Decision
Dolt is the canonical knowledge store.

## Consequences
Dolt holds the relational graph of entities and relations, while Git-tracked YAML and JSONL are used only as portable, human-reviewable artifacts and append-only evidence rather than as the source of truth (§7). Every durable record carries required provenance and validity-state fields (`observed`, `inferred`, `assumed`, `verified`, `disputed`, `superseded`, `invalid`, `stale`), and Semar must not silently promote inferred knowledge to verified fact (§7.3, §7.4). This also underpins the mitigation for agent-generated knowledge becoming stale or false, since Dolt-backed provenance, source version, and content hash enable explicit verification and staleness checks (§24 Risk: Agent-generated knowledge becomes stale or false).
