-- Keeps a canonical repository identity for new and updated registrations so
-- remote-first project lookup is an indexed point query. Existing rows retain
-- their raw URL and remain discoverable through indexed URL variants until
-- their next upsert refreshes this value.
ALTER TABLE delivery_projects ADD COLUMN repository_identity TEXT NOT NULL DEFAULT '';
CREATE INDEX delivery_projects_repository_identity_idx ON delivery_projects(repository_identity);
CREATE INDEX delivery_projects_repository_url_idx ON delivery_projects(repository_url);
