// Package planexec tracks the execution lifecycle of one plan step
// (internal/plan.PlanStep), for a project that wants plan-native step
// tracking instead of, or alongside, Beads or internal/taskstore. It is
// additive: nothing here reads or writes Beads/taskstore/tasks data, and
// nothing outside this package is required to use it.
//
// A step's readiness is computed the same way internal/taskstore computes
// task readiness: client-side and deterministic, by reading the owning
// plan's current steps (for each step's DependsOn list) and cross-
// referencing the status of each dependency's own Execution row - no
// external engine, no persisted "blocked" flag to keep in sync. Storage
// lives in the shared SQLite kernel (internal/storage, migration
// 0015_plan_step_executions.sql), one row per (plan_id, step_id),
// overwritten in place as the step's status transitions - unlike
// internal/plan.Store's append-only revisions, an Execution's whole
// history is a single mutable row.
package planexec

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
)

const timeLayout = time.RFC3339Nano

// ErrNotFound is returned by Get (and internally by Claim/Complete/Reopen)
// when no execution exists with the given id.
var ErrNotFound = errors.New("planexec: not found")

// Status is one Execution's current lifecycle state.
type Status string

const (
	// StatusReady means the step has not been claimed and every step it
	// depends on is committed (or it has no dependencies).
	StatusReady Status = "ready"
	// StatusClaimed means a worker is currently doing the step's work.
	StatusClaimed Status = "claimed"
	// StatusCommitted means the step's work is done, with evidence
	// attached elsewhere by whatever workflow is producing that evidence -
	// this domain only tracks that completion happened, not the evidence
	// itself.
	StatusCommitted Status = "committed"
	// StatusBlocked means at least one step this one depends on is not
	// yet committed. Never stored: Get and ListReady compute it live from
	// the plan's dependency graph rather than persisting it, so it can
	// never go stale relative to a dependency's own status changing.
	StatusBlocked Status = "blocked"
	// StatusReopened means a previously committed step was reopened (e.g.
	// a review found a regression in already-completed work) and needs to
	// be redone.
	StatusReopened Status = "reopened"
)

