DROP TABLE IF EXISTS approvals;

PRAGMA defer_foreign_keys = ON;

CREATE TABLE jira_assessments_new (
    id           TEXT PRIMARY KEY,
    case_id      TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    snapshot_id  TEXT NOT NULL DEFAULT '',
    clarity      TEXT NOT NULL CHECK (clarity IN ('clear', 'needs_clarification')),
    rationale    TEXT NOT NULL,
    assessed_at  TEXT NOT NULL,
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);

INSERT INTO jira_assessments_new
    (id, case_id, execution_id, session_id, snapshot_id, clarity, rationale, assessed_at)
SELECT id, case_id, execution_id, session_id, snapshot_id,
       CASE WHEN clarity = 'blocked' THEN 'needs_clarification' ELSE clarity END,
       rationale, assessed_at
FROM jira_assessments;

DROP TABLE jira_assessments;
ALTER TABLE jira_assessments_new RENAME TO jira_assessments;

CREATE TABLE github_pr_reviews_new (
    id                    TEXT PRIMARY KEY,
    repository            TEXT NOT NULL,
    pull_request_number   INTEGER NOT NULL,
    head_sha              TEXT NOT NULL,
    findings_json         TEXT NOT NULL,
    body                  TEXT NOT NULL,
    verdict               TEXT NOT NULL CHECK (verdict IN ('APPROVE', 'REQUEST_CHANGES', 'COMMENT')),
    status                TEXT NOT NULL CHECK (status IN ('proposed', 'submitted', 'failed')),
    delivery_execution_id TEXT NOT NULL DEFAULT '',
    external_review_id    TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    failure               TEXT NOT NULL DEFAULT ''
);

INSERT INTO github_pr_reviews_new (
    id, repository, pull_request_number, head_sha, findings_json, body, verdict, status,
    delivery_execution_id, external_review_id, created_at, updated_at, failure
)
SELECT id, repository, pull_request_number, head_sha, findings_json, body, verdict,
       CASE WHEN status = 'approved' THEN 'proposed' ELSE status END,
       delivery_execution_id, external_review_id, created_at, updated_at, failure
FROM github_pr_reviews;

DROP INDEX github_pr_reviews_delivery_idx;
DROP TABLE github_pr_reviews;
ALTER TABLE github_pr_reviews_new RENAME TO github_pr_reviews;
CREATE INDEX github_pr_reviews_delivery_idx
    ON github_pr_reviews(delivery_execution_id, created_at);
