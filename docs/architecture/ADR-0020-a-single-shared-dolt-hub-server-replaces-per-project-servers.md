# ADR-0020: A single shared Dolt hub server replaces per-project servers

## Status
Accepted (supersedes [ADR-0019](./ADR-0019-per-project-dolt-servers-are-bounded-by-an-lru-runtime-pool.md); amends [ADR-0005](./ADR-0005-dolt-is-the-canonical-knowledge-store.md), [ADR-0008](./ADR-0008-git-tracked-yaml-stores-portable-human-reviewable-project-knowledge.md), [ADR-0018](./ADR-0018-punakawan-managed-dolt-is-the-beads-less-fallback-task-graph.md))

## Context
ADR-0019 bounded the resource cost of one `dolt sql-server` per project with an LRU runtime pool, and explicitly rejected a single shared server: centralizing would require relocating every project's knowledge out of `<project>/.punakawan/knowledge`, decoupling it from its repo and breaking the git-portable model ADR-0005/ADR-0008 establish. That rejection was evaluated purely against *resource cost* (measured: ~5MB RSS and negligible idle CPU per server — never a real problem).

Two requirements not in view when ADR-0019 was written make the trade-off worth revisiting:

1. **Panel dashboard latency.** Browsing to a project whose server was evicted (or never started) pays a cold `dolt sql-server` boot — measured at ~6.6s to first-ready in this environment. This is a real, user-visible latency cost the LRU pool does not eliminate, only bounds the frequency of.
2. **Cross-project reference.** Each project's knowledge lives in an isolated server/database today. There is no way to query or join across two projects' knowledge without a client stitching together two separate connections.

We verified experimentally (scratch data, not production data) that a single `dolt sql-server --data-dir=<parent>` serves every immediately-nested Dolt repository under `<parent>` as its own named database, and that a standard cross-database `SELECT ... FROM proj_a.t JOIN proj_b.t ...` works natively in one connection, with no schema changes. We also verified that Dolt does **not** follow symlinks when discovering databases under `--data-dir` — a project's data must physically live under the shared parent directory for this to work; pointing the hub at a tree of symlinks back into each project's original location does not work.

## Decision
Run one long-lived `dolt sql-server` process per machine/user ("the hub"), `--data-dir` pointed at a central directory (e.g. `~/.punakawan/hub/`). Each registered project gets one subdirectory/database under the hub, using the existing knowledge/taskstore schema unchanged (ADR-0005/ADR-0018's data model is not migrated, only relocated).

- **Canonical storage moves to the hub.** `<project>/.punakawan/knowledge` stops being the live store. This is a deliberate reversal of ADR-0005/ADR-0008's "colocated in the repo" property, traded for eliminating per-project server boot latency and enabling native cross-project queries.
- **Git-portability is preserved via explicit export/import, not automatic colocation.** An on-demand export command snapshots a project's current hub database into `<project>/.punakawan/knowledge` so it can still be git-tracked and travel with the repo; import seeds a hub entry from that snapshot (e.g. on a new machine, or restoring). Sync is explicit/on-demand by design — the export can go stale between exports, which is an accepted trade against the cost of mirroring every write.
- **Existing projects migrate via a one-time, explicitly-invoked script**, not silently on next open. Nothing relocates without a deliberate command.
- **Cross-project query access is scoped by default.** Rather than adding a new cross-project MCP tool, the existing knowledge query tool(s) gain a project-name filter parameter. Callers (agents) are hardwired to pass their own project's name on every query by default; querying another project's database is an explicit, visible parameter choice, not an ambient capability. This prevents accidental cross-project leakage now that all projects share one connection.
- **ADR-0019's LRU runtime pool becomes unnecessary and is removed.** With one always-warm hub server, there is nothing to evict or reboot per project.

## Consequences
- **Panel dashboard project switches no longer pay a per-project server boot.** One connection, one process, `USE <project db>` or a `WHERE`/database-qualified query instead of a process spawn.
- **Cross-project reference is now possible** via database-qualified SQL within a single connection, gated through the query tool's project-name parameter rather than a separate mechanism.
- **Git-portability becomes an explicit action, not an ambient property.** A project's `.punakawan/knowledge` is a snapshot as of the last export, not a live mirror. Forgetting to export before committing means the git-tracked copy is stale relative to the hub. This is the direct trade for the latency/cross-project wins.
- **A migration step is required for every already-live project** (this project and others already running independent per-project servers) before they benefit from the hub; until migrated, they continue on the old per-project path.
- **Symlinking the hub to existing project directories does not work** (confirmed experimentally) — the migration must physically move/copy data into the hub's directory tree, not merely reference it.
- **The LRU pool (`internal/panel/runtime`) and its cold-start/eviction trade-offs from ADR-0019 are retired**, not layered underneath the hub — one process replaces the pool of up to four.
