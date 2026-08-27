CREATE TABLE delivery_cases (
    id                  TEXT PRIMARY KEY,
    jira_source_key     TEXT NOT NULL UNIQUE,
    jira_issue_key      TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('active', 'closed')),
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);

CREATE TABLE delivery_executions (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    orchestration_id    TEXT NOT NULL UNIQUE,
    ordinal             INTEGER NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('active', 'completed', 'cancelled')),
    session_id          TEXT NOT NULL DEFAULT '',
    started_at          TEXT NOT NULL,
    ended_at            TEXT,
    UNIQUE (case_id, ordinal),
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);
CREATE INDEX delivery_executions_case_ordinal_idx ON delivery_executions(case_id, ordinal DESC);

CREATE TABLE delivery_sessions (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    orchestration_id    TEXT NOT NULL,
    resumed_from_id     TEXT NOT NULL DEFAULT '',
    participant         TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('active', 'handed_off', 'closed')),
    started_at          TEXT NOT NULL,
    ended_at            TEXT,
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id),
    FOREIGN KEY (execution_id) REFERENCES delivery_executions(id)
);
CREATE INDEX delivery_sessions_execution_started_idx ON delivery_sessions(execution_id, started_at, id);

CREATE TABLE delivery_session_checkpoints (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL,
    sequence            INTEGER NOT NULL,
    summary             TEXT NOT NULL,
    progress_percent    REAL,
    handoff_to          TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    UNIQUE (session_id, sequence),
    FOREIGN KEY (session_id) REFERENCES delivery_sessions(id)
);

CREATE TABLE delivery_usage_ledger (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL,
    entry_kind          TEXT NOT NULL CHECK (entry_kind IN ('estimate', 'actual')),
    category            TEXT NOT NULL,
    model               TEXT NOT NULL DEFAULT '',
    quantity            REAL NOT NULL CHECK (quantity >= 0),
    unit                TEXT NOT NULL,
    unit_price          REAL,
    cost_amount         REAL,
    cost_currency       TEXT NOT NULL DEFAULT '',
    price_source        TEXT NOT NULL DEFAULT '',
    recorded_at         TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES delivery_sessions(id)
);
CREATE INDEX delivery_usage_ledger_case_recorded_idx ON delivery_usage_ledger(case_id, recorded_at, id);

CREATE TABLE delivery_budgets (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL DEFAULT '',
    category            TEXT NOT NULL DEFAULT '',
    amount              REAL NOT NULL CHECK (amount >= 0),
    currency            TEXT NOT NULL,
    created_at          TEXT NOT NULL,
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);

CREATE TABLE jira_source_snapshots (
    id                  TEXT PRIMARY KEY,
    idempotency_key     TEXT NOT NULL UNIQUE,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL DEFAULT '',
    jira_issue_key      TEXT NOT NULL,
    version             INTEGER NOT NULL,
    title               TEXT NOT NULL DEFAULT '',
    body                TEXT NOT NULL DEFAULT '',
    content_hash        TEXT NOT NULL,
    captured_at         TEXT NOT NULL,
    UNIQUE (case_id, version),
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);

CREATE TABLE jira_assessments (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL DEFAULT '',
    snapshot_id         TEXT NOT NULL DEFAULT '',
    clarity             TEXT NOT NULL CHECK (clarity IN ('clear', 'needs_clarification', 'blocked')),
    approval            TEXT NOT NULL CHECK (approval IN ('not_required', 'pending', 'approved', 'rejected')),
    rationale           TEXT NOT NULL,
    assessed_at         TEXT NOT NULL,
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);

CREATE TABLE jira_work_item_mappings (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL DEFAULT '',
    orchestration_id    TEXT NOT NULL,
    parent_task_id      TEXT NOT NULL,
    requirement_source_id TEXT NOT NULL,
    jira_issue_key      TEXT NOT NULL,
    created_at          TEXT NOT NULL,
    UNIQUE (orchestration_id, parent_task_id),
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);

CREATE TABLE jira_write_intents (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL DEFAULT '',
    jira_issue_key      TEXT NOT NULL,
    action              TEXT NOT NULL,
    payload             TEXT NOT NULL,
    idempotency_key     TEXT NOT NULL UNIQUE,
    status              TEXT NOT NULL CHECK (status IN ('pending', 'retrying', 'succeeded', 'failed')),
    attempt_count       INTEGER NOT NULL DEFAULT 0,
    retry_at            TEXT,
    last_error          TEXT NOT NULL DEFAULT '',
    external_id         TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL,
    FOREIGN KEY (case_id) REFERENCES delivery_cases(id)
);
CREATE INDEX jira_write_intents_retry_idx ON jira_write_intents(status, retry_at, created_at);

CREATE TABLE delivery_progress_reports (
    id                  TEXT PRIMARY KEY,
    case_id             TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    session_id          TEXT NOT NULL,
    progress_percent    REAL,
    summary             TEXT NOT NULL,
    reported_at         TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES delivery_sessions(id)
);
CREATE INDEX delivery_progress_reports_execution_reported_idx ON delivery_progress_reports(execution_id, reported_at, id);

ALTER TABLE delivery_worklogs ADD COLUMN case_id TEXT NOT NULL DEFAULT '';
ALTER TABLE delivery_worklogs ADD COLUMN execution_id TEXT NOT NULL DEFAULT '';