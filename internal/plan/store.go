package plan

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

	"github.com/oklog/ulid/v2"

	"github.com/ygrip/punakawan/internal/storage"
)

// ErrNotFound is returned by Get/GetRevision when no plan (or no such
// revision) exists.
var ErrNotFound = errors.New("plan: not found")

// Store is the Plan aggregate's persistence boundary, over the shared
// SQLite storage kernel. It is not scoped to a single project, mirroring
// internal/delivery.Store: a Plan can name several ProjectIDs, so there
// is no single project id to partition rows by.
type Store struct {
	db *storage.DB
}

// NewStore wraps an opened storage kernel database.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// NewID mints a filesystem-safe, lexicographically sortable ULID for a
// new plan lineage, matching delivery.NewID's convention.
func NewID() string { return ulid.Make().String() }

// Save appends a new revision to p.ID's lineage, minting a fresh
// idempotency key on every call: Save is a compatibility wrapper over
// SaveWithKey for a caller with no idempotency key of its own, matching
// its previous never-deduplicated behavior exactly.
func (s *Store) Save(ctx context.Context, p Plan) (Plan, error) {
	key, err := writeKey()
	if err != nil {
		return Plan{}, err
	}
	return s.SaveWithKey(ctx, key, p)
}

// SaveWithKey appends a new revision to p.ID's lineage: Revision and
// PreviousRevision are always computed here and overwrite whatever p
// carried in, so a lineage's revisions stay strictly sequential and
// never mutate an existing (plan_id, revision) row. Reuse an existing
// plan's ID to add a revision on top of it, or a fresh ID (NewID) to
// start a new lineage. Repeating the same idempotencyKey resolves to the
// lineage's current head instead of appending a second, identical
// revision - the property internal/deliveryservice's reconcile.go relies
// on to make repeated reconciliation idempotent.
func (s *Store) SaveWithKey(ctx context.Context, idempotencyKey string, p Plan) (Plan, error) {
	if strings.TrimSpace(p.ID) == "" {
		return Plan{}, fmt.Errorf("plan: save: id is required")
	}
	if strings.TrimSpace(p.Objective) == "" {
		return Plan{}, fmt.Errorf("plan: save %s: objective is required", p.ID)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	for i := range p.Steps {
		if strings.TrimSpace(p.Steps[i].ID) != "" {
			continue
		}
		id, err := newStepID()
		if err != nil {
			return Plan{}, fmt.Errorf("plan: save %s: %w", p.ID, err)
		}
		p.Steps[i].ID = id
	}

	err := s.db.Write(ctx, idempotencyKey, "save plan "+p.ID, func(tx *sql.Tx) error {
		var maxRevision sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(revision) FROM plan_revisions WHERE plan_id = ?`, p.ID).Scan(&maxRevision); err != nil {
			return fmt.Errorf("plan: save %s: read current revision: %w", p.ID, err)
		}
		if maxRevision.Valid {
			prev := int(maxRevision.Int64)
			p.Revision = prev + 1
			p.PreviousRevision = &prev
		} else {
			p.Revision = 1
			p.PreviousRevision = nil
		}

		data, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("plan: save %s: encode: %w", p.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO plan_revisions (plan_id, revision, data, created_at) VALUES (?, ?, ?, ?)`,
			p.ID, p.Revision, string(data), p.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("plan: save %s: insert revision %d: %w", p.ID, p.Revision, err)
		}
		return nil
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		// A repeated idempotencyKey means this exact call already
		// happened - fall back to reading back the lineage's actual
		// current head rather than trusting the p this call computed,
		// since this call's own write did not happen.
		return s.Get(ctx, p.ID)
	}
	if err != nil {
		return Plan{}, err
	}
	return p, nil
}

// ExistsRevision reports whether exactly (id, revision) has been saved.
func (s *Store) ExistsRevision(ctx context.Context, id string, revision int) (bool, error) {
	var exists int
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT 1 FROM plan_revisions WHERE plan_id = ? AND revision = ?`, id, revision,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("plan: exists revision %s@%d: %w", id, revision, err)
	}
	return true, nil
}

// Get returns id's current (highest-revision) Plan.
func (s *Store) Get(ctx context.Context, id string) (Plan, error) {
	var data string
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT data FROM plan_revisions WHERE plan_id = ? ORDER BY revision DESC LIMIT 1`, id,
	).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("plan: get %s: %w", id, err)
	}
	return decodePlan(id, data)
}

