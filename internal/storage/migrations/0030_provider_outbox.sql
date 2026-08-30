CREATE TABLE provider_write_intents (
    id                    TEXT PRIMARY KEY,
    orchestration_id      TEXT NOT NULL,
    execution_id          TEXT NOT NULL DEFAULT '',
    session_id            TEXT NOT NULL DEFAULT '',
    adapter_id            TEXT NOT NULL,
    operation             TEXT NOT NULL,
    target_key            TEXT NOT NULL,
    payload_json          TEXT NOT NULL,
    operation_fingerprint TEXT NOT NULL UNIQUE,
    status                TEXT NOT NULL CHECK (status IN ('pending','claimed','retrying','succeeded','failed','cancelled','reconciling')),
    claim_owner           TEXT NOT NULL DEFAULT '',
    claim_until           TEXT,
    attempt_count         INTEGER NOT NULL DEFAULT 0,
    next_attempt_at       TEXT,
    external_id           TEXT NOT NULL DEFAULT '',
    provider_request_id   TEXT NOT NULL DEFAULT '',
    last_error_code       TEXT NOT NULL DEFAULT '',
    last_error_redacted   TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);

CREATE TABLE provider_write_attempts (
    intent_id           TEXT NOT NULL,
    attempt             INTEGER NOT NULL,
    worker_id           TEXT NOT NULL,
    started_at          TEXT NOT NULL,
    finished_at         TEXT,
    outcome             TEXT NOT NULL CHECK (outcome IN ('running','succeeded','retryable','permanent','ambiguous')),
    provider_request_id TEXT NOT NULL DEFAULT '',
    diagnostic_redacted TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (intent_id, attempt),
    FOREIGN KEY (intent_id) REFERENCES provider_write_intents(id)
);

CREATE TABLE provider_effects (
    intent_id   TEXT NOT NULL,
    effect_key  TEXT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL,
    PRIMARY KEY (intent_id, effect_key),
    FOREIGN KEY (intent_id) REFERENCES provider_write_intents(id)
);

CREATE INDEX provider_write_claim_idx
ON provider_write_intents(status, next_attempt_at, claim_until, created_at);

CREATE TABLE IF NOT EXISTS migration_warnings (
    migration_version INTEGER NOT NULL,
    entity_kind       TEXT NOT NULL,
    entity_key        TEXT NOT NULL,
    warning           TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (migration_version, entity_kind, entity_key)
);

-- Migrate every still-actionable Jira write intent (internal/delivery's own
-- durable jira_write_intents table, migrations 0018/0021/0024) into the
-- provider-neutral outbox. Each migrated row keeps a fingerprint derived from
-- its own immutable id, so re-running this exact migration can never enqueue
-- the same legacy write twice. Rows already succeeded, failed, or cancelled
-- are left in place as history; jira_write_intents itself is not dropped,
-- since it remains the durable record of every write ever queued through the
-- pre-outbox path.
INSERT INTO provider_write_intents (
    id, orchestration_id, execution_id, session_id, adapter_id, operation, target_key,
    payload_json, operation_fingerprint, status, claim_owner, claim_until, attempt_count,
    next_attempt_at, external_id, provider_request_id, last_error_code, last_error_redacted,
    created_at, updated_at
)
SELECT
    'migrated-jira-write-intent-' || jwi.id,
    COALESCE(de.orchestration_id, ''),
    jwi.execution_id,
    jwi.session_id,
    'atlassian',
    CASE jwi.action
        WHEN 'add_comment' THEN 'atlassian.addJiraComment'
        WHEN 'comment' THEN 'atlassian.addJiraComment'
        WHEN 'clarification_comment' THEN 'atlassian.addJiraComment'
        WHEN 'update_description' THEN 'atlassian.editJiraIssue'
        WHEN 'description' THEN 'atlassian.editJiraIssue'
        WHEN 'transition_status' THEN 'atlassian.transitionJiraIssue'
        WHEN 'transition' THEN 'atlassian.transitionJiraIssue'
        WHEN 'create_subtask' THEN 'atlassian.createJiraSubtask'
        WHEN 'update_estimate' THEN 'atlassian.editJiraIssue'
        WHEN 'update_story_points' THEN 'atlassian.editJiraIssue'
        WHEN 'worklog' THEN 'atlassian.addWorklog'
        ELSE 'atlassian.' || jwi.action
    END,
    jwi.jira_issue_key,
    jwi.payload,
    'legacy:jira_write_intent:' || jwi.id,
    CASE WHEN jwi.status = 'retrying' THEN 'retrying' ELSE 'pending' END,
    '',
    NULL,
    jwi.attempt_count,
    jwi.retry_at,
    COALESCE(jwi.external_id, ''),
    '',
    '',
    COALESCE(jwi.last_error, ''),
    jwi.created_at,
    jwi.updated_at
FROM jira_write_intents AS jwi
LEFT JOIN delivery_executions AS de ON de.id = jwi.execution_id
WHERE jwi.status IN ('pending', 'retrying');

