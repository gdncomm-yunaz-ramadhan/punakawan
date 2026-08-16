# ADR-0018: Punakawan-managed Dolt is the Beads-less fallback task graph

## Status
Accepted (qualifies [ADR-0006](./ADR-0006-beads-is-the-execution-task-graph.md)); storage backend corrected by [ADR-0021](./ADR-0021-an-embedded-sqlite-kernel-replaces-the-dolt-hub-as-active-storage.md) — the fallback task store (`internal/taskstore`) is backed by the embedded SQLite kernel, not a per-project Dolt database or shared `sql-server` connection. The decision below to use a Punakawan-managed fallback instead of Beads remains current; only the storage engine it runs on has changed.

## Context
[ADR-0006](./ADR-0006-beads-is-the-execution-task-graph.md) makes Beads the execution task graph. In practice Beads requires a `.beads/` directory in the project, created by `bd init` — which also runs `git init` on the directory and writes `AGENTS.md`, a `CLAUDE.md`, and Claude Code hooks. Many projects a user points Punakawan at have no `.beads/` (and should not be reconfigured that invasively just to be tracked). Before this change, task reads for such a project errored (the panel's `/tasks` and `/task-graph` returned 500) and `submit_task_graph` could not persist, so tasks and plans were simply unavailable there.

We want tasks/plans tracked for any loadable project, without mutating the project.

## Decision
When a project has no `.beads/` directory, Punakawan tracks its tasks and plans in a Punakawan-managed Dolt store instead of Beads. Beads remains primary and unchanged wherever a project is Beads-initialized.

The fallback store (`internal/taskstore`) reuses the existing per-project Punakawan Dolt database (the same engine as `internal/knowledge`, sharing its `sql-server` connection and lifecycle) rather than cloning that lifecycle or standing up a second server. Its `tasks`/`task_deps` tables live in `.punakawan/` alongside the knowledge store. The selection seam is `beads.ProjectInitialized(root)` — a pure filesystem check for `.beads/`, walking up exactly as `bd` resolves its own store.

## Consequences
- **Zero project mutation.** The fallback path writes only under `.punakawan/` (already Punakawan's own directory, like the knowledge store). It never runs `git init`, and never writes `AGENTS.md`, `CLAUDE.md`, or Claude Code hooks. That invasiveness is the reason `bd init` is not auto-run.
- **Contract parity.** `taskstore` emits the same shapes the panel already consumes (`beads.ReadyIssue`, `beads.Issue`, and the ready-set fed to `tasksnapshot.BuildSnapshot`), so the board, dependency graph, and counts are identical in shape regardless of backend. The read seam (`internal/panel/sources.TaskSource`) and write seam (`internal/tasks.GenerateGraph`) branch on `beads.ProjectInitialized`; the Beads path is byte-for-byte unchanged.
- **Two backends, one primary.** Beads stays the canonical, syncable execution graph (ADR-0006) with all its guarantees (`refs/dolt/data` sync, `bd` tooling, stable IDs). The fallback is a local convenience for non-Beads projects; its IDs are Punakawan-generated (`pkt-…`) and it is not Jira-mapped or remote-synced. A project that later runs `bd init` uses Beads from then on; the fallback store is not migrated into Beads automatically.
- **Requires a loadable workspace.** The fallback presumes the project is discoverable (a git repository or a `.punakawan/workspace.yaml`); a directory that is neither still degrades to "unavailable" rather than falling back.
- **Shared Dolt database.** Task tables coexist with knowledge in one per-project Punakawan Dolt DB. This is an internal implementation detail (the DB is not a human repository); it trades strict separation for reuse of a single, carefully-managed `sql-server` lifecycle.
