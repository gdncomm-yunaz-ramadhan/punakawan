-- Adds RoleVersion to agent_sessions: the internal/agent role manifest
-- version (RoleSpec.Version) a session's Participant resolved to at
-- start_delivery_session time, so a session can be tied back to the exact
-- manifest content that produced it. Empty string means the participant
-- did not name one of the four known roles (best-effort enrichment, not a
-- required input).
ALTER TABLE agent_sessions ADD COLUMN role_version TEXT NOT NULL DEFAULT '';
