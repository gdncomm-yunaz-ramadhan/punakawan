// Package taskstore is Punakawan's Beads-less fallback task graph: a
// SQLite-backed store (internal/storage) for projects that have no .beads
// directory. When a project does not use Beads, the panel's
// task board and submit_task_graph persist to and read from here instead, so
// tasks and their dependency graph are still tracked - without mutating the
// project (no git init, no CLAUDE.md, no hooks that `bd init` would write).
//
// The kernel is one database shared by every local project checkout, so
// every row is scoped by an explicit projectID (see internal/storage/
// migrations/0003_taskstore.sql): two projects can mint the identical task
// id without colliding or leaking into each other's List/Get results.
//
// The read/write shapes mirror internal/beads exactly (beads.ReadyIssue,
// beads.Issue, beads.CreateTaskOptions) so the panel's TaskReader and the
// tasks.GenerateGraph write path can switch to this store without any change
// to the contract types they already produce.
package taskstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/beads"
	"github.com/ygrip/punakawan/internal/storage"
)

const timeLayout = time.RFC3339Nano

// closedStatuses are the task statuses that count as "not blocking" when
// deciding readiness, mirroring how bd treats a closed dependency as
// satisfied. Everything else is treated as still-open.
var closedStatuses = map[string]bool{"closed": true, "done": true, "resolved": true, "completed": true}

// blockingDepTypes are the dependency types whose unresolved target blocks a
// task from being ready, matching bd's own vocabulary (a task "depends on" /
// is "blocked by" its target). parent-child is structural, not blocking.
var blockingDepTypes = map[string]bool{"blocks": true, "blocked-by": true, "requires": true}

// Store is a SQLite-backed fallback task store, scoped to one project within
// the shared storage kernel. Schema migration happens once, centrally, when
// the kernel opens (internal/storage/migrations/0003_taskstore.sql) - a
// Store never creates its own tables.
type Store struct {
	db        *storage.DB
	projectID string
}

// New wraps db, scoping every read and write to projectID.
func New(db *storage.DB, projectID string) *Store {
	return &Store{db: db, projectID: projectID}
}

// CreateInput describes a task to create. It mirrors the fields
// beads.CreateTaskOptions + the title/description that beads.CreateTask takes,
// so the write path can pass the same values to either backend.
type CreateInput struct {
	Title              string
	Description        string
	Type               string
	Parent             string
	Labels             []string
	AcceptanceCriteria []string
	ExternalRef        string
	Priority           int
}

