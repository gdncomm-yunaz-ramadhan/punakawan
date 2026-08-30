PRAGMA defer_foreign_keys = ON;

CREATE TABLE delivery_cases_new (
    id              TEXT PRIMARY KEY,
    source_kind     TEXT NOT NULL CHECK (source_kind IN ('jira', 'adhoc')),
    source_provider TEXT NOT NULL DEFAULT '',
    source_tenant   TEXT NOT NULL DEFAULT '',
    source_key      TEXT,
    jira_issue_key  TEXT,
    status          TEXT NOT NULL CHECK (status IN ('active', 'cancelled')),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    CHECK (
      (source_kind = 'jira' AND source_provider = 'jira' AND source_key IS NOT NULL AND jira_issue_key IS NOT NULL)
      OR
      (source_kind = 'adhoc' AND source_key IS NULL AND jira_issue_key IS NULL)
    )
);

INSERT INTO delivery_cases_new
    (id, source_kind, source_provider, source_tenant, source_key, jira_issue_key, status, created_at, updated_at)
SELECT id, 'jira', 'jira', '', jira_source_key, jira_issue_key,
       CASE WHEN status = 'closed' THEN 'cancelled' ELSE 'active' END,
       created_at, updated_at
FROM delivery_cases;

DROP TABLE delivery_cases;
ALTER TABLE delivery_cases_new RENAME TO delivery_cases;

CREATE UNIQUE INDEX delivery_cases_active_source_idx
ON delivery_cases(source_provider, source_tenant, source_key)
WHERE source_kind = 'jira' AND status = 'active';

CREATE TABLE delivery_projection_versions (
    orchestration_id TEXT PRIMARY KEY,
    revision         INTEGER NOT NULL DEFAULT 1,
    updated_at       TEXT NOT NULL
);

INSERT INTO delivery_projection_versions(orchestration_id, revision, updated_at)
SELECT orchestration_id, 1, started_at FROM delivery_executions;