// GetRevision returns exactly revision of id's lineage.
func (s *Store) GetRevision(ctx context.Context, id string, revision int) (Plan, error) {
	var data string
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT data FROM plan_revisions WHERE plan_id = ? AND revision = ?`, id, revision,
	).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, fmt.Errorf("plan: get %s revision %d: %w", id, revision, err)
	}
	return decodePlan(id, data)
}

// List returns the current revision of every plan lineage, ordered by
// id.
func (s *Store) List(ctx context.Context) ([]Plan, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
SELECT p.data FROM plan_revisions p
INNER JOIN (SELECT plan_id, MAX(revision) AS revision FROM plan_revisions GROUP BY plan_id) latest
  ON latest.plan_id = p.plan_id AND latest.revision = p.revision
ORDER BY p.plan_id`)
	if err != nil {
		return nil, fmt.Errorf("plan: list: %w", err)
	}
	defer rows.Close()

	var out []Plan
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("plan: list: scan: %w", err)
		}
		p, err := decodePlan("", data)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plan: list: %w", err)
	}
	return out, nil
}

// ListByProject returns the current revision of every plan lineage that
// names projectID among its ProjectIDs, matching how
// internal/artifact.PlanStore.ListPlans scopes listing to one owner -
// here a plan's owners are its ProjectIDs field rather than a workspace
// root, so filtering happens after decode rather than via a directory
// boundary.
func (s *Store) ListByProject(ctx context.Context, projectID string) ([]Plan, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Plan, 0, len(all))
	for _, p := range all {
		for _, id := range p.ProjectIDs {
			if id == projectID {
				out = append(out, p)
				break
			}
		}
	}
	return out, nil
}

// LinkedPlan is one exact plan revision a delivery links, at whichever
// scope it was linked under: "delivery" for the cross-project high-level
// plan, "project" for one project's own detailed plan.
type LinkedPlan struct {
	ProjectID    string    `json:"project_id,omitempty"`
	Scope        string    `json:"scope"`
	Plan         Plan      `json:"plan"`
	LinkedAt     time.Time `json:"linked_at"`
	HeadRevision int       `json:"head_revision"`
}

// DeliveryPlanRef is one delivery that links a plan revision belonging to
// a given project, the reverse of LinkedPlan's delivery -> plan
// direction.
type DeliveryPlanRef struct {
	OrchestrationID string    `json:"orchestration_id"`
	Scope           string    `json:"scope"`
	PlanID          string    `json:"plan_id"`
	PlanRevision    int       `json:"plan_revision"`
	LinkedAt        time.Time `json:"linked_at"`
}

