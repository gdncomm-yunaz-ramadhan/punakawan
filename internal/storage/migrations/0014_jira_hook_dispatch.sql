-- Records that a delivery hook has already fired a Jira side effect (a
-- comment, a status transition) for one (delivery_id, event_type, revision)
-- combination, so a retried or re-delivered dispatch of the same delivery
-- event can be recognized as already-handled and skipped instead of
-- posting a duplicate comment or firing a duplicate transition.
CREATE TABLE jira_hook_dispatch (
    delivery_id TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    revision    INTEGER NOT NULL,
    issue_key   TEXT NOT NULL,
    fired_at    TEXT NOT NULL,
    PRIMARY KEY (delivery_id, event_type, revision)
);
