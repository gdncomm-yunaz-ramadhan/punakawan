package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// checkCapabilities verifies the JSON1 and FTS5 extensions this kernel
// depends on are available, failing startup early and clearly rather
// than at first use of a JSON or full-text query deep in a repository.
func checkCapabilities(ctx context.Context, db *sql.DB) error {
	var jsonProbe string
	if err := db.QueryRowContext(ctx, `SELECT json('{"ok":true}')`).Scan(&jsonProbe); err != nil {
		return fmt.Errorf("storage: JSON1 capability check failed: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin FTS5 capability check: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE VIRTUAL TABLE temp.__fts5_probe USING fts5(x)`); err != nil {
		return fmt.Errorf("storage: FTS5 capability check failed: %w", err)
	}
	return nil
}
