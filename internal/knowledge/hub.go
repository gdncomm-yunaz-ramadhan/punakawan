package knowledge

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/ygrip/punakawan/internal/tools"
)

// projectIDPattern restricts a hub projectID to characters safe both as a
// filesystem path segment and, unavoidably, as a raw SQL identifier: Dolt's
// CREATE DATABASE has no parameterized form for identifiers, so this
// validation is the only guard against a malformed or hostile projectID
// reaching that statement.
var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// OpenInHub connects to (starting if necessary) a shared "hub" Dolt
// sql-server whose --data-dir is hubDir, and returns a Store bound to one
// project's database within it: hubDir/projectID (ADR-0020). This is the
// additive, opt-in alternative to Open's one-server-per-project model:
// multiple projects opened against the same hubDir share one sql-server
// process via the exact in-process refcounting Open already uses
// (serverRegistry), keyed here by hubDir instead of a single project's own
// data dir - the last Store closed, across every project sharing this
// hubDir, stops the process; every other project's Store closing first just
// decrements the shared refcount.
//
// The project's database is always created (if missing) via a live `CREATE
// DATABASE IF NOT EXISTS` against a running connection, never via a
// filesystem-level `dolt init` in hubDir/projectID. This was verified
// empirically, not assumed from documentation: Dolt's --data-dir database
// discovery only scans hubDir's immediate children at the server's own
// startup - a project subdirectory that appears after the server is already
// running is invisible to it ("database not found") until CREATE DATABASE
// is issued live, which registers it immediately with no restart. It was
// also verified that hubDir/projectID must stay completely untouched by us
// before that CREATE DATABASE runs - even an unrelated pre-existing empty
// subdirectory there makes Dolt silently fail to create the database - which
// is why per-project events live in a hubDir/<projectID>.events sibling
// directory, never inside hubDir/projectID itself.
func OpenInHub(sup *tools.Supervisor, hubDir, projectID string) (*Store, error) {
	if !projectIDPattern.MatchString(projectID) {
		return nil, fmt.Errorf("knowledge: OpenInHub: invalid projectID %q (must match %s)", projectID, projectIDPattern.String())
	}

	// Registry key is the shared server's own directory, not this project's
	// subdirectory, so every project opened against the same hubDir joins one
	// refcounted entry instead of each believing it owns a private server.
	key := filepath.Clean(hubDir)

	eventsDir := filepath.Join(hubDir, projectID+".events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return nil, fmt.Errorf("knowledge: create %s: %w", eventsDir, err)
	}
	eventsPath := filepath.Join(eventsDir, "knowledge-events.jsonl")

	if store, ok, err := joinHubServer(hubDir, key, projectID, eventsPath); err != nil {
		return nil, err
	} else if ok {
		return store, nil
	}

	if err := os.MkdirAll(hubDir, 0o755); err != nil {
		return nil, fmt.Errorf("knowledge: create hub dir %s: %w", hubDir, err)
	}

	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("knowledge: find free port: %w", err)
	}

	logPath := filepath.Join(hubDir, "dolt-sql-server.log")
	server, err := sup.StartBackground(tools.Spec{
		Name: "dolt",
		Args: []string{"sql-server", "-H", "127.0.0.1", "-P", fmt.Sprintf("%d", port), "--data-dir", hubDir},
		Dir:  hubDir,
	}, logPath)
	if err != nil {
		return nil, fmt.Errorf("knowledge: start hub dolt sql-server: %w", err)
	}

	neutral, err := waitForConnection(doltDSN(port, "information_schema"), 15*time.Second, server)
	if err != nil {
		_ = server.Stop()
		// Two processes can race between the reuse check and server startup;
		// if the other process won hubDir's write lock, join its server instead.
		if store, ok, joinErr := joinHubServer(hubDir, key, projectID, eventsPath); joinErr == nil && ok {
			return store, nil
		}
		return nil, fmt.Errorf("knowledge: connect to hub dolt sql-server: %w%s", err, startupLogSuffix(logPath))
	}
	if err := createHubDatabase(neutral, projectID); err != nil {
		_ = neutral.Close()
		_ = server.Stop()
		return nil, err
	}
	_ = neutral.Close()

	db, err := waitForConnection(doltDSN(port, projectID), 5*time.Second, server)
	if err != nil {
		_ = server.Stop()
		return nil, fmt.Errorf("knowledge: connect to newly created hub database %q: %w", projectID, err)
	}

	registerStartedServer(key, server)
	store := &Store{db: db, server: server, dataDir: key, project: projectID, eventsPath: eventsPath}
	if err := store.migrate(); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

// joinHubServer attempts to reuse an already-running hub server at hubDir:
// connects neutrally (information_schema always exists, unlike projectID's
// own database, which may not yet), ensures projectID's database exists
// (idempotent - fine whether this project is brand new to the hub or has
// been opened many times before), then reconnects scoped to it. ok=false
// with a nil error means no server is currently running at hubDir - not a
// failure, just "the caller should start one".
func joinHubServer(hubDir, key, projectID, eventsPath string) (*Store, bool, error) {
	neutral, err := connectExistingServer(hubDir, "information_schema", 750*time.Millisecond)
	if err != nil {
		return nil, false, nil
	}
	if err := createHubDatabase(neutral, projectID); err != nil {
		_ = neutral.Close()
		return nil, false, err
	}
	_ = neutral.Close()

	db, err := connectExistingServer(hubDir, projectID, 2*time.Second)
	if err != nil {
		return nil, false, fmt.Errorf("knowledge: connect to hub database %q after creating it: %w", projectID, err)
	}
	store := newReusedStore(key, projectID, db, eventsPath)
	if err := store.migrate(); err != nil {
		_ = store.Close()
		return nil, false, err
	}
	return store, true, nil
}

// createHubDatabase issues a live CREATE DATABASE against db - the mechanism
// an already-running hub server actually registers a new project database
// through (see OpenInHub's doc comment). projectID is validated by
// projectIDPattern before this is ever reached, since it cannot be
// parameterized as a SQL identifier.
func createHubDatabase(db *sql.DB, projectID string) error {
	if _, err := db.Exec("CREATE DATABASE IF NOT EXISTS `" + projectID + "`"); err != nil {
		return fmt.Errorf("knowledge: create hub database %q: %w", projectID, err)
	}
	return nil
}
