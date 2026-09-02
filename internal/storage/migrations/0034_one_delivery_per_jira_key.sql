-- A Jira issue is one piece of work, but delivery_cases_active_source_idx
-- keyed uniqueness on the tenant as well, so the same issue key opened a
-- second parallel lifetime whenever two callers named the tenant
-- differently - which they did: the removed references-based start path
-- always resolved with an empty tenant while the source path passed a
-- real one. This merges any lifetimes that split that way and re-keys the
-- index so it cannot happen again.
--
-- The tenant column stays, and empty tenants are left empty rather than
-- backfilled: it records which adapter instance a delivery was reached
-- through, and inventing one for a row that never had it would be worse
-- than an honest blank now that nothing resolves by it.

CREATE TEMP TABLE jira_case_merge AS
SELECT c.id AS loser_id,
       (SELECT w.id
          FROM delivery_cases w
         WHERE w.source_kind = 'jira'
           AND w.status = 'active'
           AND w.source_key = c.source_key
         ORDER BY w.created_at DESC, w.id DESC
         LIMIT 1) AS winner_id
  FROM delivery_cases c
 WHERE c.source_kind = 'jira' AND c.status = 'active';

DELETE FROM jira_case_merge WHERE winner_id IS NULL OR winner_id = loser_id;

-- delivery_executions is unique on (case_id, ordinal), so a loser's
-- ordinals are parked out of range before the move and every affected
-- case is renumbered densely by start time afterwards.
UPDATE delivery_executions
   SET ordinal = 1000000 + rowid
 WHERE case_id IN (SELECT loser_id FROM jira_case_merge);

UPDATE delivery_budgets            SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = delivery_budgets.case_id)            WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE delivery_progress_reports   SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = delivery_progress_reports.case_id)   WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE delivery_session_checkpoints SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = delivery_session_checkpoints.case_id) WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE delivery_sessions           SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = delivery_sessions.case_id)           WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE delivery_usage_ledger       SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = delivery_usage_ledger.case_id)       WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE delivery_worklogs           SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = delivery_worklogs.case_id)           WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE jira_assessments            SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = jira_assessments.case_id)            WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE jira_source_snapshots       SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = jira_source_snapshots.case_id)       WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE jira_work_item_mappings     SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = jira_work_item_mappings.case_id)     WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE jira_write_intents          SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = jira_write_intents.case_id)          WHERE case_id IN (SELECT loser_id FROM jira_case_merge);
UPDATE delivery_executions         SET case_id = (SELECT winner_id FROM jira_case_merge WHERE loser_id = delivery_executions.case_id)         WHERE case_id IN (SELECT loser_id FROM jira_case_merge);

UPDATE delivery_executions
   SET ordinal = (SELECT COUNT(*)
                    FROM delivery_executions peer
                   WHERE peer.case_id = delivery_executions.case_id
                     AND (peer.started_at < delivery_executions.started_at
                          OR (peer.started_at = delivery_executions.started_at AND peer.id <= delivery_executions.id)))
 WHERE case_id IN (SELECT winner_id FROM jira_case_merge);

DELETE FROM delivery_cases WHERE id IN (SELECT loser_id FROM jira_case_merge);

DROP TABLE jira_case_merge;

DROP INDEX IF EXISTS delivery_cases_active_source_idx;
CREATE UNIQUE INDEX delivery_cases_active_source_idx
ON delivery_cases(source_provider, source_key)
WHERE source_kind = 'jira' AND status = 'active';
