package doltimport

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ygrip/punakawan/internal/tools"
)

// Querier runs one read-only SQL statement against the Dolt source and returns
// its result rows, each a column-name -> raw-JSON-value map. An empty or
// DDL-shaped result (`{}`) returns a nil slice, not an error. Extracted as an
// interface-like func type so the importer can be driven by an in-memory fake
// in tests that do not have a dolt binary.
type Querier func(ctx context.Context, sql string) ([]map[string]json.RawMessage, error)

// newDoltQuerier builds a Querier that shells out to the dolt binary once per
// query with `dolt sql -q "..." -r json`, supervised through tools.Supervisor
// for a bounded lifecycle and output cap. It never starts a sql-server.
func newDoltQuerier(src Source) Querier {
	// The supervisor allows only the source directory as a working root; the
	// import reads exactly one Dolt store and needs nothing else.
	sup := tools.New(src.Dir)
	return func(ctx context.Context, sql string) ([]map[string]json.RawMessage, error) {
		args := make([]string, 0, 7)
		if src.DoltCfgDir != "" {
			// A hub lays out many databases sharing one .doltcfg at its root;
			// without pointing dolt at it explicitly, dolt errors with
			// "multiple .doltcfg directories detected".
			args = append(args, "--doltcfg-dir", src.DoltCfgDir)
		}
		args = append(args, "sql", "-q", sql, "-r", "json")

		res, err := sup.Run(ctx, tools.Spec{Name: "dolt", Args: args, Dir: src.Dir})
		if err != nil {
			return nil, fmt.Errorf("doltimport: run dolt: %w", err)
		}
		if res.ExitCode != 0 {
			return nil, fmt.Errorf("doltimport: dolt sql exited %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
		}
		return parseRows(res.Stdout)
	}
}

// parseRows decodes dolt's `-r json` output. A non-empty result is
// {"rows": [ {col: value, ...}, ... ]}; an empty or DDL result is {} (or
// blank), which decodes to no rows.
func parseRows(stdout []byte) ([]map[string]json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(stdout))
	if trimmed == "" || trimmed == "{}" {
		return nil, nil
	}
	var out struct {
		Rows []map[string]json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, fmt.Errorf("doltimport: parse dolt json output: %w", err)
	}
	return out.Rows, nil
}

// jsonString unmarshals a raw JSON string column value into a Go string,
// tolerating a SQL NULL (JSON null) as the empty string.
func jsonString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

// jsonInt unmarshals a raw JSON numeric column value into a Go int, tolerating
// either shape dolt's `-r json` output has been observed to use for the same
// query: a bare JSON number (newer dolt) or a JSON string containing the
// number (dolt 2.2.1, at least for COUNT(*)). dolt's own serialization of
// aggregate scalars is not consistent across versions, so any caller reading
// a numeric column back from dolt should go through this rather than
// unmarshaling into int directly.
func jsonInt(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("not a number or numeric string: %s", string(raw))
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("not a number or numeric string: %s", string(raw))
	}
	return n, nil
}

// jsonAny unmarshals a raw JSON struct/object column value into out,
// tolerating either shape dolt's `-r json` output has been observed to use
// for the same JSON-typed column: a bare JSON value (newer dolt) or a JSON
// string containing that value's JSON text, double-encoded (dolt 2.2.1, at
// least for a legacy per-project store's `data` column). This is the same
// version-inconsistency as jsonInt above, just on a struct column instead of
// a numeric scalar, so any caller reading a JSON-typed column back from dolt
// should go through this rather than unmarshaling into the target directly.
func jsonAny(raw json.RawMessage, out any) error {
	if err := json.Unmarshal(raw, out); err == nil {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("neither a bare JSON value nor a JSON string containing one: %s", string(raw))
	}
	if err := json.Unmarshal([]byte(s), out); err != nil {
		return fmt.Errorf("string-wrapped JSON did not unmarshal: %w", err)
	}
	return nil
}

// countRows runs SELECT COUNT(*) against table and returns the scalar count.
func countRows(ctx context.Context, q Querier, table string) (int, error) {
	rows, err := q(ctx, "SELECT COUNT(*) AS n FROM "+table)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	n, err := jsonInt(rows[0]["n"])
	if err != nil {
		return 0, fmt.Errorf("doltimport: parse count for %s: %w", table, err)
	}
	return n, nil
}
