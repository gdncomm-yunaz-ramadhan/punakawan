package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// Backup writes a consistent snapshot of the database to destPath using
// VACUUM INTO, then opens the snapshot and runs an integrity check
// before reporting success. destPath must not already exist.
func (db *DB) Backup(ctx context.Context, destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("storage: backup destination already exists: %s", destPath)
	}
	if _, err := db.write.ExecContext(ctx, `VACUUM INTO ?`, destPath); err != nil {
		return fmt.Errorf("storage: vacuum into %s: %w", destPath, err)
	}
	if err := IntegrityCheck(ctx, destPath); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("storage: backup at %s failed integrity check: %w", destPath, err)
	}
	return nil
}

// IntegrityCheck opens the SQLite database at path read-only and runs
// PRAGMA integrity_check, returning an error unless it reports "ok".
func IntegrityCheck(ctx context.Context, path string) error {
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("storage: open %s for integrity check: %w", path, err)
	}
	defer conn.Close()

	var result string
	if err := conn.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("storage: run integrity_check on %s: %w", path, err)
	}
	if result != "ok" {
		return fmt.Errorf("storage: integrity_check on %s reported %q", path, result)
	}
	return nil
}