// Create inserts a new task and returns its generated id.
func (s *Store) Create(ctx context.Context, in CreateInput) (string, error) {
	if strings.TrimSpace(in.Title) == "" {
		return "", fmt.Errorf("taskstore: create: title is required")
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	issueType := in.Type
	if issueType == "" {
		issueType = "task"
	}
	labels, err := json.Marshal(in.Labels)
	if err != nil {
		return "", fmt.Errorf("taskstore: marshal labels: %w", err)
	}
	now := time.Now().UTC().Format(timeLayout)

	err = s.db.Write(ctx, "taskstore-create-"+s.projectID+"-"+id, "create task "+id, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO tasks (project_id, id, title, description, acceptance_criteria, status, priority, issue_type, owner, assignee, labels, parent, external_ref, created_at, created_by, updated_at, closed_at)
VALUES (?, ?, ?, ?, ?, 'open', ?, ?, '', '', ?, ?, ?, ?, 'punakawan', ?, NULL)`,
			s.projectID, id, in.Title, in.Description, strings.Join(in.AcceptanceCriteria, "\n"), in.Priority, issueType,
			string(labels), in.Parent, in.ExternalRef, now, now); err != nil {
			return fmt.Errorf("taskstore: insert task: %w", err)
		}
		if in.Parent != "" {
			// Record the hierarchy as a parent-child edge so the dependency
			// graph mirrors what bd emits.
			if err := addDependencyTx(ctx, tx, s.projectID, id, in.Parent, "parent-child"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return "", err
	}
	return id, nil
}

// AddDependency records that fromID depends on (is blocked by) toID, matching
// beads.AddDependency's direction. Idempotent on the (project, from, to,
// type) key.
func (s *Store) AddDependency(ctx context.Context, fromID, toID, depType string) error {
	if depType == "" {
		depType = "blocks"
	}
	key := fmt.Sprintf("taskstore-dep-%s-%s-%s-%s", s.projectID, fromID, toID, depType)
	err := s.db.Write(ctx, key, "add dependency "+fromID+" -> "+toID, func(tx *sql.Tx) error {
		return addDependencyTx(ctx, tx, s.projectID, fromID, toID, depType)
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return nil
	}
	return err
}

func addDependencyTx(ctx context.Context, tx *sql.Tx, projectID, fromID, toID, depType string) error {
	// The primary key is (project_id, from_id, to_id, type): a differing
	// type is a different row, not a conflict, so plain INSERT OR IGNORE
	// (rather than an upsert) is the correct translation of the prior
	// MySQL "ON DUPLICATE KEY UPDATE type = VALUES(type)", which could
	// never actually change type either.
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO task_deps (project_id, from_id, to_id, type) VALUES (?, ?, ?, ?)`,
		projectID, fromID, toID, depType,
	); err != nil {
		return fmt.Errorf("taskstore: add dependency %s -> %s: %w", fromID, toID, err)
	}
	return nil
}

// row is the scanned shape of a tasks row.
type row struct {
	id, title, description, acceptance, status, issueType, owner, assignee, labels, parent, externalRef, createdBy string
	priority                                                                                                       int
	createdAt, updatedAt                                                                                           time.Time
	closedAt                                                                                                       sql.NullString
}

func (s *Store) allRows(ctx context.Context) ([]row, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
SELECT id, title, description, acceptance_criteria, status, priority, issue_type, owner, assignee, labels, parent, external_ref, created_at, created_by, updated_at, closed_at
FROM tasks WHERE project_id = ? ORDER BY created_at ASC`, s.projectID)
	if err != nil {
		return nil, fmt.Errorf("taskstore: query tasks: %w", err)
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var r row
		var createdAt, updatedAt string
		if err := rows.Scan(&r.id, &r.title, &r.description, &r.acceptance, &r.status, &r.priority, &r.issueType,
			&r.owner, &r.assignee, &r.labels, &r.parent, &r.externalRef, &createdAt, &r.createdBy, &updatedAt, &r.closedAt); err != nil {
			return nil, fmt.Errorf("taskstore: scan task: %w", err)
		}
		if r.createdAt, err = time.Parse(timeLayout, createdAt); err != nil {
			return nil, fmt.Errorf("taskstore: parse created_at: %w", err)
		}
		if r.updatedAt, err = time.Parse(timeLayout, updatedAt); err != nil {
			return nil, fmt.Errorf("taskstore: parse updated_at: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// deps returns every dependency edge, grouped by from_id.
func (s *Store) allDeps(ctx context.Context) (map[string][]beads.ReadyDependency, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `SELECT from_id, to_id, type FROM task_deps WHERE project_id = ?`, s.projectID)
	if err != nil {
		return nil, fmt.Errorf("taskstore: query deps: %w", err)
	}
	defer rows.Close()
	byFrom := map[string][]beads.ReadyDependency{}
	for rows.Next() {
		var from, to, typ string
		if err := rows.Scan(&from, &to, &typ); err != nil {
			return nil, fmt.Errorf("taskstore: scan dep: %w", err)
		}
		byFrom[from] = append(byFrom[from], beads.ReadyDependency{IssueId: from, DependsOnId: to, Type: typ})
	}
	return byFrom, rows.Err()
}

func parseLabels(s string) []string {
	if s == "" || s == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func (r row) toReadyIssue(deps []beads.ReadyDependency) beads.ReadyIssue {
	return beads.ReadyIssue{
		ID:           r.id,
		Title:        r.title,
		Description:  r.description,
		Status:       r.status,
		Priority:     r.priority,
		IssueType:    r.issueType,
		Owner:        r.owner,
		Assignee:     r.assignee,
		Labels:       parseLabels(r.labels),
		Parent:       r.parent,
		Dependencies: deps,
		CreatedAt:    r.createdAt.Format(time.RFC3339),
		CreatedBy:    r.createdBy,
		UpdatedAt:    r.updatedAt.Format(time.RFC3339),
		ExternalRef:  r.externalRef,
	}
}

// List returns every task as a beads.ReadyIssue plus the set of ready ids,
// exactly the two inputs tasksnapshot.BuildSnapshot needs - so the panel's
// TaskReader builds an identical snapshot whether the backend is bd or this
// store. Ready means: the task is open and every blocking dependency's target
// is closed (mirroring bd ready).
func (s *Store) List(ctx context.Context) ([]beads.ReadyIssue, map[string]bool, error) {
	rows, err := s.allRows(ctx)
	if err != nil {
		return nil, nil, err
	}
	depsByFrom, err := s.allDeps(ctx)
	if err != nil {
		return nil, nil, err
	}
	statusByID := make(map[string]string, len(rows))
	for _, r := range rows {
		statusByID[r.id] = r.status
	}
	issues := make([]beads.ReadyIssue, 0, len(rows))
	ready := map[string]bool{}
	for _, r := range rows {
		issues = append(issues, r.toReadyIssue(depsByFrom[r.id]))
		if closedStatuses[r.status] {
			continue
		}
		blocked := false
		for _, d := range depsByFrom[r.id] {
			if blockingDepTypes[d.Type] && !closedStatuses[statusByID[d.DependsOnId]] {
				blocked = true
				break
			}
		}
		if !blocked {
			ready[r.id] = true
		}
	}
	return issues, ready, nil
}

// Get returns a single task as a beads.Issue, with both its dependencies (what
// it is blocked by) and dependents (what is blocked by it), matching
// beads.Show's shape.
func (s *Store) Get(ctx context.Context, id string) (beads.Issue, error) {
	rows, err := s.allRows(ctx)
	if err != nil {
		return beads.Issue{}, err
	}
	byID := make(map[string]row, len(rows))
	for _, r := range rows {
		byID[r.id] = r
	}
	self, ok := byID[id]
	if !ok {
		return beads.Issue{}, fmt.Errorf("taskstore: task %q not found", id)
	}
	depsByFrom, err := s.allDeps(ctx)
	if err != nil {
		return beads.Issue{}, err
	}
	related := func(other, depType string) beads.RelatedIssue {
		r := byID[other]
		return beads.RelatedIssue{ID: r.id, Title: r.title, Status: r.status, Priority: r.priority, IssueType: r.issueType, DependencyType: depType}
	}
	var dependencies, dependents []beads.RelatedIssue
	for _, d := range depsByFrom[id] {
		dependencies = append(dependencies, related(d.DependsOnId, d.Type))
	}
	for from, ds := range depsByFrom {
		for _, d := range ds {
			if d.DependsOnId == id {
				dependents = append(dependents, related(from, d.Type))
			}
		}
	}
	issue := beads.Issue{
		ID:                 self.id,
		Title:              self.title,
		Description:        self.description,
		AcceptanceCriteria: self.acceptance,
		Status:             self.status,
		Priority:           self.priority,
		IssueType:          self.issueType,
		Owner:              self.owner,
		Assignee:           self.assignee,
		Labels:             parseLabels(self.labels),
		Parent:             self.parent,
		Dependencies:       dependencies,
		Dependents:         dependents,
		CreatedAt:          self.createdAt.Format(time.RFC3339),
		CreatedBy:          self.createdBy,
		UpdatedAt:          self.updatedAt.Format(time.RFC3339),
		ExternalRef:        self.externalRef,
	}
	if self.closedAt.Valid {
		closedAt, err := time.Parse(timeLayout, self.closedAt.String)
		if err != nil {
			return beads.Issue{}, fmt.Errorf("taskstore: parse closed_at: %w", err)
		}
		issue.ClosedAt = closedAt.Format(time.RFC3339)
	}
	return issue, nil
}

// newID returns a short, collision-resistant task id (a "pkt-" prefix marks it
// as a Punakawan-managed task, distinct from bd's own "<prefix>-<n>" ids).
func newID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("taskstore: generate id: %w", err)
	}
	return "pkt-" + hex.EncodeToString(b[:]), nil
}
