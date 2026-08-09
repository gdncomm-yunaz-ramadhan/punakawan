package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// CommitWorkingSet records a checkpoint for a batch of mutations just applied
// (e.g. a delete_knowledge / reset_project_knowledge run) and returns a stable
// hash identifying it.
//
// The Dolt-backed store this replaced had a real version-control commit here,
// and callers surfaced its hash as an output field. The shared SQLite kernel
// has no per-statement version history to commit; every mutation is already
// durable the moment its Write transaction commits. So this now computes a
// content hash over the commit message and the moment it ran, appends one
// audit_log entry recording the message (keyed by that hash so a genuine
// replay is a no-op), and returns the hash. The hash is an opaque identifier
// for this operation in the audit trail, not a revertable snapshot pointer -
// the caller's only use of it was ever to echo it back in a response field.
func (s *Store) CommitWorkingSet(message string) (hash string, err error) {
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	hash = ContentHash([]byte(message + "\x00" + ts))

	err = s.db.Write(context.Background(), hash, message, func(*sql.Tx) error { return nil })
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return "", fmt.Errorf("knowledge: commit working set: %w", err)
	}
	return hash, nil
}
