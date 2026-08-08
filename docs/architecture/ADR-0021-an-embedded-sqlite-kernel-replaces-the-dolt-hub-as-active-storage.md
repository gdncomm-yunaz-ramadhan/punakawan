# ADR-0021: An embedded SQLite kernel replaces the Dolt hub as active storage

## Status
Accepted (supersedes [ADR-0020](./ADR-0020-a-single-shared-dolt-hub-server-replaces-per-project-servers.md); by extension retires the still-live parts of [ADR-0019](./ADR-0019-per-project-dolt-servers-are-bounded-by-an-lru-runtime-pool.md), [ADR-0018](./ADR-0018-punakawan-managed-dolt-is-the-beads-less-fallback-task-graph.md), [ADR-0005](./ADR-0005-dolt-is-the-canonical-knowledge-store.md))

## Context
punokawan-14yn (the continuous multi-project delivery orchestrator) requires one daemon-owned metadata store for delivery orchestration state — projects, lanes, worker leases, evidence, review conclusions — shared safely across concurrent projects and workers in a single process. ADR-0020's shared Dolt hub meets the "one process, no per-project boot" requirement, but Dolt itself brings costs that only matter once storage is on the hot path of every delivery lane rather than an occasional knowledge query:

- **A supervised subprocess and a MySQL wire connection for every access.** `internal/knowledge/dolt.go` opens `sql.Open` over TCP to a child `dolt sql-server`, with a 500ms per-query timeout and a hand-rolled `PROCESSLIST`-based refcount to decide when it's safe to stop the process (`dolt.go:437-482`). This is inherent process- and protocol-overhead the delivery orchestrator's worker scheduler (punokawan-14yn.3) would pay on every state transition.
- **No CGO-free, single-binary story.** The Dolt CLI is installed separately in CI (`.github/workflows/ci.yml`) and expected on the host; there is no embedded, dependency-free path to ship one binary that owns its own storage.
- **No built-in migration integrity guarantee.** `taskstore.go`'s `Migrate()` runs `CREATE TABLE IF NOT EXISTS` with no checksum or version tracking — safe for its current narrow use, insufficient for a growing set of orchestrator tables across daemon upgrades.

None of ADR-0020's own justifications (panel cold-start latency, cross-project query) are lost by this change: `modernc.org/sqlite` is pure Go (no CGO, no subprocess, no network listener — verified buildable with `CGO_ENABLED=0` on darwin, linux, and windows), opens in-process in low single-digit milliseconds, and a daemon holding one open `*sql.DB` serves every project without a boot per switch. Cross-project queries become plain SQL over tables scoped by a `project_id` column instead of database-qualified identifiers — no regex-validated identifier interpolation (`internal/knowledge/crossproject.go`) required.

## Decision
Replace Dolt as the *active* storage engine with an embedded SQLite kernel (`internal/storage`), owned by exactly one daemon process per OS user:

- **One local file, two connection pools.** A single serialized writer (`SetMaxOpenConns(1)`) and a bounded reader pool (four connections) over the same WAL-mode database file. No subprocess, no listener, no network protocol.
- **Fixed durability posture, not configurable per caller.** `journal_mode=WAL`, `synchronous=FULL`, `foreign_keys=ON`, `busy_timeout=5000` are set once, in the DSN, for every connection — callers cannot silently weaken them.
- **Migrations are checksummed and monotonic.** Applying, rejecting an unknown version, a newer-than-known version, or a modified already-applied migration are all decided before any write touches the database.
- **Every domain write is transactional and audited.** `DB.Write` requires an idempotency key, commits the caller's mutation and an `audit_log` row in one transaction, and turns a duplicate key into a no-op rather than a replay.
- **Unsafe locations are rejected at open time**, not discovered later as file-locking corruption: a network-mounted (NFS/SMB/CIFS/AFP) database directory fails `CheckLocation` with an actionable error instead of silently degrading SQLite's locking guarantees.
- **This ADR covers the kernel only.** Moving `internal/knowledge`, `internal/taskstore`, and `internal/syncqueue` onto it (punokawan-14yn.15), consolidating all active metadata (punokawan-14yn.16), the one-way Dolt→SQLite import (punokawan-14yn.19), and deleting the Dolt runtime (punokawan-14yn.20) are separate, later tasks — this decision does not itself relocate any data.

## Consequences
- **No subprocess or network listener in the storage path.** Every access is an in-process `database/sql` call; there is nothing to boot, supervise, or refcount.
- **CGO_ENABLED=0 builds on darwin, linux, and windows are possible for the first time** for anything that depends on active storage — a prerequisite for shipping punokawan as one binary.
- **Cross-project access is a `WHERE project_id = ?` clause, not a database-qualified identifier the driver must validate against injection.**
- **Git-portability, as ADR-0020 designed it (explicit export/import to `.punakawan/knowledge`), is superseded, not preserved as-is.** A replacement portability story, if needed, is scoped to punokawan-14yn.19's one-way import design, not this ADR.
- **`internal/hub`, `internal/knowledge`, and the Dolt process-lifecycle code in `dolt.go` become migration inputs only.** They are not deleted by this ADR (punokawan-14yn.20 does that once punokawan-14yn.15/16/19 land) but are no longer the target for new active-storage work.
- **The LRU-pool history in ADR-0019 and the hub-vs-per-project trade-off in ADR-0020 remain useful record of *why* per-project Dolt servers were tried and abandoned** — this ADR does not invalidate that history, it closes the chapter it was building toward.
