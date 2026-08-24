-- Persists internal/planexec.Execution: one plan step's execution
-- lifecycle (ready/claimed/committed/reopened). This tracks the same
-- kind of thing a Beads issue tracks - has this unit of work been picked
-- up, is it done, was it reopened - but scoped to one plan's step
-- (referenced by the step's stable id from the plans table) instead of a
-- separate Beads issue. It is additive alongside Beads/taskstore, not a
-- replacement: a project may use either, both, or neither.
--
-- status/claimed_by/claimed_at/completed_at/reopen_reason are plain
-- current-state columns, not an event log: a step's whole history is one
-- row, overwritten on each transition, mirroring how the tasks table in
-- 0003_taskstore.sql tracks task state.
CREATE TABLE plan_step_executions (
    id            TEXT NOT NULL PRIMARY KEY,
    plan_id       TEXT NOT NULL,
    plan_revision INTEGER NOT NULL,
    step_id       TEXT NOT NULL,
    status        TEXT NOT NULL,
    claimed_by    TEXT NOT NULL DEFAULT '',
    claimed_at    TEXT,
    completed_at  TEXT,
    reopen_reason TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    -- One execution per (plan, step): re-invoking a plan whose steps
    -- already have executions must find the existing row rather than
    -- create a duplicate.
    UNIQUE (plan_id, step_id)
);

CREATE INDEX idx_plan_step_executions_plan_id ON plan_step_executions (plan_id);
