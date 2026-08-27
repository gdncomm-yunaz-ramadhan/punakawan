CREATE TABLE jira_field_mappings (
    id TEXT PRIMARY KEY,
    cloud_id TEXT NOT NULL,
    project_key TEXT NOT NULL,
    issue_type_id TEXT NOT NULL,
    purpose TEXT NOT NULL,
    field_id TEXT NOT NULL,
    field_name TEXT NOT NULL,
    discovered_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(cloud_id, project_key, issue_type_id, purpose)
);

CREATE INDEX jira_field_mappings_lookup_idx
    ON jira_field_mappings(cloud_id, project_key, issue_type_id, purpose);