-- Fold the generic sync queue's append-only history (internal/syncqueue,
-- migration 0011) down to each entry's latest record.
CREATE TEMP TABLE sync_queue_latest AS
SELECT sqe.project_id, sqe.id, sqe.data
FROM sync_queue_entries AS sqe
JOIN (
    SELECT project_id, id, MAX(seq) AS max_seq
    FROM sync_queue_entries
    GROUP BY project_id, id
) AS latest ON latest.project_id = sqe.project_id AND latest.id = sqe.id AND latest.max_seq = sqe.seq;

-- A generic sync-queue entry carries only an adapter id and a bare operation
-- name (never a manifest to check against), so "side-effecting" here is
-- decided against the fixed set of write operations this codebase's
-- Atlassian and GitHub adapter manifests currently declare
-- (packages/adapter-atlassian/src/manifest.ts,
-- packages/github-adapter/src/manifest.ts). A pending entry naming any other
-- operation, or missing its recorded params, is not guessable and is
-- recorded as a migration warning instead.
INSERT INTO provider_write_intents (
    id, orchestration_id, execution_id, session_id, adapter_id, operation, target_key,
    payload_json, operation_fingerprint, status, claim_owner, claim_until, attempt_count,
    next_attempt_at, external_id, provider_request_id, last_error_code, last_error_redacted,
    created_at, updated_at
)
SELECT
    'migrated-sync-queue-' || sq.project_id || '-' || sq.id,
    COALESCE(json_extract(sq.data, '$.run_id'), ''),
    '',
    '',
    json_extract(sq.data, '$.adapter'),
    json_extract(sq.data, '$.adapter') || '.' || json_extract(sq.data, '$.op'),
    COALESCE(json_extract(sq.data, '$.issue_id_or_key'), ''),
    json_extract(sq.data, '$.params'),
    'legacy:sync_queue:' || sq.project_id || ':' || sq.id,
    'pending',
    '',
    NULL,
    COALESCE(json_extract(sq.data, '$.attempts'), 0),
    NULL,
    '',
    '',
    '',
    COALESCE(json_extract(sq.data, '$.error'), ''),
    json_extract(sq.data, '$.created_at'),
    json_extract(sq.data, '$.created_at')
FROM sync_queue_latest AS sq
WHERE json_extract(sq.data, '$.status') = 'pending'
  AND json_extract(sq.data, '$.params') IS NOT NULL
  AND (json_extract(sq.data, '$.adapter') || '.' || json_extract(sq.data, '$.op')) IN (
    'atlassian.addJiraComment', 'atlassian.transitionJiraIssue', 'atlassian.editJiraIssueFields',
    'atlassian.editJiraIssue', 'atlassian.addWorklog', 'atlassian.createJiraIssue',
    'atlassian.createJiraSubtask', 'atlassian.createIssueLink', 'atlassian.downloadJiraAttachment',
    'atlassian.uploadJiraAttachment', 'atlassian.deleteJiraAttachment',
    'github.createPullRequest', 'github.addLabels', 'github.requestReviewers',
    'github.replyToReviewComment', 'github.createPullRequestReview', 'github.resolveReviewThread'
  );

INSERT INTO migration_warnings(migration_version, entity_kind, entity_key, warning)
SELECT 30, 'sync_queue_entry', sq.project_id || ':' || sq.id,
    'sync queue entry omitted from the provider outbox: status=' || COALESCE(json_extract(sq.data, '$.status'), 'unknown') ||
    ' operation=' || COALESCE(json_extract(sq.data, '$.adapter'), '?') || '.' || COALESCE(json_extract(sq.data, '$.op'), '?') ||
    ' params_present=' || (json_extract(sq.data, '$.params') IS NOT NULL)
FROM sync_queue_latest AS sq
WHERE NOT (
    json_extract(sq.data, '$.status') = 'pending'
    AND json_extract(sq.data, '$.params') IS NOT NULL
    AND (json_extract(sq.data, '$.adapter') || '.' || json_extract(sq.data, '$.op')) IN (
      'atlassian.addJiraComment', 'atlassian.transitionJiraIssue', 'atlassian.editJiraIssueFields',
      'atlassian.editJiraIssue', 'atlassian.addWorklog', 'atlassian.createJiraIssue',
      'atlassian.createJiraSubtask', 'atlassian.createIssueLink', 'atlassian.downloadJiraAttachment',
      'atlassian.uploadJiraAttachment', 'atlassian.deleteJiraAttachment',
      'github.createPullRequest', 'github.addLabels', 'github.requestReviewers',
      'github.replyToReviewComment', 'github.createPullRequestReview', 'github.resolveReviewThread'
    )
);

DROP TABLE sync_queue_latest;
DROP INDEX IF EXISTS sync_queue_entries_project_seq_idx;
DROP INDEX IF EXISTS sync_queue_entries_project_id_idx;
DROP TABLE sync_queue_entries;
