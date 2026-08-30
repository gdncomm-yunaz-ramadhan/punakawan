package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

type migration struct {
	version  int
	name     string
	checksum string
	sql      string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("storage: read embedded migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("storage: malformed migration filename %q", e.Name())
		}
		var version int
		fmt.Sscanf(m[1], "%d", &version)
		content, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("storage: read migration %q: %w", e.Name(), err)
		}
		sum := sha256.Sum256(content)
		out = append(out, migration{
			version:  version,
			name:     e.Name(),
			checksum: hex.EncodeToString(sum[:]),
			sql:      string(content),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("storage: duplicate migration version %04d", out[i].version)
		}
	}
	return out, nil
}

// migrate applies pending schema migrations, or rejects an unknown,
// modified, or newer-than-known schema without changing the database.
func migrate(ctx context.Context, db *sql.DB) error {
	embedded, err := loadMigrations()
	if err != nil {
		return err
	}
	maxKnown := 0
	if len(embedded) > 0 {
		maxKnown = embedded[len(embedded)-1].version
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			checksum   TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		)`); err != nil {
		return fmt.Errorf("storage: bootstrap schema_migrations: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("storage: read schema_migrations: %w", err)
	}
	applied := map[int]migration{}
	for rows.Next() {
		var m migration
		if err := rows.Scan(&m.version, &m.name, &m.checksum); err != nil {
			rows.Close()
			return fmt.Errorf("storage: scan schema_migrations: %w", err)
		}
		applied[m.version] = m
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("storage: read schema_migrations: %w", err)
	}
	rows.Close()

	embeddedByVersion := make(map[int]migration, len(embedded))
	for _, m := range embedded {
		embeddedByVersion[m.version] = m
	}

	// Validate every applied row before changing anything: an unknown
	// version, a newer version than this binary knows, or a mismatched
	// checksum on an already-applied migration must reject up front.
	for version, got := range applied {
		if version > maxKnown {
			return fmt.Errorf("storage: database has migration %04d newer than this binary knows (max %04d); refusing to open", version, maxKnown)
		}
		want, ok := embeddedByVersion[version]
		if !ok {
			return fmt.Errorf("storage: database references unknown migration %04d; refusing to open", version)
		}
		if want.checksum != got.checksum {
			return fmt.Errorf("storage: migration %04d (%s) was modified after being applied; refusing to open", version, want.name)
		}
	}

	for _, m := range embedded {
		if _, ok := applied[m.version]; ok {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	// A table rebuild (DROP + CREATE-under-the-old-name + RENAME) that
	// other tables still hold a foreign key into cannot be done under
	// PRAGMA defer_foreign_keys alone: that pragma only defers immediate
	// per-statement violation errors, it does not re-validate the final
	// schema at COMMIT - SQLite's own deferred-constraint counter is
	// incremented by the DROP and is never decremented just because a new
	// table with matching rows appears under the same name, so COMMIT
	// still fails. SQLite's documented fix for this exact rebuild pattern
	// is disabling foreign_keys for the whole operation instead, and that
	// pragma is a no-op once a transaction is already open, so it must be
	// toggled on this connection before BeginTx/after the transaction ends
	// - not from inside the migration's own SQL text.
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("storage: disable foreign keys for migration %04d: %w", m.version, err)
	}
	defer db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: begin migration %04d: %w", m.version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("storage: apply migration %04d (%s): %w", m.version, m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, checksum) VALUES (?, ?, ?)`,
		m.version, m.name, m.checksum,
	); err != nil {
		return fmt.Errorf("storage: record migration %04d: %w", m.version, err)
	}
	var violations []string
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("storage: foreign_key_check after migration %04d: %w", m.version, err)
	}
	for rows.Next() {
		var table, rowid, referredTable, fkid sql.NullString
		if err := rows.Scan(&table, &rowid, &referredTable, &fkid); err != nil {
			rows.Close()
			return fmt.Errorf("storage: scan foreign_key_check row for migration %04d: %w", m.version, err)
		}
		violations = append(violations, fmt.Sprintf("%s row %s -> missing %s", table.String, rowid.String, referredTable.String))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("storage: read foreign_key_check for migration %04d: %w", m.version, err)
	}
	rows.Close()
	if len(violations) > 0 {
		return fmt.Errorf("storage: migration %04d (%s) left dangling foreign keys: %v", m.version, m.name, violations)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit migration %04d: %w", m.version, err)
	}
	return nil
}
