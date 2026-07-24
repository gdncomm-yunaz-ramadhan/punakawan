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
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/ygrip/punakawan/internal/tools"
)

// Store is a Dolt-backed durable knowledge store.
type Store struct {
	db     *sql.DB
	server *tools.BackgroundProcess

	// dataDir is the registry key (see serverRegistry) when this Store
	// participates in the in-process refcount for a server this process
	// started - i.e. the starter or an in-process reuser. Empty for a Store
	// bound to a server started by a different OS process (reused via
	// sql-server.info), which this process must never stop.
	dataDir string

	eventsPath string
	eventsMu   sync.Mutex
}

// serverRegistry tracks the Dolt sql-servers this process started, keyed by
// data directory, with a reference count of the live Stores using each one.
// It closes the ownership gap behind punokawan-q9r.6.1: a Store that reuses a
// server another Store in THIS process already started joins the refcount, and
// the last Store to close stops the server - instead of every reuser leaving
// it running (the old server==nil early return in Close) and the starter
// orphaning it whenever a transient PROCESSLIST count was non-zero. Servers we
// did not start (reused across OS processes via sql-server.info) are absent
// here and are never stopped by us.
var serverRegistry = struct {
	mu      sync.Mutex
	servers map[string]*sharedServer
}{servers: map[string]*sharedServer{}}

type sharedServer struct {
	proc *tools.BackgroundProcess
	refs int
}

// joinInProcessServer increments the refcount for key when this process
// started its server, returning true so the caller records key on its Store
// and participates in Close's decrement. Returns false for a cross-process
// reuse, which this process does not own.
func joinInProcessServer(key string) bool {
	serverRegistry.mu.Lock()
	defer serverRegistry.mu.Unlock()
	if ss, ok := serverRegistry.servers[key]; ok {
		ss.refs++
		return true
	}
	return false
}

// registerStartedServer records a server this process just started, with an
// initial refcount of 1 for the starting Store.
func registerStartedServer(key string, proc *tools.BackgroundProcess) {
	serverRegistry.mu.Lock()
	defer serverRegistry.mu.Unlock()
	serverRegistry.servers[key] = &sharedServer{proc: proc, refs: 1}
}

// newReusedStore builds a Store for a server reached via connectExistingServer
// and joins the in-process refcount when this process started that server.
func newReusedStore(key string, db *sql.DB, eventsPath string) *Store {
	st := &Store{db: db, eventsPath: eventsPath}
	if joinInProcessServer(key) {
		st.dataDir = key
	}
	return st
}

// Open ensures dataDir is an initialized Dolt repository, starts a Dolt
// sql-server rooted at it, connects to it, and applies the knowledge schema.
func Open(sup *tools.Supervisor, dataDir string) (*Store, error) {
	if err := ensureInitialized(sup, dataDir); err != nil {
		return nil, err
	}
	dbName := filepath.Base(dataDir)
	// Registry key: a stable, per-process-consistent form of dataDir so the
	// starter and any in-process reuser agree on the same sharedServer entry.
	key := filepath.Clean(dataDir)

	// Per §10.2's directory layout, knowledge-events.jsonl lives in events/
	// alongside (not inside) the knowledge/ Dolt data directory.
	eventsDir := filepath.Join(filepath.Dir(dataDir), "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return nil, fmt.Errorf("knowledge: create %s: %w", eventsDir, err)
	}
	eventsPath := filepath.Join(eventsDir, "knowledge-events.jsonl")

	// A prior MCP host may have exited without getting a chance to stop its
	// child Dolt process. Dolt deliberately keeps that healthy server alive
	// and records its port in sql-server.info, so reuse it instead of starting
	// a second server that can only fail on the repository's write lock.
	if db, err := connectExistingServer(dataDir, dbName, 750*time.Millisecond); err == nil {
		store := newReusedStore(key, db, eventsPath)
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
			store := newReusedStore(key, existing, eventsPath)
			if migrateErr := store.migrate(); migrateErr != nil {
				_ = store.Close()
				return nil, migrateErr
			}
			return store, nil
		}
		return nil, fmt.Errorf("knowledge: connect to dolt sql-server: %w%s", err, startupLogSuffix(logPath))
	}

	registerStartedServer(key, server)
	store := &Store{db: db, server: server, dataDir: key, eventsPath: eventsPath}
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
		// Pin the pool to a single connection. Besides serializing access to
		// the single-writer Dolt store, this keeps Close's cross-process guard
		// honest: with one connection, information_schema.PROCESSLIST minus our
		// own CONNECTION_ID() counts only genuine other-process clients, not
		// idle connections from our own pool (punokawan-q9r.6.1).
		db.SetMaxOpenConns(1)
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

	// schema_migrations records which versioned, non-idempotent-by-default
	// migrations (the secondary indexes below) have already been applied, so a
	// reused open - the common case, since a Dolt sql-server is shared across
	// processes and every Open re-runs migrate() - skips work it has done
	// before instead of re-issuing DDL on every panel/tool startup. The base
	// CREATE TABLE IF NOT EXISTS statements above stay unconditional: they are
	// cheap and idempotent by construction.
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(64) PRIMARY KEY,
  applied_at DATETIME NOT NULL
)`); err != nil {
		return fmt.Errorf("knowledge: create schema_migrations: %w", err)
	}

	// Secondary indexes backing the panel's counts and first-page browses
	// (punokawan-rit, Phase 4 §11): every List query orders by updated_at DESC
	// and filters on type/status/validity_state, so these keep the panel off a
	// full-corpus scan. The version key is the index name; each is applied once
	// per store and then recorded so later opens skip it.
	indexMigrations := []struct {
		table   string
		index   string
		columns string
	}{
		{"knowledge_records", "idx_knowledge_updated_at", "updated_at"},
		{"knowledge_records", "idx_knowledge_type_updated_at", "type, updated_at"},
		{"knowledge_records", "idx_knowledge_status_updated_at", "status, updated_at"},
		{"knowledge_records", "idx_knowledge_validity_updated_at", "validity_state, updated_at"},
	}
	for _, m := range indexMigrations {
		applied, err := s.migrationApplied(m.index)
		if err != nil {
			return fmt.Errorf("knowledge: check migration %s: %w", m.index, err)
		}
		if applied {
			continue
		}
		if err := s.ensureIndex(m.table, m.index, m.columns); err != nil {
			return err
		}
		if err := s.recordMigration(m.index); err != nil {
			return fmt.Errorf("knowledge: record migration %s: %w", m.index, err)
		}
	}
	return nil
}

// migrationApplied reports whether version has already been recorded in
// schema_migrations.
func (s *Store) migrationApplied(version string) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// recordMigration marks version as applied. ON DUPLICATE KEY keeps it
// idempotent under the narrow race of two processes migrating a fresh store
// at once.
func (s *Store) recordMigration(version string) error {
	_, err := s.db.Exec(`
INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)
ON DUPLICATE KEY UPDATE applied_at = VALUES(applied_at)`, version, time.Now().UTC())
	return err
}

// indexExists reports whether an index named idx exists on table, via the
// SQL-standard information_schema.statistics view (the portable fallback for
// engines that reject CREATE INDEX IF NOT EXISTS).
func (s *Store) indexExists(table, idx string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM information_schema.statistics WHERE table_name = ? AND index_name = ?`,
		table, idx).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ensureIndex creates idx on table(columns) if it does not already exist.
// Dolt accepts CREATE INDEX IF NOT EXISTS, which is the primary path; should a
// future engine reject that syntax, it falls back to a plain CREATE guarded by
// an information_schema.statistics existence check so the migration is still
// idempotent.
func (s *Store) ensureIndex(table, idx, columns string) error {
	if exists, err := s.indexExists(table, idx); err == nil && exists {
		return nil
	}
	if _, err := s.db.Exec(fmt.Sprintf("CREATE INDEX IF NOT EXISTS `%s` ON `%s` (%s)", idx, table, columns)); err != nil {
		// IF NOT EXISTS may be unsupported; retry the plain form after
		// re-checking existence so a concurrent creator doesn't fail us.
		if exists, exErr := s.indexExists(table, idx); exErr == nil && exists {
			return nil
		}
		if _, err2 := s.db.Exec(fmt.Sprintf("CREATE INDEX `%s` ON `%s` (%s)", idx, table, columns)); err2 != nil {
			if exists, exErr := s.indexExists(table, idx); exErr == nil && exists {
				return nil
			}
			return fmt.Errorf("knowledge: create index %s: %w", idx, err2)
		}
	}
	return nil
}

// Close disconnects from the database and, when this Store is the last
// in-process user of a Dolt sql-server this process started, stops it. A
// server reused from a different OS process (via sql-server.info) is left
// running - and even the last in-process holder leaves it running if another
// OS process is still connected, so cross-process sharing keeps working. See
// serverRegistry and punokawan-q9r.6.1.
func (s *Store) Close() error {
	var proc *tools.BackgroundProcess
	lastHolder := false
	if s.dataDir != "" {
		serverRegistry.mu.Lock()
		if ss, ok := serverRegistry.servers[s.dataDir]; ok {
			ss.refs--
			if ss.refs <= 0 {
				delete(serverRegistry.servers, s.dataDir)
				proc = ss.proc
				lastHolder = true
			}
		}
		serverRegistry.mu.Unlock()
	}

	// Cross-process guard, evaluated outside the registry lock while our db
	// handle is still open: if a client other than us is on the server, it is
	// another OS process that reused it, so orphan it for reuse instead of
	// stopping it. This narrows but does not fully close a TOCTOU window: once
	// we have deleted the registry entry, a concurrent in-process Open for the
	// same key can no longer join the refcount and instead connects as a
	// "foreign" reuser (dataDir=""); if it connects in the gap between this
	// PROCESSLIST check and proc.Stop(), it will lose its server. This is
	// acceptable because concurrent cold opens of the identical knowledge
	// directory within one process do not occur in practice (the panel and
	// tests each open distinct directories), and the cross-process case has the
	// same inherent window. Do not assume this is race-free under adversarial
	// same-key concurrency.
	stop := lastHolder && proc != nil && !s.otherClientsConnected()

	dbErr := s.db.Close()
	if stop {
		if stopErr := proc.Stop(); stopErr != nil && dbErr == nil {
			return stopErr
		}
	}
	return dbErr
}

// otherClientsConnected reports whether a client other than this Store's own
// connection is on the Dolt server - evidence that a different OS process
// reused it and that stopping it now would break a live session. A query
// error is treated as "no other clients" so a transient failure never blocks
// reaping our own child.
func (s *Store) otherClientsConnected() bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID <> CONNECTION_ID()`).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
