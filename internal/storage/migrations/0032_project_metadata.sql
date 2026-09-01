-- Adds a place for upsert_project to hold static repository configuration
-- facts (package manager, layout, naming convention, test framework,
-- linters, formatters) - previously these had nowhere to live except the
-- now-removed knowledge store. NULL means "nothing captured yet"; the
-- application layer merges into this column field-by-field rather than
-- replacing it wholesale.
ALTER TABLE delivery_projects ADD COLUMN metadata TEXT;
