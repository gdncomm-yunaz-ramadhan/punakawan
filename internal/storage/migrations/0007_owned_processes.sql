-- Durable process ownership (punokawan-14yn.18): a record is written
-- before its process is exposed as running, and read back by
-- Registry.Reconcile after an abrupt daemon restart to find and clean
-- up genuine survivors without ever acting on a pid whose recorded
-- start_time no longer matches - that mismatch means the pid has been
-- reused by an unrelated process, which must be left alone.
CREATE TABLE owned_processes (
    run_id          TEXT PRIMARY KEY,
    lease_id        TEXT NOT NULL DEFAULT '',
    pid             INTEGER NOT NULL,
    executable      TEXT NOT NULL,
    start_time      TEXT NOT NULL,
    ownership_token TEXT NOT NULL,
    state           TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX owned_processes_state_idx ON owned_processes (state);
