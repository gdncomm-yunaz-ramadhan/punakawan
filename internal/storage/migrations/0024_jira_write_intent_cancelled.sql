ALTER TABLE jira_write_intents RENAME TO jira_write_intents_legacy;

CREATE TABLE jira_write_intents (
    id TEXT PRIMARY KEY, case_id TEXT NOT NULL, execution_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '', jira_issue_key TEXT NOT NULL, action TEXT NOT NULL,
    payload TEXT NOT NULL, idempotency_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'retrying', 'succeeded', 'failed', 'cancelled')),
    attempt_count INTEGER NOT NULL DEFAULT 0, retry_at TEXT, last_error TEXT NOT NULL DEFAULT '',
    external_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);
INSERT INTO jira_write_intents SELECT * FROM jira_write_intents_legacy;
DROP TABLE jira_write_intents_legacy;
CREATE INDEX jira_write_intents_retry_idx ON jira_write_intents(status, retry_at, created_at);