// Execution is one plan step's execution lifecycle: whether it has been
// claimed, completed, or reopened, and by whom.
type Execution struct {
	ID           string
	PlanID       string
	PlanRevision int
	StepID       string
	Status       Status
	ClaimedBy    string
	ClaimedAt    *time.Time
	CompletedAt  *time.Time
	ReopenReason string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Store is the Execution persistence boundary, over the shared SQLite
// storage kernel. It reads plan step definitions and dependency edges
// through plans, which it never writes to - a Plan's own content stays
// entirely internal/plan's responsibility.
type Store struct {
	db    *storage.DB
	plans *plan.Store
}

// NewStore wraps an opened storage kernel database and the Plan store used
// to resolve a plan's steps and their DependsOn edges.
func NewStore(db *storage.DB, plans *plan.Store) *Store {
	return &Store{db: db, plans: plans}
}

// querier is the common subset of *sql.DB and *sql.Tx this package needs:
// dependency and row lookups run the same query whether they are part of
// a larger write transaction or a plain read.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Create returns stepID's Execution within planID/revision, creating one
// in status Ready if none exists yet. Idempotent-ish: calling it again for
// a step that already has an execution returns that existing row
// unchanged (not a new one and not an error), since a plan can be
// re-invoked with the same steps more than once.
func (s *Store) Create(ctx context.Context, planID string, revision int, stepID string) (Execution, error) {
	planID = strings.TrimSpace(planID)
	stepID = strings.TrimSpace(stepID)
	if planID == "" {
		return Execution{}, fmt.Errorf("planexec: create: plan id is required")
	}
	if stepID == "" {
		return Execution{}, fmt.Errorf("planexec: create: step id is required")
	}

	p, err := s.plans.Get(ctx, planID)
	if err != nil {
		return Execution{}, fmt.Errorf("planexec: create: load plan %s: %w", planID, err)
	}
	if _, ok := findStep(p, stepID); !ok {
		return Execution{}, fmt.Errorf("planexec: create: plan %s has no step with id %q", planID, stepID)
	}

	id := newExecutionID()
	now := time.Now().UTC()
	// Deterministic on (planID, stepID), unlike plan.Store.Save's random
	// per-call writeKey: a second Create for the same pair must be
	// recognized as the same logical request and fall back to reading the
	// row the first call inserted, not attempt (and fail) a second insert.
	key := fmt.Sprintf("planexec-create-%s-%s", planID, stepID)
	err = s.db.Write(ctx, key, fmt.Sprintf("create plan step execution %s/%s", planID, stepID), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO plan_step_executions (id, plan_id, plan_revision, step_id, status, claimed_by, claimed_at, completed_at, reopen_reason, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, '', NULL, NULL, '', ?, ?)`,
			id, planID, revision, stepID, string(StatusReady), now.Format(timeLayout), now.Format(timeLayout),
		); err != nil {
			return fmt.Errorf("planexec: create %s/%s: %w", planID, stepID, err)
		}
		return nil
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.getByPlanAndStep(ctx, planID, stepID)
	}
	if err != nil {
		return Execution{}, err
	}
	return Execution{
		ID: id, PlanID: planID, PlanRevision: revision, StepID: stepID,
		Status: StatusReady, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Claim atomically moves executionID from Ready or Reopened to Claimed.
// Fails if the step is not in one of those statuses, or if its
// dependencies are not all committed (i.e. it is actually blocked, even
// though the stored status alone does not say so - see StatusBlocked).
func (s *Store) Claim(ctx context.Context, executionID, claimedBy string) (Execution, error) {
	executionID = strings.TrimSpace(executionID)
	claimedBy = strings.TrimSpace(claimedBy)
	if executionID == "" {
		return Execution{}, fmt.Errorf("planexec: claim: execution id is required")
	}
	if claimedBy == "" {
		return Execution{}, fmt.Errorf("planexec: claim: claimed_by is required")
	}

	key := writeKey()
	var result Execution
	err := s.db.Write(ctx, key, "claim plan step execution "+executionID, func(tx *sql.Tx) error {
		row, err := s.getRow(ctx, tx, executionID)
		if err != nil {
			return err
		}
		if row.Status != StatusReady && row.Status != StatusReopened {
			return fmt.Errorf("planexec: claim %s: not claimable (status is %s, claimed_by %q)", executionID, row.Status, row.ClaimedBy)
		}
		satisfied, err := s.dependenciesSatisfied(ctx, tx, row.PlanID, row.StepID)
		if err != nil {
			return err
		}
		if !satisfied {
			return fmt.Errorf("planexec: claim %s: blocked - not every dependency is committed yet", executionID)
		}

		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx,
			`UPDATE plan_step_executions SET status = ?, claimed_by = ?, claimed_at = ?, updated_at = ? WHERE id = ?`,
			string(StatusClaimed), claimedBy, now.Format(timeLayout), now.Format(timeLayout), executionID,
		); err != nil {
			return fmt.Errorf("planexec: claim %s: %w", executionID, err)
		}
		row.Status = StatusClaimed
		row.ClaimedBy = claimedBy
		row.ClaimedAt = &now
		row.UpdatedAt = now
		result = row
		return nil
	})
	if err != nil {
		return Execution{}, err
	}
	return result, nil
}

// Complete moves executionID to Committed, attaching CompletedAt. Calling
// it again on an already-committed execution is a no-op that returns the
// existing row unchanged, rather than an error - a plan re-invocation
// should not fail just because a step was already finished.
func (s *Store) Complete(ctx context.Context, executionID string) (Execution, error) {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return Execution{}, fmt.Errorf("planexec: complete: execution id is required")
	}

	key := writeKey()
	var result Execution
	err := s.db.Write(ctx, key, "complete plan step execution "+executionID, func(tx *sql.Tx) error {
		row, err := s.getRow(ctx, tx, executionID)
		if err != nil {
			return err
		}
		if row.Status == StatusCommitted {
			result = row
			return nil
		}
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx,
			`UPDATE plan_step_executions SET status = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
			string(StatusCommitted), now.Format(timeLayout), now.Format(timeLayout), executionID,
		); err != nil {
			return fmt.Errorf("planexec: complete %s: %w", executionID, err)
		}
		row.Status = StatusCommitted
		row.CompletedAt = &now
		row.UpdatedAt = now
		result = row
		return nil
	})
	if err != nil {
		return Execution{}, err
	}
	return result, nil
}

