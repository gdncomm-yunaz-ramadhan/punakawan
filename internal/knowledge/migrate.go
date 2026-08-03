package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ygrip/punakawan/internal/hub"
)

// Sentinel errors MigrateToHub returns for its refused-rather-than-worked-around
// preconditions, so callers (and their own tests) can distinguish them from an
// unexpected filesystem or Dolt failure.
var (
	ErrNothingToMigrate    = errors.New("knowledge: root has no legacy Dolt knowledge store to migrate")
	ErrAlreadyMigrated     = errors.New("knowledge: root already points at a hub")
	ErrLegacyServerRunning = errors.New("knowledge: a dolt sql-server is currently serving this project's legacy store; stop the running punakawan session for this project (panel, mcp serve, etc.) and retry")
	ErrHubServerRunning    = errors.New("knowledge: the hub's dolt sql-server is already running; a project can only be added to a live hub's data directory when that server next (re)starts - stop every punakawan session sharing this hub and retry, or wait until it restarts on its own")
	ErrHubProjectExists    = errors.New("knowledge: a database already exists at that project id in the hub")
)

// MigrateToHub moves root's legacy per-project Dolt knowledge store (§10.2 -
// which per ADR-0018 already holds the taskstore tables too, since both share
// one database) into hubDir as database projectID, then records the pointer
// (hub.Write) so future opens use OpenInHub instead of the legacy per-project
// path (ADR-0020). This is deliberately explicit and one-time - nothing calls
// it automatically - and it never deletes the legacy directory: migration
// copies, so the original is left in place as an inert backup until whoever
// ran this is satisfied and removes it by hand.
//
// Dolt's --data-dir discovery only scans a hub's immediate children at the
// server's own startup (verified empirically, not assumed - see hub.go's
// OpenInHub doc comment for the sibling finding that live CREATE DATABASE
// cannot register a directory that already exists on disk, only a genuinely
// new one). A directory copied in while the hub server is already running is
// invisible to it until the server restarts, and restarting a hub server that
// may be serving other active projects is too disruptive to do automatically
// here - so this function refuses outright if the hub server is currently up,
// rather than silently producing a migrated project nothing can query yet.
func MigrateToHub(root, hubDir, projectID string) error {
	if !projectIDPattern.MatchString(projectID) {
		return fmt.Errorf("knowledge: MigrateToHub: invalid projectID %q (must match %s)", projectID, projectIDPattern.String())
	}

	legacyDir := filepath.Join(root, ".punakawan", "knowledge")
	if _, err := os.Stat(filepath.Join(legacyDir, ".dolt")); err != nil {
		return fmt.Errorf("%w: %s has no .dolt", ErrNothingToMigrate, legacyDir)
	}

	if _, ok, err := hub.Lookup(root); err != nil {
		return fmt.Errorf("knowledge: MigrateToHub: %w", err)
	} else if ok {
		return ErrAlreadyMigrated
	}

	if db, err := connectExistingServer(legacyDir, "knowledge", 500*time.Millisecond); err == nil {
		_ = db.Close()
		return ErrLegacyServerRunning
	}
	if db, err := connectExistingServer(hubDir, "information_schema", 500*time.Millisecond); err == nil {
		_ = db.Close()
		return ErrHubServerRunning
	}

	hubProjectDir := filepath.Join(hubDir, projectID)
	if _, err := os.Stat(hubProjectDir); err == nil {
		return ErrHubProjectExists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("knowledge: MigrateToHub: stat %s: %w", hubProjectDir, err)
	}

	if err := os.MkdirAll(hubDir, 0o755); err != nil {
		return fmt.Errorf("knowledge: MigrateToHub: create hub dir %s: %w", hubDir, err)
	}
	if err := os.CopyFS(hubProjectDir, os.DirFS(legacyDir)); err != nil {
		return fmt.Errorf("knowledge: MigrateToHub: copy %s to %s: %w", legacyDir, hubProjectDir, err)
	}

	// Legacy knowledge-events.jsonl (§10.2: a sibling of the knowledge/ dir,
	// not inside it) moves to the hub's own per-project events sibling
	// (hubDir/<projectID>.events), matching OpenInHub's layout.
	legacyEvents := filepath.Join(root, ".punakawan", "events", "knowledge-events.jsonl")
	if data, err := os.ReadFile(legacyEvents); err == nil {
		hubEventsDir := filepath.Join(hubDir, projectID+".events")
		if err := os.MkdirAll(hubEventsDir, 0o755); err != nil {
			return fmt.Errorf("knowledge: MigrateToHub: create %s: %w", hubEventsDir, err)
		}
		if err := os.WriteFile(filepath.Join(hubEventsDir, "knowledge-events.jsonl"), data, 0o644); err != nil {
			return fmt.Errorf("knowledge: MigrateToHub: write events file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("knowledge: MigrateToHub: read %s: %w", legacyEvents, err)
	}

	if err := hub.Write(root, hub.Ref{HubDir: hubDir, ProjectID: projectID}); err != nil {
		return fmt.Errorf("knowledge: MigrateToHub: record hub pointer: %w", err)
	}
	return nil
}

// ImportToHub seeds a hub project database from root/.punakawan/knowledge -
// the git-portable snapshot ExportFromHub produces (ADR-0020's "restore on a
// new machine" case). It is exactly MigrateToHub's own preconditions and
// mechanism: both take a valid, static Dolt repository at that path and
// copy it into hubDir/projectID, refusing if the hub server is currently up
// (see MigrateToHub's doc comment for why) or a hub-ref/target already
// exists. The two are named separately because they answer different
// questions a caller asks - "adopt my existing live per-project store" vs.
// "restore from a committed snapshot" - not because the underlying
// operation differs.
func ImportToHub(root, hubDir, projectID string) error {
	return MigrateToHub(root, hubDir, projectID)
}
