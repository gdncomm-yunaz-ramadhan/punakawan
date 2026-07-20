# ADR-0001: Go owns the trusted runtime

## Status
Accepted

## Context
Punakawan performs sensitive operations — process supervision, capability enforcement, approval gates, secret leasing, filesystem boundaries, and Git worktree lifecycle — that must run in a single trusted core rather than be scattered across integration code (§3 High-Level Architecture, §3.1 Go responsibilities).

## Decision
Go owns the trusted runtime.

## Consequences
The Go core is the CLI/daemon lifecycle owner, workflow state machine, role orchestrator, tool supervisor, and audit logger, while all integration and browser-automation logic is kept out of it (§3.1). This keeps the trusted surface small and auditable and avoids Go becoming a duplicate business-logic implementation, per the corresponding risk mitigation: keep browser and integration logic in TypeScript and keep Go focused on trusted orchestration, policy, process, workspace, and recovery (§24 Risk: Go core becomes a duplicate business-logic implementation).
