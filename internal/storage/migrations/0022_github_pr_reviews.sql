CREATE TABLE github_pr_reviews (
    id TEXT PRIMARY KEY,
    repository TEXT NOT NULL,
    pull_request_number INTEGER NOT NULL,
    head_sha TEXT NOT NULL,
    findings_json TEXT NOT NULL,
    body TEXT NOT NULL,
    verdict TEXT NOT NULL CHECK (verdict IN ('APPROVE', 'REQUEST_CHANGES', 'COMMENT')),
    status TEXT NOT NULL CHECK (status IN ('proposed', 'approved', 'submitted', 'failed')),
    delivery_execution_id TEXT NOT NULL DEFAULT '',
    external_review_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX github_pr_reviews_delivery_idx ON github_pr_reviews(delivery_execution_id, created_at);
