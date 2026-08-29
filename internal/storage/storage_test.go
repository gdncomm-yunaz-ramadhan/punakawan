package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func open(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func TestEmptyCreateAndReopen(t *testing.T) {
	db, path := open(t)
	if err := db.write.Ping(); err != nil {
		t.Fatalf("ping after create: %v", err)
	}
	db.Close()

	db2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := db2.write.Ping(); err != nil {
		t.Fatalf("ping after reopen: %v", err)
	}
}

func TestSchemaUpgradeIsIdempotent(t *testing.T) {
	_, path := open(t)
	// Reopening an already-migrated database must not re-apply or fail.
	db2, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen already-migrated db: %v", err)
	}
	defer db2.Close()

	var count int
	if err := db2.write.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one recorded migration")
	}
}

func TestIncompatibleSchemaRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := db.write.Exec(
		`INSERT INTO schema_migrations (version, name, checksum) VALUES (9999, 'from_the_future.sql', 'deadbeef')`,
	); err != nil {
		t.Fatalf("seed future migration: %v", err)
	}
	var before []string
	rows, err := db.write.Query(`SELECT version || ':' || checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read migrations before: %v", err)
	}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		before = append(before, s)
	}
	rows.Close()
	db.Close()

	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("expected Open to reject a database with a newer-than-known migration")
	}

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw after rejected Open: %v", err)
	}
	defer raw.Close()
	var after []string
	rows, err = raw.Query(`SELECT version || ':' || checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read migrations after: %v", err)
	}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		after = append(after, s)
	}
	rows.Close()

	if len(before) != len(after) {
		t.Fatalf("rejected open must not change recorded migrations: before=%v after=%v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("rejected open must not change recorded migrations: before=%v after=%v", before, after)
		}
	}
}

