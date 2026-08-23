-- Canonical multi-project delivery control plane (punokawan-14yn.1).
--
-- delivery_projects is plain registry data (CRUD, not event-sourced).
-- delivery_orchestrations and delivery lanes are NOT materialized tables:
-- their state is derived by replaying delivery_events, per design ("State
-- derives from append-only idempotent events"). Only the event log is
-- durable; orchestration/lane structs are a read-time projection over it.
--
-- Approval, question, worker-lease, artifact, and learning-provenance
-- tables are deliberately not defined here: punokawan-14yn.4, .3, .7, and
-- .9 own those schemas and would otherwise have to redefine a guessed
-- shape from this task.

CREATE TABLE delivery_projects (
    id             TEXT PRIMARY KEY,
    slug           TEXT NOT NULL UNIQUE,
    repository_url TEXT NOT NULL,
    default_branch TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL CHECK (status IN ('active', 'disabled')),
    registered_at  TEXT NOT NULL,
    revision       INTEGER NOT NULL DEFAULT 0
);

-- orchestration_id is not a foreign key to a materialized orchestrations
-- table (none exists); the first event for an id (sequence = 0, type =
-- 'orchestration.created') is what establishes that the id exists.
CREATE TABLE delivery_events (
    id              TEXT PRIMARY KEY,
    orchestration_id TEXT NOT NULL,
    lane_id         TEXT,
    idempotency_key TEXT NOT NULL,
    type            TEXT NOT NULL CHECK (type IN (
        'orchestration.created', 'orchestration.cancelled', 'orchestration.completed',
        'input.registered', 'input.resolved',
        'lane.created', 'lane.status_changed'
    )),
    payload         TEXT NOT NULL,
    sequence        INTEGER NOT NULL,
    occurred_at     TEXT NOT NULL,
    UNIQUE (orchestration_id, sequence)
);

CREATE INDEX delivery_events_orchestration_idx ON delivery_events (orchestration_id, sequence);
CREATE INDEX delivery_events_lane_idx ON delivery_events (lane_id) WHERE lane_id IS NOT NULL;
