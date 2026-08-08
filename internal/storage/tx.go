package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrDuplicateWrite is returned by Write when idempotencyKey was already
// committed by a prior call; fn was not invoked and no new audit row was
// written. Callers can treat it as success (the effect already happened)
// or inspect it to distinguish a fresh commit from a replay.
var ErrDuplicateWrite = errors.New("storage: duplicate idempotency key")

// Write runs fn inside a single transaction on the serialized writer
// connection and appends an audit_log row recording summary, committing
// both together or rolling back both together. If idempotencyKey was
// already committed, fn does not run and Write returns ErrDuplicateWrite
// without changing the database.
func (db *DB) Write(ctx context.Context, idempotencyKey, summary string, fn func(tx *sql.Tx) error) error {
	tx, err := db.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin write: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM audit_log WHERE idempotency_key = ?`, idempotencyKey).Scan(&exists)
	switch {
	case err == nil:
		return ErrDuplicateWrite
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("storage: check idempotency key: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_log (idempotency_key, summary) VALUES (?, ?)`,
		idempotencyKey, summary,
	); err != nil {
		return fmt.Errorf("storage: record audit revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit write: %w", err)
	}
	return nil
}
