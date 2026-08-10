-- Sync-queue entries (internal/syncqueue), moved off the per-workspace
-- append-only JSONL file onto the shared SQLite kernel (punokawan-14yn.16).
-- History is append-only: resolving an entry inserts a new row carrying the
-- same id rather than mutating the original, so Current folds to the latest
-- row per id (ordered by seq) while List returns the full history.
--
-- Because the kernel is one database shared by every local project checkout,
-- every row is scoped by project_id so identical entry ids minted by two
-- different projects can never collide or leak into each other's queries.
CREATE TABLE sync_queue_entries (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL,
    id         TEXT NOT NULL,
    data       TEXT NOT NULL
);

CREATE INDEX sync_queue_entries_project_seq_idx ON sync_queue_entries (project_id, seq);
CREATE INDEX sync_queue_entries_project_id_idx ON sync_queue_entries (project_id, id);
