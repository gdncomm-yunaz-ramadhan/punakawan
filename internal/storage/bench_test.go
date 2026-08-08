package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// These benchmarks are the reproducible baseline required by
// punokawan-14yn.14 acceptance criterion 7: cold-open latency, write
// latency, query latency, database file size, and incremental RSS.
// Run with: go test ./internal/storage/... -bench=. -benchmem -run=^$

func BenchmarkColdOpen(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		path := filepath.Join(b.TempDir(), "bench.db")
		db, err := Open(ctx, path)
		if err != nil {
			b.Fatalf("Open: %v", err)
		}
		db.Close()
	}
}

func BenchmarkWrite(b *testing.B) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := db.write.Exec(`CREATE TABLE t (v INTEGER)`); err != nil {
		b.Fatalf("create table: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "bench-" + strconv.Itoa(i)
		if err := db.Write(ctx, key, "bench row", func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO t (v) VALUES (?)`, i)
			return err
		}); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}

func BenchmarkQuery(b *testing.B) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := db.write.Exec(`CREATE TABLE t (v INTEGER)`); err != nil {
		b.Fatalf("create table: %v", err)
	}
	for i := 0; i < 1000; i++ {
		db.write.Exec(`INSERT INTO t (v) VALUES (?)`, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v int
		if err := db.Reader().QueryRowContext(ctx, `SELECT v FROM t WHERE rowid = ?`, (i%1000)+1).Scan(&v); err != nil {
			b.Fatalf("query: %v", err)
		}
	}
}

func BenchmarkDatabaseSizeAndRSS(b *testing.B) {
	ctx := context.Background()
	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	path := filepath.Join(b.TempDir(), "bench.db")
	db, err := Open(ctx, path)
	if err != nil {
		b.Fatalf("Open: %v", err)
	}
	if _, err := db.write.Exec(`CREATE TABLE t (v INTEGER)`); err != nil {
		b.Fatalf("create table: %v", err)
	}
	for i := 0; i < 1000; i++ {
		db.write.Exec(`INSERT INTO t (v) VALUES (?)`, i)
	}
	db.Close()

	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)

	info, err := os.Stat(path)
	if err != nil {
		b.Fatalf("stat: %v", err)
	}
	b.ReportMetric(float64(info.Size()), "db-bytes/op")
	b.ReportMetric(float64(after.HeapAlloc)-float64(before.HeapAlloc), "incremental-heap-bytes/op")
}
