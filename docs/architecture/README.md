# Architecture Decision Records

This directory contains the Architecture Decision Records (ADRs) formalizing the 15 initial technical decisions from §25 ("Initial Technical Decisions") of `punakawan-go-typescript-detailed-plan.md`.

| ADR | Decision |
| --- | --- |
| [ADR-0001](./ADR-0001-go-owns-the-trusted-runtime.md) | Go owns the trusted runtime. |
| [ADR-0002](./ADR-0002-typescript-owns-adapters-mcp-integrations-and-browser-automation.md) | TypeScript owns adapters, MCP integrations, and browser automation. |
| [ADR-0003](./ADR-0003-json-schema-is-the-canonical-cross-language-contract.md) | JSON Schema is the canonical cross-language contract. |
| [ADR-0004](./ADR-0004-json-rpc-over-stdio-is-the-first-adapter-transport.md) | JSON-RPC over stdio is the first adapter transport. |
| [ADR-0005](./ADR-0005-dolt-is-the-canonical-knowledge-store.md) | Dolt is the canonical knowledge store. |
| [ADR-0006](./ADR-0006-beads-is-the-execution-task-graph.md) | Beads is the execution task graph. |
| [ADR-0007](./ADR-0007-jsonl-is-the-event-and-raw-evidence-format.md) | JSONL is the event and raw evidence format. |
| [ADR-0008](./ADR-0008-git-tracked-yaml-stores-portable-human-reviewable-project-knowledge.md) | Git-tracked YAML stores portable, human-reviewable project knowledge. |
| [ADR-0009](./ADR-0009-docling-is-reused-for-document-parsing.md) | Docling is reused for document parsing. |
| [ADR-0010](./ADR-0010-atlassian-mcp-is-reused-for-jira-and-confluence-cloud.md) | Atlassian MCP is reused for Jira and Confluence Cloud. |
| [ADR-0011](./ADR-0011-playwright-mcp-or-direct-playwright-apis-are-reused-for-browser-control.md) | Playwright MCP or direct Playwright APIs are reused for browser control. |
| [ADR-0012](./ADR-0012-the-punakawan-recorder-is-custom-and-injected-visibly.md) | The Punakawan recorder is custom and injected visibly. |
| [ADR-0013](./ADR-0013-external-writes-require-approval-by-default.md) | External writes require approval by default. |
| [ADR-0014](./ADR-0014-petruk-works-one-isolated-task-at-a-time.md) | Petruk works one isolated task at a time. |
| [ADR-0015](./ADR-0015-bagong-reviews-raw-evidence-independently.md) | Bagong reviews raw evidence independently. |

See `punakawan-go-typescript-detailed-plan.md` §25 for the original decision list and the surrounding sections referenced in each ADR's Context for full rationale.