// Reopen moves a Committed executionID back to Reopened, recording why.
// Fails if the execution is not currently Committed - there is nothing
// meaningful to reopen otherwise.
func (s *Store) Reopen(ctx context.Context, executionID, reason string) (Execution, error) {
	executionID = strings.TrimSpace(executionID)
	reason = strings.TrimSpace(reason)
	if executionID == "" {
		return Execution{}, fmt.Errorf("planexec: reopen: execution id is required")
	}
	if reason == "" {
		return Execution{}, fmt.Errorf("planexec: reopen: reason is required")
	}

	key := writeKey()
	var result Execution
	err := s.db.Write(ctx, key, "reopen plan step execution "+executionID, func(tx *sql.Tx) error {
		row, err := s.getRow(ctx, tx, executionID)
		if err != nil {
			return err
		}
		if row.Status != StatusCommitted {
			return fmt.Errorf("planexec: reopen %s: status is %s, must be committed to reopen", executionID, row.Status)
		}
		now := time.Now().UTC()
		if _, err := tx.ExecContext(ctx,
			`UPDATE plan_step_executions SET status = ?, reopen_reason = ?, completed_at = NULL, updated_at = ? WHERE id = ?`,
			string(StatusReopened), reason, now.Format(timeLayout), executionID,
		); err != nil {
			return fmt.Errorf("planexec: reopen %s: %w", executionID, err)
		}
		row.Status = StatusReopened
		row.ReopenReason = reason
		row.CompletedAt = nil
		row.UpdatedAt = now
		result = row
		return nil
	})
	if err != nil {
		return Execution{}, err
	}
	return result, nil
}

// Get returns executionID's current Execution, with Status resolved to
// StatusBlocked (in place of its stored Ready/Reopened value) when its
// dependencies are not all committed yet.
func (s *Store) Get(ctx context.Context, executionID string) (Execution, error) {
	executionID = strings.TrimSpace(executionID)
	if executionID == "" {
		return Execution{}, fmt.Errorf("planexec: get: execution id is required")
	}
	row, err := s.getRow(ctx, s.db.Reader(), executionID)
	if err != nil {
		return Execution{}, err
	}
	if row.Status == StatusReady || row.Status == StatusReopened {
		satisfied, err := s.dependenciesSatisfied(ctx, s.db.Reader(), row.PlanID, row.StepID)
		if err != nil {
			return Execution{}, err
		}
		if !satisfied {
			row.Status = StatusBlocked
		}
	}
	return row, nil
}

// ListReady returns every planID execution that is actually claimable
// right now: stored status Ready or Reopened, and every step it depends
// on is committed.
func (s *Store) ListReady(ctx context.Context, planID string) ([]Execution, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return nil, fmt.Errorf("planexec: list ready: plan id is required")
	}
	p, err := s.plans.Get(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("planexec: list ready: load plan %s: %w", planID, err)
	}
	dependsByStep := make(map[string][]string, len(p.Steps))
	for _, st := range p.Steps {
		dependsByStep[st.ID] = st.DependsOn
	}

	rows, err := s.allForPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	statusByStep := make(map[string]Status, len(rows))
	for _, r := range rows {
		statusByStep[r.StepID] = r.Status
	}

	ready := make([]Execution, 0, len(rows))
	for _, r := range rows {
		if r.Status != StatusReady && r.Status != StatusReopened {
			continue
		}
		satisfied := true
		for _, dep := range dependsByStep[r.StepID] {
			if statusByStep[dep] != StatusCommitted {
				satisfied = false
				break
			}
		}
		if satisfied {
			ready = append(ready, r)
		}
	}
	return ready, nil
}

