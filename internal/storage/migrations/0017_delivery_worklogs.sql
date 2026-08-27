CREATE TABLE delivery_worklogs (
    id                TEXT PRIMARY KEY,
    orchestration_id  TEXT NOT NULL,
    lane_id           TEXT NOT NULL,
    parent_task_id    TEXT NOT NULL DEFAULT '',
    session_id        TEXT NOT NULL DEFAULT '',
    jira_issue_key    TEXT NOT NULL,
    started_at        TEXT NOT NULL,
    duration_seconds  INTEGER NOT NULL CHECK (duration_seconds > 0),
    summary           TEXT NOT NULL,
    sync_status       TEXT NOT NULL DEFAULT 'pending' CHECK (sync_status IN ('pending', 'synced', 'failed')),
    jira_worklog_id   TEXT NOT NULL DEFAULT '',
    synced_at         TEXT,
    created_at        TEXT NOT NULL
);

CREATE INDEX delivery_worklogs_orchestration_created_idx
    ON delivery_worklogs (orchestration_id, created_at, id);
