-- Learning proposals (internal/learning), moved off the per-workspace
-- append-only JSONL file onto the shared SQLite kernel (punokawan-14yn.16).
-- History is append-only: each state change (a new proposal, a dedup support
-- bump, an accept/reject) inserts a new row carrying the same id rather than
-- mutating the original, so List folds to the latest row per id (later seq
-- wins) while the raw history is preserved.
--
-- Because the kernel is one database shared by every local project checkout,
-- every row is scoped by project_id so identical proposal ids minted by two
-- different projects can never collide or leak into each other's queries.
CREATE TABLE learning_proposals (
    seq        INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL,
    id         TEXT NOT NULL,
    data       TEXT NOT NULL
);

CREATE INDEX learning_proposals_project_seq_idx ON learning_proposals (project_id, seq);
CREATE INDEX learning_proposals_project_id_idx ON learning_proposals (project_id, id);
