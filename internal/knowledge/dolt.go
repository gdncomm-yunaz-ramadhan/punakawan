// Package knowledge implements the durable, provenance-backed knowledge
// store on top of Dolt, per punakawan-go-typescript-detailed-plan.md §7.
//
// Dolt runs as an external process (§3.3, §14.1: "Dolt server or CLI"),
// supervised the same way as any other tool (§11.4), and is queried over
// its MySQL wire protocol via the standard go-sql-driver/mysql client.
package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/ygrip/punakawan/internal/tools"
)

// Store is a Dolt-backed durable knowledge store.
type Store struct {
	db     *sql.DB
	server *tools.BackgroundProcess
}

// Open ensures dataDir is an initialized Dolt repository, starts a Dolt
// sql-server rooted at it, connects to it, and applies the knowledge schema.
func Open(sup *tools.Supervisor, dataDir string) (*Store, error) {
	if err := ensureInitialized(sup, dataDir); err != nil {
		return nil, err
	}

	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("knowledge: find free port: %w", err)
	}

	logPath := filepath.Join(filepath.Dir(dataDir), "dolt-sql-server.log")
	server, err := sup.StartBackground(tools.Spec{
		Name: "dolt",
		Args: []string{"sql-server", "-H", "127.0.0.1", "-P", fmt.Sprintf("%d", port), "--data-dir", dataDir},
		Dir:  dataDir,
	}, logPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge: start dolt sql-server: %w", err)
	}

	dbName := filepath.Base(dataDir)
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/%s?parseTime=true", port, dbName)

	db, err := waitForConnection(dsn, 15*time.Second)
	if err != nil {
		_ = server.Stop()
		return nil, fmt.Errorf("knowledge: connect to dolt sql-server: %w", err)
	}

	store := &Store{db: db, server: server}
	if err := store.migrate(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func ensureInitialized(sup *tools.Supervisor, dataDir string) error {
	if _, err := os.Stat(filepath.Join(dataDir, ".dolt")); err == nil {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("knowledge: create data dir: %w", err)
	}
	// The knowledge store is Punakawan's own internal database, not a
	// human-authored repository, so dolt init is given a fixed identity
	// rather than depending on a global `dolt config` having been set.
	res, err := sup.Run(context.Background(), tools.Spec{
		Name: "dolt",
		Args: []string{"init", "--name", "punakawan", "--email", "punakawan@localhost"},
		Dir:  dataDir,
	})
	if err != nil {
		return fmt.Errorf("knowledge: dolt init: %w", err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("knowledge: dolt init failed: %s", res.Stderr)
	}
	return nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitForConnection(dsn string, timeout time.Duration) (*sql.DB, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			lastErr = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if pingErr := db.Ping(); pingErr != nil {
			lastErr = pingErr
			db.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		return db, nil
	}
	return nil, fmt.Errorf("timed out waiting for dolt sql-server to accept connections: %w", lastErr)
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS knowledge_records (
  id VARCHAR(255) PRIMARY KEY,
  type VARCHAR(64) NOT NULL,
  status VARCHAR(64) NOT NULL,
  validity_state VARCHAR(32) NOT NULL,
  data JSON NOT NULL,
  updated_at DATETIME NOT NULL
)`)
	if err != nil {
		return fmt.Errorf("knowledge: create schema: %w", err)
	}

	// Normalized index of each record's §7.2 relations, so relations that
	// cross repository (and therefore knowledge-record-id) boundaries can be
	// traversed with a query instead of scanning every record's JSON blob.
	_, err = s.db.Exec(`
CREATE TABLE IF NOT EXISTS knowledge_relations (
  from_id VARCHAR(255) NOT NULL,
  type VARCHAR(64) NOT NULL,
  to_id VARCHAR(255) NOT NULL,
  PRIMARY KEY (from_id, type, to_id)
)`)
	if err != nil {
		return fmt.Errorf("knowledge: create relations schema: %w", err)
	}
	return nil
}

// Close disconnects from the database and stops the Dolt sql-server.
func (s *Store) Close() error {
	dbErr := s.db.Close()
	stopErr := s.server.Stop()
	if dbErr != nil {
		return dbErr
	}
	return stopErr
}
