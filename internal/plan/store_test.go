package plan

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/storage"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "plan.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func TestSaveAssignsSequentialRevisions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	id := NewID()

	first, err := store.Save(ctx, Plan{ID: id, Objective: "first cut"})
	if err != nil {
		t.Fatalf("Save (revision 1): %v", err)
	}
	if first.Revision != 1 {
		t.Fatalf("first.Revision = %d, want 1", first.Revision)
	}
	if first.PreviousRevision != nil {
		t.Fatalf("first.PreviousRevision = %v, want nil", first.PreviousRevision)
	}

	second, err := store.Save(ctx, Plan{ID: id, Objective: "clarified", ReasonForChange: "clarification"})
	if err != nil {
		t.Fatalf("Save (revision 2): %v", err)
	}
	if second.Revision != 2 {
		t.Fatalf("second.Revision = %d, want 2", second.Revision)
	}
	if second.PreviousRevision == nil || *second.PreviousRevision != 1 {
		t.Fatalf("second.PreviousRevision = %v, want pointer to 1", second.PreviousRevision)
	}

	// The first revision's row is never mutated: reading it back by
	// exact revision must still show its own original content, not the
	// second revision's.
	gotFirst, err := store.GetRevision(ctx, id, 1)
	if err != nil {
		t.Fatalf("GetRevision(1): %v", err)
	}
	if gotFirst.Objective != "first cut" {
		t.Fatalf("GetRevision(1).Objective = %q, want unchanged %q", gotFirst.Objective, "first cut")
	}

	latest, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if latest.Revision != 2 || latest.Objective != "clarified" {
		t.Fatalf("Get = %+v, want revision 2 (clarified)", latest)
	}
}

func TestSaveRequiresIDAndObjective(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.Save(ctx, Plan{Objective: "no id"}); err == nil {
		t.Fatalf("Save with empty id: want error")
	}
	if _, err := store.Save(ctx, Plan{ID: NewID()}); err == nil {
		t.Fatalf("Save with empty objective: want error")
	}
}

func TestGetUnknownPlanFails(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, err := store.Get(ctx, "no-such-plan"); err != ErrNotFound {
		t.Fatalf("Get unknown: err = %v, want ErrNotFound", err)
	}
	if _, err := store.GetRevision(ctx, "no-such-plan", 1); err != ErrNotFound {
		t.Fatalf("GetRevision unknown: err = %v, want ErrNotFound", err)
	}
}

func TestListAndListByProject(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	a, err := store.Save(ctx, Plan{ID: NewID(), Objective: "plan a", ProjectIDs: []string{"proj-1"}})
	if err != nil {
		t.Fatalf("Save a: %v", err)
	}
	b, err := store.Save(ctx, Plan{ID: NewID(), Objective: "plan b", ProjectIDs: []string{"proj-2"}})
	if err != nil {
		t.Fatalf("Save b: %v", err)
	}
	// A second revision of a must not make List return two rows for it.
	if _, err := store.Save(ctx, Plan{ID: a.ID, Objective: "plan a, revised", ProjectIDs: []string{"proj-1"}}); err != nil {
		t.Fatalf("Save a revision 2: %v", err)
	}

	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List returned %d plans, want 2 (one current revision per lineage)", len(all))
	}

	onlyA, err := store.ListByProject(ctx, "proj-1")
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(onlyA) != 1 || onlyA[0].ID != a.ID || onlyA[0].Objective != "plan a, revised" {
		t.Fatalf("ListByProject(proj-1) = %+v, want a's current revision only", onlyA)
	}

	onlyB, err := store.ListByProject(ctx, "proj-2")
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(onlyB) != 1 || onlyB[0].ID != b.ID {
		t.Fatalf("ListByProject(proj-2) = %+v, want b only", onlyB)
	}
}
