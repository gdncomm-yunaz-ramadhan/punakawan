-- punokawan-14yn.2 adds requirement sources, parent tasks, and dependency
-- edges as more sub-entities scoped within a delivery_events row, not just
-- lanes. Generalize the lane-specific lane_id column to entity_id (same
-- role: filter this orchestration's events down to one sub-entity's own
-- history) and drop the type CHECK constraint - it would need editing on
-- every future event type added by .3/.4/etc; the Go layer's
-- protocol.DeliveryEventType enum already validates this at decode time,
-- so the DB-level list was duplicate enforcement, not the only one.
CREATE TABLE delivery_events_new (
    id               TEXT PRIMARY KEY,
    orchestration_id TEXT NOT NULL,
    entity_id        TEXT,
    idempotency_key  TEXT NOT NULL,
    type             TEXT NOT NULL,
    payload          TEXT NOT NULL,
    sequence         INTEGER NOT NULL,
    occurred_at      TEXT NOT NULL,
    UNIQUE (orchestration_id, sequence)
);

INSERT INTO delivery_events_new (id, orchestration_id, entity_id, idempotency_key, type, payload, sequence, occurred_at)
SELECT id, orchestration_id, lane_id, idempotency_key, type, payload, sequence, occurred_at FROM delivery_events;

DROP TABLE delivery_events;
ALTER TABLE delivery_events_new RENAME TO delivery_events;

CREATE INDEX delivery_events_orchestration_idx ON delivery_events (orchestration_id, sequence);
CREATE INDEX delivery_events_entity_idx ON delivery_events (entity_id) WHERE entity_id IS NOT NULL;