// dependenciesSatisfied reports whether every step stepID depends on (per
// the owning plan's current DependsOn list) has an execution row in
// planID with status Committed. A dependency with no execution row yet is
// treated as not satisfied.
func (s *Store) dependenciesSatisfied(ctx context.Context, q querier, planID, stepID string) (bool, error) {
	p, err := s.plans.Get(ctx, planID)
	if err != nil {
		return false, fmt.Errorf("planexec: load plan %s: %w", planID, err)
	}
	step, ok := findStep(p, stepID)
	if !ok {
		return false, fmt.Errorf("planexec: plan %s has no step with id %q", planID, stepID)
	}
	for _, dep := range step.DependsOn {
		var status string
		err := q.QueryRowContext(ctx, `SELECT status FROM plan_step_executions WHERE plan_id = ? AND step_id = ?`, planID, dep).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("planexec: check dependency %s of %s: %w", dep, stepID, err)
		}
		if Status(status) != StatusCommitted {
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) getByPlanAndStep(ctx context.Context, planID, stepID string) (Execution, error) {
	var id string
	err := s.db.Reader().QueryRowContext(ctx, `SELECT id FROM plan_step_executions WHERE plan_id = ? AND step_id = ?`, planID, stepID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, fmt.Errorf("planexec: create %s/%s: %w", planID, stepID, ErrNotFound)
	}
	if err != nil {
		return Execution{}, fmt.Errorf("planexec: create %s/%s: %w", planID, stepID, err)
	}
	return s.Get(ctx, id)
}

func (s *Store) getRow(ctx context.Context, q querier, executionID string) (Execution, error) {
	var (
		exec                   Execution
		status                 string
		claimedAt, completedAt sql.NullString
		createdAt, updatedAt   string
	)
	err := q.QueryRowContext(ctx, `
SELECT id, plan_id, plan_revision, step_id, status, claimed_by, claimed_at, completed_at, reopen_reason, created_at, updated_at
FROM plan_step_executions WHERE id = ?`, executionID).Scan(
		&exec.ID, &exec.PlanID, &exec.PlanRevision, &exec.StepID, &status, &exec.ClaimedBy,
		&claimedAt, &completedAt, &exec.ReopenReason, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Execution{}, fmt.Errorf("planexec: get %s: %w", executionID, ErrNotFound)
	}
	if err != nil {
		return Execution{}, fmt.Errorf("planexec: get %s: %w", executionID, err)
	}
	exec.Status = Status(status)
	if exec.CreatedAt, err = time.Parse(timeLayout, createdAt); err != nil {
		return Execution{}, fmt.Errorf("planexec: parse created_at: %w", err)
	}
	if exec.UpdatedAt, err = time.Parse(timeLayout, updatedAt); err != nil {
		return Execution{}, fmt.Errorf("planexec: parse updated_at: %w", err)
	}
	if claimedAt.Valid {
		t, err := time.Parse(timeLayout, claimedAt.String)
		if err != nil {
			return Execution{}, fmt.Errorf("planexec: parse claimed_at: %w", err)
		}
		exec.ClaimedAt = &t
	}
	if completedAt.Valid {
		t, err := time.Parse(timeLayout, completedAt.String)
		if err != nil {
			return Execution{}, fmt.Errorf("planexec: parse completed_at: %w", err)
		}
		exec.CompletedAt = &t
	}
	return exec, nil
}

func (s *Store) allForPlan(ctx context.Context, planID string) ([]Execution, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
SELECT id, plan_id, plan_revision, step_id, status, claimed_by, claimed_at, completed_at, reopen_reason, created_at, updated_at
FROM plan_step_executions WHERE plan_id = ?`, planID)
	if err != nil {
		return nil, fmt.Errorf("planexec: list %s: %w", planID, err)
	}
	defer rows.Close()

	var out []Execution
	for rows.Next() {
		var (
			exec                   Execution
			status                 string
			claimedAt, completedAt sql.NullString
			createdAt, updatedAt   string
		)
		if err := rows.Scan(&exec.ID, &exec.PlanID, &exec.PlanRevision, &exec.StepID, &status, &exec.ClaimedBy,
			&claimedAt, &completedAt, &exec.ReopenReason, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("planexec: list %s: scan: %w", planID, err)
		}
		exec.Status = Status(status)
		if exec.CreatedAt, err = time.Parse(timeLayout, createdAt); err != nil {
			return nil, fmt.Errorf("planexec: parse created_at: %w", err)
		}
		if exec.UpdatedAt, err = time.Parse(timeLayout, updatedAt); err != nil {
			return nil, fmt.Errorf("planexec: parse updated_at: %w", err)
		}
		if claimedAt.Valid {
			t, err := time.Parse(timeLayout, claimedAt.String)
			if err != nil {
				return nil, fmt.Errorf("planexec: parse claimed_at: %w", err)
			}
			exec.ClaimedAt = &t
		}
		if completedAt.Valid {
			t, err := time.Parse(timeLayout, completedAt.String)
			if err != nil {
				return nil, fmt.Errorf("planexec: parse completed_at: %w", err)
			}
			exec.CompletedAt = &t
		}
		out = append(out, exec)
	}
	return out, rows.Err()
}

func findStep(p plan.Plan, stepID string) (plan.PlanStep, bool) {
	for _, st := range p.Steps {
		if st.ID == stepID {
			return st, true
		}
	}
	return plan.PlanStep{}, false
}

func newExecutionID() string { return ulid.Make().String() }

// writeKey mints a fresh idempotency key for a state-transition call
// (Claim/Complete/Reopen), mirroring internal/plan's writeKey: each of
// these calls is a distinct transition attempt, not a replay of a prior
// one, so a random key is correct here - Create is the one operation in
// this package where a deterministic key is what "idempotent-ish" needs.
func writeKey() string { return ulid.Make().String() }
