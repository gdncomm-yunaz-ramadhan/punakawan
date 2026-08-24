ALTER TABLE jira_hook_dispatch RENAME TO jira_hook_dispatch_legacy;

CREATE TABLE jira_hook_dispatch (
    delivery_id TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    revision    INTEGER NOT NULL,
    entity_id   TEXT NOT NULL DEFAULT '',
    issue_key   TEXT NOT NULL,
    fired_at    TEXT NOT NULL,
    PRIMARY KEY (delivery_id, event_type, revision, entity_id)
);

INSERT INTO jira_hook_dispatch (delivery_id, event_type, revision, entity_id, issue_key, fired_at)
SELECT delivery_id, event_type, revision, '', issue_key, fired_at
FROM jira_hook_dispatch_legacy;

DROP TABLE jira_hook_dispatch_legacy;