// ListByDelivery returns every plan revision orchestrationID links -
// its own high-level plan (scope "delivery") plus one entry per
// project-scoped detailed plan (scope "project") - each carrying the
// exact linked Plan content alongside its lineage's current head
// revision, so a caller can tell "this delivery still points at the
// head" from "the plan moved on since this delivery linked it".
//
// This reaches across into internal/delivery's own delivery_plan_links
// table by direct SQL rather than importing that package: both packages
// share the one SQLite kernel (internal/app.OpenStorage), and importing
// internal/delivery here would create the same cyclic dependency
// internal/delivery.Store.LinkProjectPlan already avoids on plan.Store by
// validating plan_revisions the same way, in the other direction.
func (s *Store) ListByDelivery(ctx context.Context, orchestrationID string) ([]LinkedPlan, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
SELECT link.project_id, link.scope, link.plan_id, link.plan_revision, link.created_at
FROM delivery_plan_links link
WHERE link.orchestration_id = ?
ORDER BY link.created_at, link.scope, link.project_id`, orchestrationID)
	if err != nil {
		return nil, fmt.Errorf("plan: list by delivery %s: %w", orchestrationID, err)
	}
	defer rows.Close()

	type row struct {
		projectID string
		scope     string
		planID    string
		revision  int
		createdAt string
	}
	var linkRows []row
	for rows.Next() {
		var r row
		var projectID sql.NullString
		if err := rows.Scan(&projectID, &r.scope, &r.planID, &r.revision, &r.createdAt); err != nil {
			return nil, fmt.Errorf("plan: list by delivery %s: scan: %w", orchestrationID, err)
		}
		r.projectID = projectID.String
		linkRows = append(linkRows, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plan: list by delivery %s: %w", orchestrationID, err)
	}

	out := make([]LinkedPlan, 0, len(linkRows))
	for _, r := range linkRows {
		p, err := s.GetRevision(ctx, r.planID, r.revision)
		if err != nil {
			return nil, fmt.Errorf("plan: list by delivery %s: read %s@%d: %w", orchestrationID, r.planID, r.revision, err)
		}
		head, err := s.Get(ctx, r.planID)
		if err != nil {
			return nil, fmt.Errorf("plan: list by delivery %s: read head of %s: %w", orchestrationID, r.planID, err)
		}
		linkedAt, err := time.Parse(time.RFC3339Nano, r.createdAt)
		if err != nil {
			return nil, fmt.Errorf("plan: list by delivery %s: parse linked_at: %w", orchestrationID, err)
		}
		out = append(out, LinkedPlan{
			ProjectID: r.projectID, Scope: r.scope, Plan: p,
			LinkedAt: linkedAt, HeadRevision: head.Revision,
		})
	}
	return out, nil
}

// ListDeliveriesByProject returns every delivery that has ever linked a
// plan revision to projectID, project-scoped links only (a
// cross-project, delivery-scoped high-level plan names no single
// project). This is ListByDelivery's reverse: "which deliveries used
// this project's plans" rather than "which plans does this delivery
// use".
func (s *Store) ListDeliveriesByProject(ctx context.Context, projectID string) ([]DeliveryPlanRef, error) {
	rows, err := s.db.Reader().QueryContext(ctx, `
SELECT orchestration_id, scope, plan_id, plan_revision, created_at
FROM delivery_plan_links
WHERE project_id = ?
ORDER BY created_at, orchestration_id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("plan: list deliveries by project %s: %w", projectID, err)
	}
	defer rows.Close()

	out := []DeliveryPlanRef{}
	for rows.Next() {
		var ref DeliveryPlanRef
		var createdAt string
		if err := rows.Scan(&ref.OrchestrationID, &ref.Scope, &ref.PlanID, &ref.PlanRevision, &createdAt); err != nil {
			return nil, fmt.Errorf("plan: list deliveries by project %s: scan: %w", projectID, err)
		}
		linkedAt, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("plan: list deliveries by project %s: parse linked_at: %w", projectID, err)
		}
		ref.LinkedAt = linkedAt
		out = append(out, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("plan: list deliveries by project %s: %w", projectID, err)
	}
	return out, nil
}

func decodePlan(id, data string) (Plan, error) {
	var p Plan
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		if id != "" {
			return Plan{}, fmt.Errorf("plan: decode %s: %w", id, err)
		}
		return Plan{}, fmt.Errorf("plan: decode: %w", err)
	}
	return p, nil
}

// writeKey mints a fresh idempotency key for a Save call, mirroring
// internal/knowledge's writeKey: Save has no caller-supplied
// idempotency key of its own (unlike internal/delivery's event log),
// so each call gets its own key and is never treated as a replay.
func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("plan: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// newStepID mints a short, collision-resistant id for a PlanStep that
// arrived at Save with none set, mirroring taskstore.newID's "short
// random hex" convention (a full ULID would be needlessly long for
// something scoped to one plan's own step list).
func newStepID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("plan: generate step id: %w", err)
	}
	return "step-" + hex.EncodeToString(b[:]), nil
}
