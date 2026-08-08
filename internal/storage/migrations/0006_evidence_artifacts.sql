-- Content-addressed evidence store (punokawan-14yn.7). Both tables are
-- insert-only: a blob's bytes and an invocation record are immutable
-- once written, so there is no event log or update path here, unlike
-- delivery_events' mutable entities.
CREATE TABLE artifact_blobs (
    content_hash TEXT PRIMARY KEY,
    media_type   TEXT NOT NULL,
    byte_size    INTEGER NOT NULL,
    stored_at    TEXT NOT NULL
);

CREATE TABLE evidence_artifacts (
    id               TEXT PRIMARY KEY,
    orchestration_id TEXT NOT NULL,
    project_id       TEXT NOT NULL,
    lane_id          TEXT,
    parent_task_id   TEXT,
    kind             TEXT NOT NULL,
    content_hash     TEXT NOT NULL,
    producer         TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    retain_until     TEXT,
    FOREIGN KEY (content_hash) REFERENCES artifact_blobs (content_hash),
    FOREIGN KEY (project_id) REFERENCES delivery_projects (id)
);

CREATE INDEX evidence_artifacts_orchestration_idx ON evidence_artifacts (orchestration_id);
CREATE INDEX evidence_artifacts_project_idx ON evidence_artifacts (project_id);
CREATE INDEX evidence_artifacts_lane_idx ON evidence_artifacts (lane_id) WHERE lane_id IS NOT NULL;
CREATE INDEX evidence_artifacts_parent_task_idx ON evidence_artifacts (parent_task_id) WHERE parent_task_id IS NOT NULL;
