CREATE TABLE plan_revisions (
    plan_id    TEXT NOT NULL,
    revision   INTEGER NOT NULL CHECK (revision > 0),
    data       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (plan_id, revision)
);

INSERT INTO plan_revisions(plan_id, revision, data, created_at)
SELECT id, revision, data, created_at FROM plans;

CREATE TABLE IF NOT EXISTS migration_warnings (
    migration_version INTEGER NOT NULL,
    entity_kind       TEXT NOT NULL,
    entity_key        TEXT NOT NULL,
    warning           TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (migration_version, entity_kind, entity_key)
);

INSERT INTO migration_warnings(migration_version, entity_kind, entity_key, warning)
SELECT 28, 'delivery_plan_link',
       orchestration_id || ':' || project_id || ':' || plan_id || ':' || plan_revision,
       'link omitted because its project or exact plan revision does not exist'
FROM delivery_plan_links AS link
WHERE NOT EXISTS (SELECT 1 FROM delivery_projects AS project WHERE project.id = link.project_id)
   OR NOT EXISTS (
       SELECT 1 FROM plan_revisions AS plan
       WHERE plan.plan_id = link.plan_id AND plan.revision = link.plan_revision
   );

CREATE TABLE delivery_plan_links_new (
    orchestration_id TEXT NOT NULL,
    project_id       TEXT,
    project_scope_key TEXT NOT NULL,
    plan_id          TEXT NOT NULL,
    plan_revision    INTEGER NOT NULL,
    scope            TEXT NOT NULL CHECK (scope IN ('delivery', 'project')),
    created_at       TEXT NOT NULL,
    PRIMARY KEY (orchestration_id, scope, project_scope_key, plan_id, plan_revision),
    FOREIGN KEY (project_id) REFERENCES delivery_projects(id),
    FOREIGN KEY (plan_id, plan_revision) REFERENCES plan_revisions(plan_id, revision),
    CHECK (
        (scope = 'delivery' AND project_id IS NULL AND project_scope_key = '')
        OR
        (scope = 'project' AND project_id IS NOT NULL AND project_scope_key = project_id)
    )
);

INSERT INTO delivery_plan_links_new (
    orchestration_id, project_id, project_scope_key, plan_id, plan_revision, scope, created_at
)
SELECT link.orchestration_id, link.project_id, link.project_id,
       link.plan_id, link.plan_revision, 'project', link.created_at
FROM delivery_plan_links AS link
JOIN delivery_projects AS project ON project.id = link.project_id
JOIN plan_revisions AS plan
  ON plan.plan_id = link.plan_id AND plan.revision = link.plan_revision;

DROP TABLE delivery_plan_links;
DROP TABLE plans;
ALTER TABLE delivery_plan_links_new RENAME TO delivery_plan_links;
CREATE INDEX delivery_plan_links_project_created_idx
    ON delivery_plan_links(project_id, created_at);
CREATE INDEX delivery_plan_links_orchestration_scope_idx
    ON delivery_plan_links(orchestration_id, scope);

ALTER TABLE jira_work_item_mappings ADD COLUMN first_touched_at TEXT NOT NULL DEFAULT '';
ALTER TABLE jira_work_item_mappings ADD COLUMN last_touched_at TEXT NOT NULL DEFAULT '';
ALTER TABLE jira_work_item_mappings ADD COLUMN touch_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE jira_work_item_touches (
    mapping_id   TEXT NOT NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    tool_call_id TEXT NOT NULL,
    touched_at   TEXT NOT NULL,
    PRIMARY KEY (mapping_id, session_id, tool_call_id),
    FOREIGN KEY (mapping_id) REFERENCES jira_work_item_mappings(id)
);