func TestModifiedMigrationRejected(t *testing.T) {
	_, path := open(t)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1`); err != nil {
		t.Fatalf("tamper checksum: %v", err)
	}
	db.Close()

	if _, err := Open(context.Background(), path); err == nil {
		t.Fatal("expected Open to reject a modified migration checksum")
	}
}

func TestBackupAndRestore(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	if err := db.Write(ctx, "seed-1", "seed row", func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS t (v TEXT)`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO t (v) VALUES ('hello')`)
		return err
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := db.Backup(ctx, backupPath); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	restored, err := Open(ctx, backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer restored.Close()

	var v string
	if err := restored.write.QueryRow(`SELECT v FROM t`).Scan(&v); err != nil {
		t.Fatalf("read restored row: %v", err)
	}
	if v != "hello" {
		t.Fatalf("restored value = %q, want hello", v)
	}
}

func TestWriteCommitsMutationAndAuditTogether(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	if _, err := db.write.Exec(`CREATE TABLE t (v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	if err := db.Write(ctx, "key-1", "insert one", func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO t (v) VALUES ('a')`)
		return err
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var rows, audits int
	db.write.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&rows)
	db.write.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE idempotency_key = 'key-1'`).Scan(&audits)
	if rows != 1 || audits != 1 {
		t.Fatalf("rows=%d audits=%d, want 1 and 1", rows, audits)
	}
}

func TestWriteRollsBackMutationAndAuditTogether(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	if _, err := db.write.Exec(`CREATE TABLE t (v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	failure := context.DeadlineExceeded
	err := db.Write(ctx, "key-2", "insert then fail", func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO t (v) VALUES ('a')`); err != nil {
			return err
		}
		return failure
	})
	if err != failure {
		t.Fatalf("Write error = %v, want %v", err, failure)
	}

	var rows, audits int
	db.write.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&rows)
	db.write.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE idempotency_key = 'key-2'`).Scan(&audits)
	if rows != 0 || audits != 0 {
		t.Fatalf("rows=%d audits=%d, want 0 and 0 after rollback", rows, audits)
	}
}

func TestDuplicateIdempotencyKeyIsHarmless(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	if _, err := db.write.Exec(`CREATE TABLE t (v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	write := func() error {
		return db.Write(ctx, "dup-key", "insert", func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO t (v) VALUES ('a')`)
			return err
		})
	}
	if err := write(); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := write(); err != ErrDuplicateWrite {
		t.Fatalf("second Write error = %v, want ErrDuplicateWrite", err)
	}

	var rows int
	db.write.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("rows = %d, want 1 (duplicate must not re-run fn)", rows)
	}
}

func TestConcurrentReadersDuringWrite(t *testing.T) {
	db, _ := open(t)
	ctx := context.Background()
	if _, err := db.write.Exec(`CREATE TABLE t (v INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.write.Exec(`INSERT INTO t (v) VALUES (1)`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	const readers = 4
	var wg sync.WaitGroup
	errs := make(chan error, readers+1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- db.Write(ctx, "concurrent-write", "bump", func(tx *sql.Tx) error {
			_, err := tx.Exec(`UPDATE t SET v = v + 1`)
			return err
		})
	}()

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v int
			errs <- db.Reader().QueryRowContext(ctx, `SELECT v FROM t LIMIT 1`).Scan(&v)
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent access error: %v", err)
		}
	}

	var final int
	db.write.QueryRow(`SELECT v FROM t`).Scan(&final)
	if final != 2 {
		t.Fatalf("final v = %d, want 2 (no lost update)", final)
	}
}

func TestRemoveExecutionApprovalsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			checksum   TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		)`); err != nil {
		t.Fatalf("bootstrap schema_migrations: %v", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	for _, m := range migrations {
		if m.version >= 26 {
			continue
		}
		if err := applyMigration(ctx, raw, m); err != nil {
			t.Fatalf("apply migration %04d: %v", m.version, err)
		}
	}

	if _, err := raw.ExecContext(ctx,
		`INSERT INTO approvals (project_id, id, data) VALUES ('proj-1', 'appr-1', '{"status":"pending"}')`,
	); err != nil {
		t.Fatalf("seed pending approval: %v", err)
	}
	if _, err := raw.ExecContext(ctx,
		`INSERT INTO delivery_cases (id, jira_source_key, jira_issue_key, status, created_at, updated_at)
		 VALUES ('case-1', 'jira:ABC-1', 'ABC-1', 'active', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed delivery_cases: %v", err)
	}
	if _, err := raw.ExecContext(ctx,
		`INSERT INTO jira_assessments (id, case_id, execution_id, session_id, snapshot_id, clarity, approval, rationale, assessed_at)
		 VALUES ('assess-1', 'case-1', 'exec-1', '', '', 'clear', 'approved', 'looks good', '2026-08-29T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed jira_assessments: %v", err)
	}
	if _, err := raw.ExecContext(ctx,
		`INSERT INTO github_pr_reviews (id, repository, pull_request_number, head_sha, findings_json, body, verdict, status, delivery_execution_id, external_review_id, created_at, updated_at, failure)
		 VALUES ('review-1', 'org/repo', 1, 'deadbeef', '[]', 'lgtm', 'APPROVE', 'approved', 'exec-1', '', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z', '')`,
	); err != nil {
		t.Fatalf("seed github_pr_reviews: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open (apply remaining migrations): %v", err)
	}
	defer db.Close()

	var approvalsTableCount int
	if err := db.write.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'approvals'`,
	).Scan(&approvalsTableCount); err != nil {
		t.Fatalf("check approvals table: %v", err)
	}
	if approvalsTableCount != 0 {
		t.Fatal("approvals table still exists after migration")
	}

	var clarity, rationale string
	if err := db.write.QueryRow(
		`SELECT clarity, rationale FROM jira_assessments WHERE id = 'assess-1'`,
	).Scan(&clarity, &rationale); err != nil {
		t.Fatalf("read migrated jira_assessments row: %v", err)
	}
	if clarity != "clear" || rationale != "looks good" {
		t.Fatalf("jira_assessments row = clarity=%q rationale=%q, want clarity=clear rationale=%q", clarity, rationale, "looks good")
	}

	var reviewStatus string
	if err := db.write.QueryRow(
		`SELECT status FROM github_pr_reviews WHERE id = 'review-1'`,
	).Scan(&reviewStatus); err != nil {
		t.Fatalf("read migrated github_pr_reviews row: %v", err)
	}
	if reviewStatus != "proposed" {
		t.Fatalf("github_pr_reviews status = %q, want proposed", reviewStatus)
	}

	if _, err := db.write.Exec(`UPDATE github_pr_reviews SET status = 'approved' WHERE id = 'review-1'`); err == nil {
		t.Fatal("expected the rebuilt github_pr_reviews CHECK constraint to reject status = 'approved'")
	}
}

func TestCheckLocationAllowsTempDir(t *testing.T) {
	if err := CheckLocation(filepath.Join(t.TempDir(), "x.db")); err != nil {
		t.Fatalf("CheckLocation on local temp dir: %v", err)
	}
}

func TestDataDirIsUnderPlatformConfigDir(t *testing.T) {
	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	if filepath.Base(dir) != "punakawan" {
		t.Fatalf("DataDir = %q, want a path ending in punakawan", dir)
	}
}
