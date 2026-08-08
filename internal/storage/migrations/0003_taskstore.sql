-- Beads-less fallback task graph (internal/taskstore), moved off the
-- per-project Dolt database onto the shared SQLite kernel (punokawan-14yn.15).
-- Because the kernel is one database shared by every local project checkout,
-- every row is scoped by project_id so identical task ids minted by two
-- different projects can never collide or leak into each other's queries.
CREATE TABLE tasks (
    project_id          TEXT NOT NULL,
    id                  TEXT NOT NULL,
    title               TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL,
    priority            INTEGER NOT NULL,
    issue_type          TEXT NOT NULL,
    owner               TEXT NOT NULL DEFAULT '',
    assignee            TEXT NOT NULL DEFAULT '',
    labels              TEXT NOT NULL DEFAULT '[]',
    parent              TEXT NOT NULL DEFAULT '',
    external_ref        TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    created_by          TEXT NOT NULL DEFAULT '',
    updated_at          TEXT NOT NULL,
    closed_at           TEXT,
    PRIMARY KEY (project_id, id)
);

CREATE TABLE task_deps (
    project_id TEXT NOT NULL,
    from_id    TEXT NOT NULL,
    to_id      TEXT NOT NULL,
    type       TEXT NOT NULL,
    PRIMARY KEY (project_id, from_id, to_id, type),
    FOREIGN KEY (project_id, from_id) REFERENCES tasks (project_id, id),
    FOREIGN KEY (project_id, to_id) REFERENCES tasks (project_id, id)
);
