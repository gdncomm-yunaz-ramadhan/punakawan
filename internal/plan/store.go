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

// Save appends a new revision to p.ID's lineage: Revision and
// PreviousRevision are always computed here and overwrite whatever p
// carried in, so a lineage's revisions stay strictly sequential and
// never mutate an existing (id, revision) row. Reuse an existing plan's
// ID to add a revision on top of it, or a fresh ID (NewID) to start a
// new lineage.
func (s *Store) Save(ctx context.Context, p Plan) (Plan, error) {
	if strings.TrimSpace(p.ID) == "" {
		return Plan{}, fmt.Errorf("plan: save: id is required")
	}
	if strings.TrimSpace(p.Objective) == "" {
		return Plan{}, fmt.Errorf("plan: save %s: objective is required", p.ID)
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}

	key, err := writeKey()
	if err != nil {
		return Plan{}, err
	}

	err = s.db.Write(ctx, key, "save plan "+p.ID, func(tx *sql.Tx) error {
		var maxRevision sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(revision) FROM plans WHERE id = ?`, p.ID).Scan(&maxRevision); err != nil {
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
			`INSERT INTO plans (id, revision, data, created_at) VALUES (?, ?, ?, ?)`,
			p.ID, p.Revision, string(data), p.CreatedAt.Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("plan: save %s: insert revision %d: %w", p.ID, p.Revision, err)
		}
		return nil
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		// writeKey is fresh random bytes every call, so a genuine replay
		// here would be surprising - fall back to reading back the
		// lineage's actual current head rather than trusting the p this
		// call computed, since this call's own write did not happen.
		return s.Get(ctx, p.ID)
	}
	if err != nil {
		return Plan{}, err
	}
	return p, nil
}

// Get returns id's current (highest-revision) Plan.
func (s *Store) Get(ctx context.Context, id string) (Plan, error) {
	var data string
	err := s.db.Reader().QueryRowContext(ctx,
		`SELECT data FROM plans WHERE id = ? ORDER BY revision DESC LIMIT 1`, id,
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
		`SELECT data FROM plans WHERE id = ? AND revision = ?`, id, revision,
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
SELECT p.data FROM plans p
INNER JOIN (SELECT id, MAX(revision) AS revision FROM plans GROUP BY id) latest
  ON latest.id = p.id AND latest.revision = p.revision
ORDER BY p.id`)
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
