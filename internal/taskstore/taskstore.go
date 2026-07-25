// Package taskstore is Punakawan's Beads-less fallback task graph: a
// Dolt-backed store for projects that have no .beads directory. When a project
// does not use Beads, the panel's task board and submit_task_graph persist to
// and read from here instead, so tasks and their dependency graph are still
// tracked - without mutating the project (no git init, no CLAUDE.md, no hooks
// that `bd init` would write).
//
// It deliberately reuses Punakawan's existing internal Dolt engine: rather
// than clone knowledge's careful sql-server lifecycle (serverRegistry,
// cross-process reuse, refcounted Close), a Store wraps a *sql.DB handed to it
// by the already-open knowledge store (see app.OpenTaskStore). The task tables
// simply live in the same per-project Punakawan Dolt database as knowledge;
// that database is an internal implementation detail, not a human repository.
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
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/beads"
)

// closedStatuses are the task statuses that count as "not blocking" when
// deciding readiness, mirroring how bd treats a closed dependency as
// satisfied. Everything else is treated as still-open.
var closedStatuses = map[string]bool{"closed": true, "done": true, "resolved": true, "completed": true}

// blockingDepTypes are the dependency types whose unresolved target blocks a
// task from being ready, matching bd's own vocabulary (a task "depends on" /
// is "blocked by" its target). parent-child is structural, not blocking.
var blockingDepTypes = map[string]bool{"blocks": true, "blocked-by": true, "requires": true}

// Store is a Dolt-backed fallback task store over an injected *sql.DB. It does
// not own the connection or the underlying dolt sql-server (the knowledge
// store does); it only owns its two tables.
type Store struct {
	db *sql.DB
}

// New wraps db in a Store. Callers must call Migrate once before use (App does
// this in OpenTaskStore).
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Migrate creates the task tables if they do not already exist. Idempotent, so
// it is safe to call on every open (the tables share the knowledge database,
// whose schema_migrations tracking is independent).
func (s *Store) Migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
  id VARCHAR(255) PRIMARY KEY,
  title TEXT NOT NULL,
  description TEXT,
  acceptance_criteria TEXT,
  status VARCHAR(64) NOT NULL,
  priority INT NOT NULL,
  issue_type VARCHAR(64) NOT NULL,
  owner VARCHAR(255),
  assignee VARCHAR(255),
  labels JSON,
  parent VARCHAR(255),
  external_ref VARCHAR(255),
  created_at DATETIME NOT NULL,
  created_by VARCHAR(255),
  updated_at DATETIME NOT NULL,
  closed_at DATETIME NULL
)`); err != nil {
		return fmt.Errorf("taskstore: create tasks table: %w", err)
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS task_deps (
  from_id VARCHAR(255) NOT NULL,
  to_id VARCHAR(255) NOT NULL,
  type VARCHAR(64) NOT NULL,
  PRIMARY KEY (from_id, to_id, type)
)`); err != nil {
		return fmt.Errorf("taskstore: create task_deps table: %w", err)
	}
	return nil
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
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO tasks (id, title, description, acceptance_criteria, status, priority, issue_type, owner, assignee, labels, parent, external_ref, created_at, created_by, updated_at, closed_at)
VALUES (?, ?, ?, ?, 'open', ?, ?, '', '', ?, ?, ?, ?, 'punakawan', ?, NULL)`,
		id, in.Title, in.Description, strings.Join(in.AcceptanceCriteria, "\n"), in.Priority, issueType,
		string(labels), in.Parent, in.ExternalRef, now, now); err != nil {
		return "", fmt.Errorf("taskstore: insert task: %w", err)
	}
	if in.Parent != "" {
		// Record the hierarchy as a parent-child edge so the dependency graph
		// mirrors what bd emits.
		if err := s.AddDependency(ctx, id, in.Parent, "parent-child"); err != nil {
			return "", err
		}
	}
	return id, nil
}

// AddDependency records that fromID depends on (is blocked by) toID, matching
// beads.AddDependency's direction. Idempotent on the (from, to, type) key.
func (s *Store) AddDependency(ctx context.Context, fromID, toID, depType string) error {
	if depType == "" {
		depType = "blocks"
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO task_deps (from_id, to_id, type) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE type = VALUES(type)`, fromID, toID, depType); err != nil {
		return fmt.Errorf("taskstore: add dependency %s -> %s: %w", fromID, toID, err)
	}
	return nil
}

// row is the scanned shape of a tasks row.
type row struct {
	id, title, description, acceptance, status, issueType, owner, assignee, labels, parent, externalRef, createdBy string
	priority                                                                                                       int
	createdAt, updatedAt                                                                                           time.Time
	closedAt                                                                                                       sql.NullTime
}

func (s *Store) allRows(ctx context.Context) ([]row, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, description, acceptance_criteria, status, priority, issue_type, owner, assignee, labels, parent, external_ref, created_at, created_by, updated_at, closed_at
FROM tasks ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("taskstore: query tasks: %w", err)
	}
	defer rows.Close()
	var out []row
	for rows.Next() {
		var r row
		var labels sql.NullString
		if err := rows.Scan(&r.id, &r.title, &r.description, &r.acceptance, &r.status, &r.priority, &r.issueType,
			&r.owner, &r.assignee, &labels, &r.parent, &r.externalRef, &r.createdAt, &r.createdBy, &r.updatedAt, &r.closedAt); err != nil {
			return nil, fmt.Errorf("taskstore: scan task: %w", err)
		}
		r.labels = labels.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// deps returns every dependency edge, grouped by from_id.
func (s *Store) allDeps(ctx context.Context) (map[string][]beads.ReadyDependency, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_id, to_id, type FROM task_deps`)
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
		issue.ClosedAt = self.closedAt.Time.Format(time.RFC3339)
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
