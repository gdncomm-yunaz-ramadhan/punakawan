-- First-class Plan aggregate (punakawan-efficiency-project-hygiene-refactor-plan.md
-- §4, punokawan-pkcd.2): one JSON-encoded row per (id, revision), inserted
-- once and never updated - immutability is structural here, not merely
-- documented in Go. A plan can span multiple projects (project_ids lives
-- inside data), so unlike knowledge_records this table has no project_id
-- partition column; internal/plan filters project_ids at read time.
CREATE TABLE plans (
    id         TEXT NOT NULL,
    revision   INTEGER NOT NULL,
    data       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (id, revision)
);

CREATE INDEX idx_plans_id_revision ON plans (id, revision DESC);
