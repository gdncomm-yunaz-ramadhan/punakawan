package knowledge

import (
	"fmt"
	"strings"
)

// CommitWorkingSet creates a Dolt version-control commit covering whatever is
// currently pending in this database's working set (via CALL DOLT_COMMIT),
// and returns the new commit's hash.
//
// This exists because Put/Delete only execute ordinary SQL transactions
// against dolt sql-server's working set - verified empirically, Dolt does
// not auto-commit a version-history snapshot per SQL statement or
// transaction. Without an explicit call like this one, deleted rows are
// only one `dolt checkout` away from being unrecoverable (a later, unrelated
// commit can silently absorb them) rather than genuinely revertable. Callers
// that want a "this delete is undoable" guarantee (e.g. delete_knowledge,
// reset_project_knowledge) must call this immediately after their delete
// completes, while nothing else has run against the same working set. It is
// still queryable/revertable via ordinary Dolt tooling afterward: `SELECT ...
// FROM <table> AS OF '<hash>'` reads pre-delete state without mutating
// anything, and `dolt checkout <hash> -- <table>` in the project's knowledge
// Dolt directory restores it.
//
// Returns ("", nil) if there is nothing pending to commit - CALL DOLT_COMMIT
// errors on a clean working set, and a caller that skips the empty case gets
// a spurious error for a call that legitimately deleted nothing (e.g. every
// requested id was already absent).
func (s *Store) CommitWorkingSet(message string) (hash string, err error) {
	rows, err := s.db.Query("CALL DOLT_COMMIT('-Am', ?)", message)
	if err != nil {
		if isNothingToCommit(err) {
			return "", nil
		}
		return "", fmt.Errorf("knowledge: commit working set: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		if err := rows.Scan(&hash); err != nil {
			return "", fmt.Errorf("knowledge: commit working set: scan commit hash: %w", err)
		}
	}
	return hash, rows.Err()
}

// isNothingToCommit reports whether err is Dolt's response to CALL
// DOLT_COMMIT on a clean working set. Matched by substring since the driver
// surfaces this as a plain SQL error, not a typed/wrapped one.
func isNothingToCommit(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "nothing to commit")
}
