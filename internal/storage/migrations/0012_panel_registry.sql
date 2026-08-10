-- Panel workspace registry (internal/panel/registry), moved off the
-- OS-config YAML file onto the shared SQLite kernel (punokawan-14yn.16).
-- Unlike the append-only subsystems, this is a small mutable-in-place list
-- ("which local workspace checkouts does this machine's panel know about"):
-- Register upserts by id, Remove deletes, SetPinned toggles a flag. It is
-- genuinely machine-global, not project-scoped, so there is no project_id
-- column - there is exactly one registry per machine, mirroring
-- internal/procreg's owned_processes.
--
-- seq gives a stable insertion order for List; id is the stable workspace
-- identity and is unique. registered_at is required; display_name,
-- last_seen_at, and pinned are optional and stored NULL when unset, matching
-- the pointer fields on protocol.PanelWorkspaceRegistryEntry.
CREATE TABLE panel_workspaces (
    seq           INTEGER PRIMARY KEY AUTOINCREMENT,
    id            TEXT NOT NULL UNIQUE,
    path          TEXT NOT NULL,
    display_name  TEXT,
    registered_at TEXT NOT NULL,
    last_seen_at  TEXT,
    pinned        INTEGER
);
