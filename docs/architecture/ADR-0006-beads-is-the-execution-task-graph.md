# ADR-0006: Beads is the execution task graph

## Status
Accepted

## Context
An approved implementation plan must become dependency-aware, executable work items across multiple repositories, while Jira remains the human-facing tracker; the plan needs a local, detailed execution graph that captures task dependencies, scope, and evidence requirements (§10 Beads Task Generation, §10.3 Task contract).

## Decision
Beads is the execution task graph.

## Consequences
Jira stays the human-facing tracker (mapped via `jira_key`/`beads_epic`) while Beads holds the detailed dependency graph that Petruk executes one task at a time in isolated worktrees (§10.1, §10.2, §11 Petruk Execution Runtime). Every Beads task must carry a stable ID, parent requirement, affected repository, dependencies, acceptance criteria, test requirements, required evidence, risk classification, and approval requirements, and Petruk must not silently expand scope — newly discovered work becomes a separate `discovered-from` task reviewed by Semar (§10.3, §10.4).
