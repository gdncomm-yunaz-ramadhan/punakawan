// Package storage is the embedded SQLite kernel shared by every Punakawan
// project through one daemon (ADR-0021). It supersedes the Dolt hub
// (internal/knowledge, internal/hub, ADR-0020): one local database file,
// one serialized writer, up to four concurrent readers, WAL, and
// idempotent, audited writes.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

const maxReaders = 4

// dsnParams are fixed, not configurable: the kernel's failure-mode
// guarantees (durability, one writer, foreign-key integrity) depend on
// every caller using the same settings.
const dsnParams = "_foreign_keys=1&_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000"

// DB is one SQLite-backed database, opened as two pools over the same
// file: a single-connection writer (serializes all writes) and a
// bounded reader pool (WAL allows readers to proceed while a write is
// in flight).
type DB struct {
	path  string
	write *sql.DB
	read  *sql.DB
}

// Open opens (creating if absent) the SQLite database at path, applies
// pending schema migrations, and verifies JSON1/FTS5 support. path must
// be a local filesystem location; use CheckLocation to reject
// network-mounted paths before calling Open.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := "file:" + url.PathEscape(path) + "?" + dsnParams
	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open writer: %w", err)
	}
	write.SetMaxOpenConns(1)

	read, err := sql.Open("sqlite", dsn)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("storage: open reader pool: %w", err)
	}
	read.SetMaxOpenConns(maxReaders)

	db := &DB{path: path, write: write, read: read}
	if err := db.write.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("storage: connect: %w", err)
	}
	if err := migrate(ctx, db.write); err != nil {
		db.Close()
		return nil, err
	}
	if err := checkCapabilities(ctx, db.write); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Close releases both connection pools. Safe to call once; the kernel
// starts no subprocess and opens no network listener, so Close is
// local-only cleanup.
func (db *DB) Close() error {
	werr := db.write.Close()
	rerr := db.read.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// Path returns the on-disk database file path.
func (db *DB) Path() string { return db.path }

// Reader returns the bounded read-only connection pool for queries.
func (db *DB) Reader() *sql.DB { return db.read }
