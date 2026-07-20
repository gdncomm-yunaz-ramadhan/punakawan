# ADR-0014: Petruk works one isolated task at a time

## Status
Accepted

## Context
Implementation tasks must not interfere with each other or with the base repository state, and each task execution should reason over a bounded, fresh context rather than a long-running conversation carried across all tasks (§11 Petruk Execution Runtime, §11.1 Worktree isolation, §11.2 Fresh context per task).

## Decision
Petruk works one isolated task at a time.

## Consequences
For every implementation task, Petruk acquires a workspace lock, confirms a clean base repository, creates a dedicated Git worktree and task branch, executes the smallest valid change, runs targeted checks, records evidence, and commits or marks the task blocked before releasing the worktree (§11.1, §11.3 Execution loop). Each task receives only the task definition, parent requirement, relevant excerpts, related decisions, and required tests as fresh context rather than full history (§11.2), and this worktree isolation is a named mitigation for autonomous code changes damaging repositories (§24 Risk: Autonomous code changes damage repositories).
