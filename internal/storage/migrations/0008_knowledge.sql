-- Durable, provenance-backed knowledge store (internal/knowledge), moved off
-- the per-project Dolt database (and the ADR-0020 Dolt hub) onto the shared
-- SQLite kernel (punokawan-14yn.15). Because the kernel is one database shared
-- by every local project checkout, every row is scoped by project_id so
-- identical record ids minted by two different projects can never collide or
-- leak into each other's queries.
--
-- updated_at is stored as fixed-width RFC3339-with-9-fractional-digit UTC text
-- (see internal/knowledge's timeLayout) so it sorts and range-compares
-- correctly under SQLite's default byte-wise text collation, which the keyset
-- (updated_at DESC, id ASC) pagination in ListRecords depends on.
CREATE TABLE knowledge_records (
    project_id     TEXT NOT NULL,
    id             TEXT NOT NULL,
    type           TEXT NOT NULL,
    status         TEXT NOT NULL,
    validity_state TEXT NOT NULL,
    data           TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (project_id, id)
);

-- Backs the panel's first-page browse and keyset pagination: every ListRecords
-- query orders by (updated_at DESC, id) within a project.
CREATE INDEX idx_knowledge_records_project_updated ON knowledge_records (project_id, updated_at DESC, id);
CREATE INDEX idx_knowledge_records_project_type ON knowledge_records (project_id, type);
CREATE INDEX idx_knowledge_records_project_status ON knowledge_records (project_id, status);
CREATE INDEX idx_knowledge_records_project_validity ON knowledge_records (project_id, validity_state);

-- Normalized index of each record's relations, so relations that cross
-- knowledge-record-id boundaries can be traversed with a query (Related's
-- reverse lookup) instead of scanning every record's JSON blob.
CREATE TABLE knowledge_relations (
    project_id TEXT NOT NULL,
    from_id    TEXT NOT NULL,
    type       TEXT NOT NULL,
    to_id      TEXT NOT NULL,
    PRIMARY KEY (project_id, from_id, type, to_id)
);

CREATE INDEX idx_knowledge_relations_project_to ON knowledge_relations (project_id, to_id);
