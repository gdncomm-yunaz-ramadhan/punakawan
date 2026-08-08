package delivery

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	// t.TempDir() lives outside any Git checkout — this exercises
	// acceptance criterion 1's "a server outside Git" requirement
	// without needing an actual second repository on disk.
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

// disableProjectForTest bypasses the public API: project lifecycle
// management (activate/disable) is punokawan-14yn.4's scope, not this
// task's. It exists only to exercise CreateLane's ErrProjectInactive
// path ahead of that task landing.
func (s *Store) disableProjectForTest(ctx context.Context, id string) error {
	return s.db.Write(ctx, "disable-"+id, "disable project "+id, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE delivery_projects SET status = 'disabled' WHERE id = ?`, id)
		return err
	})
}

func registerProject(t *testing.T, s *Store, slug string) *protocol.DeliveryProject {
	t.Helper()
	id := NewID()
	p, err := s.RegisterProject(context.Background(), "reg-"+slug, id, slug, "https://example.test/"+slug+".git", "main")
	if err != nil {
		t.Fatalf("RegisterProject(%s): %v", slug, err)
	}
	return p
}

func TestCreatePersistReloadCancelOrchestration(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := NewID()
	note := "needs-triage"

	created, err := s.CreateOrchestration(ctx, "create-"+id, id, []protocol.DeliveryOrchestrationUnresolvedInputsElem{
		{Reference: "JIRA-123", Note: &note},
	})
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if created.Status != protocol.DeliveryOrchestrationStatusPending {
		t.Fatalf("status = %s, want pending", created.Status)
	}
	if len(created.UnresolvedInputs) != 1 || created.UnresolvedInputs[0].Reference != "JIRA-123" {
		t.Fatalf("unresolved_inputs = %+v, want one JIRA-123 entry", created.UnresolvedInputs)
	}

	reloaded, err := s.GetOrchestration(ctx, id)
	if err != nil {
		t.Fatalf("GetOrchestration (reload): %v", err)
	}
	if reloaded.Revision != created.Revision || reloaded.Status != created.Status {
		t.Fatalf("reloaded = %+v, want match with created %+v", reloaded, created)
	}

	cancelled, err := s.CancelOrchestration(ctx, "cancel-"+id, id, reloaded.Revision)
	if err != nil {
		t.Fatalf("CancelOrchestration: %v", err)
	}
	if cancelled.Status != protocol.DeliveryOrchestrationStatusCancelled {
		t.Fatalf("status after cancel = %s, want cancelled", cancelled.Status)
	}

	if _, err := s.CancelOrchestration(ctx, "cancel-again-"+id, id, cancelled.Revision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cancelling an already-cancelled orchestration = %v, want ErrInvalidState", err)
	}
}

func TestTwoOrchestrationsSeparateLanesNoLostUpdatesNoCrossRead(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	projA := registerProject(t, s, "proj-a")
	projB := registerProject(t, s, "proj-b")

	orchA, err := s.CreateOrchestration(ctx, "orchA", NewID(), nil)
	if err != nil {
		t.Fatalf("create orchA: %v", err)
	}
	orchB, err := s.CreateOrchestration(ctx, "orchB", NewID(), nil)
	if err != nil {
		t.Fatalf("create orchB: %v", err)
	}

	var wg sync.WaitGroup
	laneAID, laneBID := NewID(), NewID()
	var errA, errB error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errA = s.CreateLane(ctx, "laneA", laneAID, orchA.Id, projA.Id, "")
	}()
	go func() {
		defer wg.Done()
		_, errB = s.CreateLane(ctx, "laneB", laneBID, orchB.Id, projB.Id, "")
	}()
	wg.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("concurrent CreateLane errors: A=%v B=%v", errA, errB)
	}

	laneA, err := s.GetLane(ctx, orchA.Id, laneAID)
	if err != nil {
		t.Fatalf("GetLane A: %v", err)
	}
	if laneA.ProjectId != projA.Id {
		t.Fatalf("laneA.ProjectId = %s, want %s", laneA.ProjectId, projA.Id)
	}

	laneB, err := s.GetLane(ctx, orchB.Id, laneBID)
	if err != nil {
		t.Fatalf("GetLane B: %v", err)
	}
	if laneB.ProjectId != projB.Id {
		t.Fatalf("laneB.ProjectId = %s, want %s", laneB.ProjectId, projB.Id)
	}

	// Cross-project / cross-orchestration read must fail closed, not
	// silently return the other orchestration's lane.
	if _, err := s.GetLane(ctx, orchA.Id, laneBID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetLane(orchA, laneB) = %v, want ErrNotFound", err)
	}
}

func TestUnknownAndMismatchedIdentifiersFailClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.GetProject(ctx, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProject unknown = %v, want ErrNotFound", err)
	}
	if _, err := s.GetOrchestration(ctx, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOrchestration unknown = %v, want ErrNotFound", err)
	}

	orch, err := s.CreateOrchestration(ctx, "orch-mismatch", NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if _, err := s.CreateLane(ctx, "lane-unknown-project", NewID(), orch.Id, "does-not-exist", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateLane with unknown project = %v, want ErrNotFound", err)
	}

	disabled := registerProject(t, s, "disabled-proj")
	// Disable directly at the storage layer; Store has no public
	// disable method yet (project lifecycle is punokawan-14yn.4's scope).
	if err := s.disableProjectForTest(ctx, disabled.Id); err != nil {
		t.Fatalf("disable project: %v", err)
	}
	if _, err := s.CreateLane(ctx, "lane-inactive-project", NewID(), orch.Id, disabled.Id, ""); !errors.Is(err, ErrProjectInactive) {
		t.Fatalf("CreateLane against disabled project = %v, want ErrProjectInactive", err)
	}

	if _, err := s.GetLane(ctx, orch.Id, "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetLane unknown = %v, want ErrNotFound", err)
	}
}

func TestEventReplayDeterministicAndDuplicateIdempotencyKeyHarmless(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := NewID()

	first, err := s.CreateOrchestration(ctx, "dup-key", id, []protocol.DeliveryOrchestrationUnresolvedInputsElem{{Reference: "R1"}})
	if err != nil {
		t.Fatalf("first CreateOrchestration: %v", err)
	}
	second, err := s.CreateOrchestration(ctx, "dup-key", id, []protocol.DeliveryOrchestrationUnresolvedInputsElem{{Reference: "R1"}})
	if err != nil {
		t.Fatalf("duplicate CreateOrchestration must be harmless, got: %v", err)
	}
	if first.Revision != second.Revision || len(second.UnresolvedInputs) != 1 {
		t.Fatalf("duplicate create changed state: first=%+v second=%+v", first, second)
	}

	// Replaying the same event log twice must derive identical state.
	events, err := loadEvents(ctx, s.db.Reader(), id)
	if err != nil {
		t.Fatalf("loadEvents: %v", err)
	}
	r1, err := reduceOrchestration(id, events)
	if err != nil {
		t.Fatalf("reduceOrchestration (1st replay): %v", err)
	}
	r2, err := reduceOrchestration(id, events)
	if err != nil {
		t.Fatalf("reduceOrchestration (2nd replay): %v", err)
	}
	if r1.Revision != r2.Revision || r1.Status != r2.Status || len(r1.UnresolvedInputs) != len(r2.UnresolvedInputs) {
		t.Fatalf("non-deterministic replay: r1=%+v r2=%+v", r1, r2)
	}
}

func TestRevisionConflictRejectsStaleWrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id := NewID()
	orch, err := s.CreateOrchestration(ctx, "rc-create", id, nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if _, err := s.CancelOrchestration(ctx, "rc-cancel", id, orch.Revision+1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("CancelOrchestration with stale revision = %v, want ErrRevisionConflict", err)
	}
}
