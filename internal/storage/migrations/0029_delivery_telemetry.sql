CREATE TABLE agent_sessions (
    id                  TEXT PRIMARY KEY,
    orchestration_id    TEXT NOT NULL,
    execution_id        TEXT NOT NULL,
    client_kind         TEXT NOT NULL,
    external_session_id TEXT NOT NULL,
    participant         TEXT NOT NULL DEFAULT '',
    provider            TEXT NOT NULL DEFAULT '',
    model               TEXT NOT NULL DEFAULT '',
    worktree_path       TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL CHECK (status IN ('active', 'closed', 'abandoned')),
    telemetry_status    TEXT NOT NULL CHECK (telemetry_status IN ('complete', 'incomplete')),
    started_at          TEXT NOT NULL,
    stopped_at          TEXT,
    stop_reason         TEXT NOT NULL DEFAULT '',
    UNIQUE (client_kind, external_session_id)
);

CREATE TABLE agent_usage_snapshots (
    session_id          TEXT NOT NULL,
    source_id           TEXT NOT NULL,
    sequence            INTEGER NOT NULL CHECK (sequence >= 0),
    input_tokens        INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens       INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_write_tokens  INTEGER NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    cache_read_tokens   INTEGER NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    tool_calls          INTEGER NOT NULL DEFAULT 0 CHECK (tool_calls >= 0),
    elapsed_ms          INTEGER NOT NULL DEFAULT 0 CHECK (elapsed_ms >= 0),
    model_usage_json    TEXT NOT NULL DEFAULT '[]',
    pricing_json        TEXT NOT NULL DEFAULT '[]',
    estimated_cost_json TEXT NOT NULL DEFAULT '{}',
    observed_at         TEXT NOT NULL,
    PRIMARY KEY (session_id, source_id),
    FOREIGN KEY (session_id) REFERENCES agent_sessions(id)
);

CREATE TABLE agent_session_stops (
    stop_id     TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    stopped_at  TEXT NOT NULL,
    stop_reason TEXT NOT NULL,
    FOREIGN KEY (session_id) REFERENCES agent_sessions(id)
);

CREATE INDEX agent_sessions_delivery_started_idx
ON agent_sessions(orchestration_id, started_at, id);

-- Every pre-existing delivery_sessions row becomes one legacy agent_sessions
-- row: same id (so agent_usage_snapshots' foreign key below can attach to
-- it), external_session_id defaulted to that same id since no client-native
-- session identity was ever recorded before this migration, client_kind
-- 'legacy', and telemetry_status 'incomplete' since begin/finalize
-- lifecycle events were never actually observed for it.
INSERT INTO agent_sessions (
    id, orchestration_id, execution_id, client_kind, external_session_id,
    participant, provider, model, worktree_path, status, telemetry_status,
    started_at, stopped_at, stop_reason
)
SELECT
    id, orchestration_id, execution_id, 'legacy', id,
    participant, provider, '', worktree_path,
    CASE WHEN status = 'active' THEN 'active' ELSE 'closed' END,
    'incomplete',
    started_at, ended_at, ''
FROM delivery_sessions;

-- delivery_usage_ledger never captured per-model token/tool-call/duration
-- counts (it recorded arbitrary category/quantity/unit entries), so there
-- is nothing to preserve in agent_usage_snapshots' token-shaped columns;
-- those stay at their unknown default of 0 rather than inventing a mapping
-- that would misrepresent legacy data. What the ledger does carry
-- unambiguously is priced cost, so one aggregated 'legacy' source snapshot
-- per session preserves that verbatim (never fabricated as zero): a real
-- total when at least one entry named a cost, explicitly unknown otherwise.
INSERT INTO agent_usage_snapshots (
    session_id, source_id, sequence, input_tokens, output_tokens,
    cache_write_tokens, cache_read_tokens, tool_calls, elapsed_ms,
    model_usage_json, pricing_json, estimated_cost_json, observed_at
)
SELECT
    session_id,
    'legacy',
    0,
    0, 0, 0, 0, 0, 0,
    '[]',
    '[]',
    CASE WHEN SUM(CASE WHEN cost_amount IS NOT NULL THEN 1 ELSE 0 END) > 0
         THEN json_object('amount', SUM(COALESCE(cost_amount, 0)), 'currency', MAX(cost_currency), 'known', json('true'))
         ELSE json_object('known', json('false'))
    END,
    MAX(recorded_at)
FROM delivery_usage_ledger
GROUP BY session_id;
