CREATE TABLE delivery_plan_links (
    orchestration_id TEXT NOT NULL,
    project_id       TEXT NOT NULL,
    plan_id          TEXT NOT NULL,
    plan_revision    INTEGER NOT NULL CHECK (plan_revision > 0),
    created_at       TEXT NOT NULL,
    PRIMARY KEY (orchestration_id, project_id, plan_id, plan_revision),
    FOREIGN KEY (project_id) REFERENCES delivery_projects(id)
);
CREATE INDEX delivery_plan_links_orchestration_created_idx
    ON delivery_plan_links(orchestration_id, created_at, project_id, plan_id);

ALTER TABLE delivery_sessions ADD COLUMN worktree_path TEXT NOT NULL DEFAULT '';
ALTER TABLE delivery_sessions ADD COLUMN provider TEXT NOT NULL DEFAULT '';
