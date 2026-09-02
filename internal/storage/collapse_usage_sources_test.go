package storage

import (
	"context"
	"testing"
)

// migrationSQL returns one embedded migration's text, so a data migration
// can be exercised against rows that only exist after the schema does.
func migrationSQL(t *testing.T, name string) string {
	t.Helper()
	body, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read migration %s: %v", name, err)
	}
	return string(body)
}

// A session used to store its whole cumulative usage once per agent id the
// client had seen, plus once under "main", and the totals summed them.
func TestCollapseDuplicateUsageSourcesKeepsOneReadingPerSession(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()

	if _, err := db.write.ExecContext(ctx, `
		INSERT INTO agent_sessions (id, orchestration_id, execution_id, client_kind, external_session_id, participant, role_version, provider, model, worktree_path, status, telemetry_status, started_at)
		VALUES ('sess-1', 'orc-1', 'exec-1', 'claude-code', 'ext-1', '', '', '', '', '', 'closed', 'complete', '2026-09-02T16:00:00Z')`); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	const usage = `[{"model":"claude-sonnet-5","input_tokens":56,"output_tokens":11060}]`
	rows := []struct {
		source                           string
		input, output, cacheWrite, calls int64
		models                           string
	}{
		{"main", 56, 11060, 149906, 14, usage},
		{"a5f44a2d9ba21af38", 56, 11060, 149906, 14, usage},
		{"ac1afaa5e06fe763d", 56, 11060, 149906, 14, usage},
		// A genuinely separate subagent transcript: different numbers, so
		// it is a different reading and must survive.
		{"t0123456789abcdef", 7, 3, 0, 1, `[{"model":"claude-sonnet-5","input_tokens":7,"output_tokens":3}]`},
	}
	for _, r := range rows {
		if _, err := db.write.ExecContext(ctx, `
			INSERT INTO agent_usage_snapshots (session_id, source_id, sequence, input_tokens, output_tokens, cache_write_tokens, cache_read_tokens, tool_calls, elapsed_ms, model_usage_json, pricing_json, estimated_cost_json, observed_at)
			VALUES ('sess-1', ?, ?, ?, ?, ?, 0, ?, 0, ?, '[]', '{}', '2026-09-02T16:15:00Z')`,
			r.source, r.input+r.output, r.input, r.output, r.cacheWrite, r.calls, r.models); err != nil {
			t.Fatalf("insert snapshot %s: %v", r.source, err)
		}
	}

	if _, err := db.write.ExecContext(ctx, migrationSQL(t, "0037_collapse_duplicate_usage_sources.sql")); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	var survivors []string
	cursor, err := db.Reader().QueryContext(ctx, `SELECT source_id FROM agent_usage_snapshots WHERE session_id = 'sess-1' ORDER BY source_id`)
	if err != nil {
		t.Fatalf("read survivors: %v", err)
	}
	defer cursor.Close()
	for cursor.Next() {
		var id string
		if err := cursor.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		survivors = append(survivors, id)
	}
	if len(survivors) != 2 || survivors[0] != "main" || survivors[1] != "t0123456789abcdef" {
		t.Fatalf("survivors = %v, want the main reading and the distinct subagent one", survivors)
	}

	var total int64
	if err := db.Reader().QueryRowContext(ctx, `SELECT SUM(cache_write_tokens) FROM agent_usage_snapshots WHERE session_id = 'sess-1'`).Scan(&total); err != nil {
		t.Fatalf("sum: %v", err)
	}
	if total != 149906 {
		t.Fatalf("summed cache writes = %d, want the session's actual 149906", total)
	}
}
