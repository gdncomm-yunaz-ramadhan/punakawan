-- audit_log records one row per successful write transaction. idempotency_key
-- is unique so a duplicate write is detected and skipped rather than replayed.
CREATE TABLE audit_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key TEXT NOT NULL UNIQUE,
    occurred_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    summary         TEXT NOT NULL
);
