-- Per-project delivery configuration (punokawan-14yn.4). Plain registry
-- data like delivery_projects, not event-sourced: a profile is merged
-- read-only repository configuration plus detected/learned defaults,
-- superseded wholesale on update rather than replayed from history.
CREATE TABLE delivery_profiles (
    id                     TEXT PRIMARY KEY,
    project_id             TEXT NOT NULL UNIQUE,
    local_path             TEXT NOT NULL DEFAULT '',
    canonical_remote       TEXT NOT NULL DEFAULT '',
    base_branch            TEXT NOT NULL,
    provider               TEXT NOT NULL DEFAULT '',
    build_command          TEXT NOT NULL DEFAULT '',
    test_command           TEXT NOT NULL DEFAULT '',
    required_executables   TEXT NOT NULL DEFAULT '[]',
    required_services      TEXT NOT NULL DEFAULT '[]',
    quality_rules          TEXT NOT NULL DEFAULT '[]',
    ci_adapter             TEXT NOT NULL DEFAULT '',
    verification_gates     TEXT NOT NULL DEFAULT '[]',
    max_concurrent_workers INTEGER,
    revision               INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (project_id) REFERENCES delivery_projects (id)
);
