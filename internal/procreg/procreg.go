// Package procreg is punokawan-14yn.18's durable process-ownership
// registry: every process the daemon starts on behalf of delivery work
// (git, migration, shell, test, and agent invocations - none of which
// exist as call sites yet, since .3/.5 haven't been built) is recorded
// here before it is exposed as running, so a restart after an abrupt
// daemon crash can find and clean up genuine survivors.
//
// Process-tree termination itself is not reimplemented here - it
// reuses internal/tools' per-OS SIGTERM/SIGKILL (or taskkill)
// escalation via TerminateProcessTree/KillProcessTree. This package
// only adds what tools' in-memory Supervisor cannot survive on its own:
// durability across a crash, and PID-reuse-proof identity via
// internal/procident.
package procreg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/procident"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/tools"
)

const timeLayout = time.RFC3339Nano

// killGrace bounds how long Reconcile waits after a graceful terminate
// request before escalating to a forceful kill for one verified
// survivor. Kept short since AC2 requires the whole reconciliation pass
// to finish within ten seconds and there may be more than one orphan.
const killGrace = 2 * time.Second

// State values a Record can hold.
const (
	StateRunning   = "running"
	StateCompleted = "completed"
)

// Record is one process-ownership entry.
type Record struct {
	RunID          string
	LeaseID        string
	PID            int
	Executable     string
	StartTime      time.Time
	OwnershipToken string
	State          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Registry persists Records through the shared SQLite storage kernel.
type Registry struct {
	db *storage.DB
}

// New wraps an opened storage kernel database.
func New(db *storage.DB) *Registry { return &Registry{db: db} }

// Register persists rec with state StateRunning. Callers should call
// this immediately after starting the process rec describes - so its
// pid and OS-verified StartTime are already known - and before handing
// any reference to that process to code outside this call, satisfying
// "persist ownership before exposing work as running."
//
// rec.PID must have been started as its own process group leader (e.g.
// via tools.Supervisor, which already does this) - TerminateProcessTree
// and KillProcessTree signal the process *group* (kill(-pid, ...)), so a
// process left in its parent's group cannot be reconciled.
func (r *Registry) Register(ctx context.Context, rec Record) error {
	now := time.Now().UTC()
	return r.db.Write(ctx, "procreg-register-"+rec.RunID, "register owned process "+rec.RunID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO owned_processes (run_id, lease_id, pid, executable, start_time, ownership_token, state, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rec.RunID, rec.LeaseID, rec.PID, rec.Executable, rec.StartTime.Format(timeLayout), rec.OwnershipToken, StateRunning,
			now.Format(timeLayout), now.Format(timeLayout),
		)
		return err
	})
}

// Complete marks runID as no longer owned.
func (r *Registry) Complete(ctx context.Context, runID string) error {
	now := time.Now().UTC()
	return r.db.Write(ctx, "procreg-complete-"+runID, "complete owned process "+runID, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE owned_processes SET state = ?, updated_at = ? WHERE run_id = ?`,
			StateCompleted, now.Format(timeLayout), runID,
		)
		return err
	})
}

// ListRunning returns every record still marked StateRunning.
func (r *Registry) ListRunning(ctx context.Context) ([]Record, error) {
	rows, err := r.db.Reader().QueryContext(ctx,
		`SELECT run_id, lease_id, pid, executable, start_time, ownership_token, state, created_at, updated_at
		 FROM owned_processes WHERE state = ? ORDER BY created_at ASC`, StateRunning)
	if err != nil {
		return nil, fmt.Errorf("procreg: list running: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func scanRecord(row interface {
	Scan(dest ...interface{}) error
}) (Record, error) {
	var rec Record
	var startTime, createdAt, updatedAt string
	if err := row.Scan(&rec.RunID, &rec.LeaseID, &rec.PID, &rec.Executable, &startTime, &rec.OwnershipToken, &rec.State, &createdAt, &updatedAt); err != nil {
		return Record{}, err
	}
	var err error
	if rec.StartTime, err = time.Parse(timeLayout, startTime); err != nil {
		return Record{}, fmt.Errorf("procreg: parse start_time for %s: %w", rec.RunID, err)
	}
	if rec.CreatedAt, err = time.Parse(timeLayout, createdAt); err != nil {
		return Record{}, fmt.Errorf("procreg: parse created_at for %s: %w", rec.RunID, err)
	}
	if rec.UpdatedAt, err = time.Parse(timeLayout, updatedAt); err != nil {
		return Record{}, fmt.Errorf("procreg: parse updated_at for %s: %w", rec.RunID, err)
	}
	return rec, nil
}

// Result reports what Reconcile did with every record it examined.
type Result struct {
	Killed      []string // verified survivors that were terminated
	AlreadyGone []string // pid no longer named any process
	Preserved   []string // pid now names a different process (identity mismatch) - never killed
}

// Reconcile is called once at daemon startup (AC2: "removes verified
// survivors within ten seconds"). For every record still marked
// running from a previous daemon instance, it compares the recorded
// start time against the pid's current start time (procident):
//   - no current process at that pid: already gone, just mark completed.
//   - start times match: a genuine orphaned survivor from before the
//     crash; terminate it (escalating to a forceful kill after
//     killGrace), then mark completed.
//   - start times differ: the pid has been reused by an unrelated
//     process. That process is never touched (AC4) - it is reported in
//     Preserved as an anomaly for the caller to surface to an operator.
func (r *Registry) Reconcile(ctx context.Context) (Result, error) {
	running, err := r.ListRunning(ctx)
	if err != nil {
		return Result{}, err
	}

	var result Result
	for _, rec := range running {
		current, err := procident.StartTime(rec.PID)
		switch {
		case err != nil:
			result.AlreadyGone = append(result.AlreadyGone, rec.RunID)
		case !current.Equal(rec.StartTime):
			result.Preserved = append(result.Preserved, rec.RunID)
		default:
			killVerifiedSurvivor(rec.PID)
			result.Killed = append(result.Killed, rec.RunID)
		}
		if cerr := r.Complete(ctx, rec.RunID); cerr != nil {
			return result, errors.Join(err, cerr)
		}
	}
	return result, nil
}

func killVerifiedSurvivor(pid int) {
	_ = tools.TerminateProcessTree(pid)
	deadline := time.Now().Add(killGrace)
	for time.Now().Before(deadline) {
		if _, err := procident.StartTime(pid); err != nil {
			return // exited gracefully
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = tools.KillProcessTree(pid)
}
