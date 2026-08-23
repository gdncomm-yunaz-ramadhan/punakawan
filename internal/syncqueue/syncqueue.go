// Package syncqueue records outbound adapter-write failures (e.g. a Jira
// sync call rejected or dropped by the network) so they can be found and
// retried later, per punakawan-architecture-enhancement-plan.md §9.4/Phase
// 5 and punokawan-nbz: today internal/adapters.Gate either succeeds or
// hard-errors on a write, with no trace of the failure left anywhere once
// the calling tool's own error return is discarded.
//
// A sync-queue entry is deliberately inert with respect to Beads: nothing
// in this package is called by commit_task or finish_task_execution, and
// nothing here blocks them - a task's BD-side completion was already a
// separate MCP tool call from the Jira-write tools that fail into this
// queue, so recording a failure here changes nothing about whether that
// task can be marked done.
//
// Entries now live in the shared SQLite storage kernel (internal/storage),
// scoped to one project, rather than a per-workspace JSONL file. History
// stays append-only: resolving an entry appends a new
// record with the same Id rather than mutating the original, so Current
// folds to the latest record per id while List returns full history.
package syncqueue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
)

// Status is a queue entry's current state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusResolved  Status = "resolved"
	StatusAbandoned Status = "abandoned"
)

// Entry is one outbound adapter write that failed, kept for retry.
type Entry struct {
	// Id is caller-supplied and deterministic per (adapter, op,
	// issue_id_or_key) - see Queue.Enqueue's doc comment for why re-using
	// the same id across retries matters.
	Id           string         `json:"id"`
	RunId        string         `json:"run_id"`
	Adapter      string         `json:"adapter"`
	Op           string         `json:"op"`
	Params       map[string]any `json:"params,omitempty"`
	IssueIdOrKey string         `json:"issue_id_or_key,omitempty"`
	Error        string         `json:"error"`
	Attempts     int            `json:"attempts"`
	Status       Status         `json:"status"`
	// ConflictsWith names another still-pending entry's id that targets the
	// same (Adapter, Op, IssueIdOrKey) - e.g. two queued writes both trying
	// to transition the same Jira issue. Retrying both blindly could apply
	// them out of order; a caller/human should look at both before retrying
	// either.
	ConflictsWith string     `json:"conflicts_with,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
}

// Queue appends and reads sync-queue entries for one project within the shared
// storage kernel. Schema migration happens once, centrally, when the kernel
// opens (internal/storage/migrations/0011_syncqueue.sql) - a Queue never
// creates its own tables. History is append-only: resolving an entry appends a
// new record with the same Id rather than mutating the original, so Current
// folds to the latest record per id while List returns full history.
type Queue struct {
	db        *storage.DB
	projectID string
}

// New wraps db, scoping every read and write to projectID.
func New(db *storage.DB, projectID string) *Queue {
	return &Queue{db: db, projectID: projectID}
}

// writeKey returns a fresh random idempotency key. Append is a genuine append -
// the same entry id may be written more than once (a failure, then a later
// resolution, then a re-enqueued retry) and each such write must always take
// effect - so every call wants a unique key rather than the kernel's replay
// dedup collapsing them.
func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("syncqueue: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Append writes one entry record to the queue.
func (q *Queue) Append(e Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("syncqueue: encode entry %s: %w", e.Id, err)
	}

	ctx := context.Background()
	key, err := writeKey()
	if err != nil {
		return err
	}
	err = q.db.Write(ctx, key, "append sync queue entry "+e.Id, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO sync_queue_entries (project_id, id, data) VALUES (?, ?, ?)`,
			q.projectID, e.Id, string(data)); err != nil {
			return fmt.Errorf("syncqueue: append %s: %w", e.Id, err)
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return err
	}
	return nil
}

// List returns the full append-only history of entry records, in the order
// they were written.
func (q *Queue) List() ([]Entry, error) {
	rows, err := q.db.Reader().Query(
		`SELECT data FROM sync_queue_entries WHERE project_id = ? ORDER BY seq ASC`, q.projectID)
	if err != nil {
		return nil, fmt.Errorf("syncqueue: list: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("syncqueue: scan entry: %w", err)
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, fmt.Errorf("syncqueue: decode entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("syncqueue: iterate entries: %w", err)
	}
	return entries, nil
}

// Current folds the append-only history to the latest record per id.
func (q *Queue) Current() (map[string]Entry, error) {
	all, err := q.List()
	if err != nil {
		return nil, err
	}
	latest := make(map[string]Entry, len(all))
	for _, e := range all {
		latest[e.Id] = e
	}
	return latest, nil
}

// Pending returns every entry whose latest state is still pending.
func (q *Queue) Pending() ([]Entry, error) {
	current, err := q.Current()
	if err != nil {
		return nil, err
	}
	var pending []Entry
	for _, e := range current {
		if e.Status == StatusPending {
			pending = append(pending, e)
		}
	}
	return pending, nil
}

// Enqueue records a failed adapter write for retry. e.Id, e.Status,
// e.Attempts, and e.ConflictsWith are computed here and overwrite whatever
// the caller set:
//
//   - If an entry with e.Id already exists (any status), Attempts is that
//     entry's Attempts + 1, so re-enqueuing the same failing write across
//     retries accumulates attempt history under one id instead of growing
//     the queue unboundedly with near-duplicate entries.
//   - Status is always set to StatusPending - Enqueue only ever records a
//     failure, never a resolution (see Resolve for that).
//   - ConflictsWith is set to the id of any other entry that is currently
//     pending for the same (Adapter, Op, IssueIdOrKey), so a caller
//     reviewing the queue can see two queued writes might race if both are
//     retried.
//
// The conflict scan runs over Current, which is already scoped to this
// Queue's project, so an entry belonging to another project can never be
// picked up as a conflict here.
func (q *Queue) Enqueue(e Entry) (Entry, error) {
	current, err := q.Current()
	if err != nil {
		return Entry{}, err
	}

	if existing, ok := current[e.Id]; ok {
		e.Attempts = existing.Attempts + 1
	} else {
		e.Attempts = 1
	}
	e.Status = StatusPending
	e.ConflictsWith = ""
	for id, other := range current {
		if id == e.Id || other.Status != StatusPending {
			continue
		}
		if other.Adapter == e.Adapter && other.Op == e.Op && other.IssueIdOrKey == e.IssueIdOrKey {
			e.ConflictsWith = id
			break
		}
	}

	if err := q.Append(e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Resolve marks the entry identified by id as resolved or abandoned,
// appending a new record with the same id per the append-only convention.
func (q *Queue) Resolve(id string, status Status) error {
	if status != StatusResolved && status != StatusAbandoned {
		return fmt.Errorf("syncqueue: resolve %q: status must be resolved or abandoned, got %q", id, status)
	}
	current, err := q.Current()
	if err != nil {
		return err
	}
	e, ok := current[id]
	if !ok {
		return fmt.Errorf("syncqueue: no entry %q", id)
	}
	if e.Status != StatusPending {
		return fmt.Errorf("syncqueue: entry %q is already %s", id, e.Status)
	}

	now := time.Now().UTC()
	e.Status = status
	e.ResolvedAt = &now
	return q.Append(e)
}
