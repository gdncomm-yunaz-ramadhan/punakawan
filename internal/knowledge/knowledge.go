// Package knowledge implements the durable, provenance-backed knowledge
// store on top of the shared SQLite kernel (internal/storage).
//
// The kernel is one database shared by every local project checkout, so every
// row is scoped by an explicit projectID (see internal/storage/migrations/
// 0008_knowledge.sql): two projects can mint the identical record id without
// colliding or leaking into each other's Get/List/Related results. Writes go
// through the kernel's idempotent, audited Write helper (one domain mutation
// plus one audit_log row per transaction); reads use the kernel's WAL-
// concurrent reader pool.
package knowledge

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ygrip/punakawan/internal/storage"
)

// ErrNotFound is returned by Get when no record exists for the given id.
var ErrNotFound = errors.New("knowledge: record not found")

// TimeLayout is the fixed-width, UTC RFC3339 form (always 9 fractional
// digits, always a "Z" zone) used for every updated_at value the store
// writes. The fixed width is what makes SQLite's default byte-wise text
// comparison agree with chronological order, which the keyset pagination in
// ListRecords relies on for its (updated_at DESC, id ASC) seek predicate.
// Exported so other writers of this same column format timestamps
// identically rather than duplicating the layout string.
const TimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Store is a SQLite-backed durable knowledge store, scoped to one project
// within the shared storage kernel. Schema migration happens once, centrally,
// when the kernel opens (internal/storage/migrations/0008_knowledge.sql) - a
// Store never creates its own tables.
type Store struct {
	db        *storage.DB
	projectID string

	// eventsPath is this project's append-only knowledge-events.jsonl (§10.2),
	// a sibling of the shared database file. It is deliberately outside the
	// SQLite database: it is an external audit trail for tailing, not a source
	// of truth Punakawan reads back for correctness.
	eventsPath string
	eventsMu   sync.Mutex
}

// New wraps db, scoping every read and write to projectID. Same shape as
// taskstore.New, since both are siblings over the one shared kernel.
func New(db *storage.DB, projectID string) *Store {
	return &Store{
		db:         db,
		projectID:  projectID,
		eventsPath: filepath.Join(filepath.Dir(db.Path()), "knowledge-events-"+safeFileSegment(projectID)+".jsonl"),
	}
}

// writeKey returns a fresh random idempotency key. Put/Supersede/Delete are
// each a distinct mutation that must always take effect (Put is an upsert, not
// a create), so unlike a one-shot create they never want the kernel's replay
// dedup to skip them - a unique key per call guarantees fn runs and appends
// its own audit row every time.
func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("knowledge: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// safeFileSegment reduces an arbitrary project id to a filename-safe segment
// for the per-project events file, so a project id carrying a path separator
// or other awkward character cannot escape the data directory.
func safeFileSegment(s string) string {
	seg := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
	if seg == "" {
		return "default"
	}
	return seg
}
