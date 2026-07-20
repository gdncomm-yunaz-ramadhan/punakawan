# ADR-0008: Git-tracked YAML stores portable, human-reviewable project knowledge

## Status
Accepted

## Context
Alongside the canonical Dolt store, the plan needs a portable artifact format that travels with the repository, is diffable in code review, and is human-readable for things like workspace definitions, policy files, and semantic browser flows (§7 Durable Knowledge Model; §6 Workspace Model; §12.8 Storage layout).

## Decision
Git-tracked YAML stores portable, human-reviewable project knowledge.

## Consequences
Workspace configuration (`workspace.yaml`), policy (`policy.yaml`), semantic browser flows (`flow.yaml`, `assertions.yaml`, `metadata.yaml`), and other project-level artifacts are stored as Git-tracked YAML under `.punakawan/` rather than only inside Dolt (§6.1 Project directory; §12.8 Storage layout; §12.9 Semantic flow example). This makes core project knowledge reviewable through normal Git workflows and portable across environments, while Dolt remains canonical for the queryable relational graph and JSONL remains the append-only raw record (§7).
