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
	"strconv"
	"strings"
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
	dbName := filepath.Base(dataDir)

	// A prior MCP host may have exited without getting a chance to stop its
	// child Dolt process. Dolt deliberately keeps that healthy server alive
	// and records its port in sql-server.info, so reuse it instead of starting
	// a second server that can only fail on the repository's write lock.
	if db, err := connectExistingServer(dataDir, dbName, 750*time.Millisecond); err == nil {
		store := &Store{db: db}
		if err := store.migrate(); err != nil {
			_ = store.Close()
			return nil, err
		}
		return store, nil
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

	dsn := doltDSN(port, dbName)

	db, err := waitForConnection(dsn, 15*time.Second, server)
	if err != nil {
		_ = server.Stop()
		// Two Punakawan processes can race between the reuse check and server
		// startup. If the other process won the Dolt lock, its info file now
		// points at the server we should share.
		if existing, existingErr := connectExistingServer(dataDir, dbName, 2*time.Second); existingErr == nil {
			store := &Store{db: existing}
			if migrateErr := store.migrate(); migrateErr != nil {
				_ = store.Close()
				return nil, migrateErr
			}
			return store, nil
		}
		return nil, fmt.Errorf("knowledge: connect to dolt sql-server: %w%s", err, startupLogSuffix(logPath))
	}

	store := &Store{db: db, server: server}
	if err := store.migrate(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

func doltDSN(port int, dbName string) string {
	return fmt.Sprintf("root@tcp(127.0.0.1:%d)/%s?parseTime=true&timeout=500ms&readTimeout=500ms&writeTimeout=500ms", port, dbName)
}

func connectExistingServer(dataDir, dbName string, timeout time.Duration) (*sql.DB, error) {
	info, err := os.ReadFile(filepath.Join(dataDir, ".dolt", "sql-server.info"))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimSpace(string(info)), ":")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid dolt sql-server.info")
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid dolt sql-server port %q", parts[1])
	}
	return waitForConnection(doltDSN(port, dbName), timeout, nil)
}

func startupLogSuffix(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil || len(data) == 0 {
		return ""
	}
	const maxLogBytes = 4096
	if len(data) > maxLogBytes {
		data = data[len(data)-maxLogBytes:]
	}
	return fmt.Sprintf("; dolt startup log: %s", strings.TrimSpace(string(data)))
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

func waitForConnection(dsn string, timeout time.Duration, server *tools.BackgroundProcess) (*sql.DB, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if server != nil {
			select {
			case <-server.Done():
				return nil, fmt.Errorf("dolt sql-server exited before accepting connections: %w", server.WaitError())
			default:
			}
		}
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
	keepSharedServer := false
	if s.server != nil {
		var otherConnections int
		// If another Punakawan process reused this server, leave it running.
		// Its connection is evidence that stopping our child would break an
		// otherwise healthy MCP session. An orphan is safe and is reused by the
		// next Open call through sql-server.info.
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID <> CONNECTION_ID()`).Scan(&otherConnections); err == nil {
			keepSharedServer = otherConnections > 0
		}
	}
	dbErr := s.db.Close()
	if s.server == nil || keepSharedServer {
		return dbErr
	}
	stopErr := s.server.Stop()
	if dbErr != nil {
		return dbErr
	}
	return stopErr
}
