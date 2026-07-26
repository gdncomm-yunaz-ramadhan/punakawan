# ADR-0019: Per-project Dolt servers are bounded by an LRU runtime pool

## Status
Accepted (relates to [ADR-0005](./ADR-0005-dolt-is-the-canonical-knowledge-store.md), [ADR-0008](./ADR-0008-git-tracked-yaml-stores-portable-human-reviewable-project-knowledge.md), [ADR-0018](./ADR-0018-punakawan-managed-dolt-is-the-beads-less-fallback-task-graph.md))

## Context
Each project's knowledge (and its Beads-less fallback task store, ADR-0018) is a Dolt database stored *inside that project* at `<project>/.punakawan/knowledge`, colocated with the repo so it stays portable and reviewable per project (ADR-0005, ADR-0008). Opening it (`app.OpenKnowledge`) starts a `dolt sql-server` bound to that one data directory and connects over the MySQL wire protocol.

Because a `dolt sql-server` binds a single `--data-dir`, there is one server per project workspace, not one shared server. The server is already deduplicated *across OS processes* — the first process to open a project records the port in `.dolt/sql-server.info`, and later processes (CLI, `mcp serve`, panel) reuse it rather than starting a second. So the process count reflects the number of *distinct projects* touched, not the number of Punakawan invocations.

The panel makes this visible: browsing across projects (overview, tasks, knowledge, approvals) acquires a per-project `*app.App` from a runtime pool, and each distinct project that actually hits Dolt boots its own server. Left unbounded, N projects browsed leaves up to N resident MySQL-wire server processes.

A single shared server across all projects was considered and rejected: it would require relocating every project's knowledge into one central multi-database directory, which decouples knowledge from its repo and breaks the per-project, git-portable model ADR-0005/ADR-0008 establish. That trade is not worth it for a resource-bounding problem.

## Decision
Keep knowledge per-project and colocated; bound the resource cost with the panel's runtime pool (`internal/panel/runtime`), which already owns each non-primary `*app.App`'s lifecycle:

- **LRU cap.** At most `max_active_runtimes` project runtimes are live at once (default **4**, including the never-evicted primary). Admitting one beyond the cap evicts the least-recently-used *idle, non-primary* runtime; closing its `*app.App` stops that project's `dolt sql-server`. A runtime with outstanding references is never evicted, so the pool may sit briefly over cap rather than pull a server out from under an in-flight request.
- **Idle shutdown.** A periodic sweep closes any non-primary runtime idle past `runtime_idle_timeout_seconds` (default 720s / 12 min).
- **Configurable from the System panel.** Both values persist at `<primary>/.punakawan/panel/settings.json` (`internal/panel/settings`) and are exposed at `GET`/`PATCH /api/v1/system/settings`. A change applies to the live pool at once — lowering the cap evicts idle runtimes immediately (`ProjectRuntimeManager.SetMaxActive`), so freed memory is reclaimed without a restart.

The primary project's server is exempt: it is owned by the panel command for the panel's whole lifetime.

## Consequences
- **Bounded, predictable footprint.** Resident Dolt servers from panel browsing are capped and reclaimed on idle. Killing a server is always safe — data is on disk and the server restarts on demand.
- **Design preserved.** Knowledge stays per-project, colocated, and git-portable (ADR-0005/ADR-0008); no central store, no migration, no cross-project coupling.
- **Scope is the panel pool.** The cap governs the panel's runtime pool. A standalone `mcp serve` or one-off CLI process still opens the project(s) it needs (deduplicated per project across processes via `sql-server.info`); it is not subject to the panel's cap because it is not the panel.
- **A cap that is too low costs latency, not correctness.** If more distinct projects are actively used than the cap allows, evicted projects reboot their server on next access. The default of 4 suits typical multi-project browsing; heavy multi-project users raise it in the System panel.
- **Idle timeout vs. cap are independent knobs.** The cap bounds concurrency; the idle timeout bounds how long an unused server lingers. Both are tunable.
