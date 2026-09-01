-- internal/knowledge and its MCP/CLI surface were removed: the durable
-- knowledge store (0008_knowledge.sql) had lost every caller once its
-- MCP tools and CLI commands were deleted, leaving it reachable only from
-- the panel's read-only dashboard. Existing records were migrated out to
-- a separate memory system (mom) before this migration was written.
-- Dropping both tables also drops their indexes automatically.
DROP TABLE knowledge_relations;
DROP TABLE knowledge_records;
