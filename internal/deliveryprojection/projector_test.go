package deliveryprojection

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/telemetry"
)

func newTestProjector(t *testing.T) (*Projector, *delivery.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "projection.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store := delivery.NewStore(db)
	return NewProjector(store), store
}

// seededProjector returns a Projector over one seeded ad-hoc delivery
// with a fixed, known id ("delivery-1"), plus one telemetry session
// already begun against it so mustSnapshotUsage's caller has something
// to ingest a snapshot for.
func seededProjector(t *testing.T) *Projector {
	t.Helper()
	p, store := newTestProjector(t)
	ctx := context.Background()
	if _, err := store.CreateOrchestration(ctx, "seed-delivery-1", "delivery-1", nil); err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	if _, err := p.telemetry.Begin(ctx, telemetry.BeginRequest{
		DeliveryID: "delivery-1", ClientKind: "claude-code", ExternalSessionID: "delivery-1-session",
	}); err != nil {
		t.Fatalf("telemetry.Begin: %v", err)
	}
	return p
}

func mustList(t *testing.T, p *Projector) []DeliverySummary {
	t.Helper()
	list, err := p.ListSummaries(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	return list
}

func mustDetail(t *testing.T, p *Projector, id string) *DeliveryDetail {
	t.Helper()
	detail, err := p.GetDetail(context.Background(), id)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	return detail
}

// mustSnapshotUsage ingests one usage snapshot against the telemetry
// session seededProjector already began for id, advancing
// delivery_projection_versions the same way a real usage-reporting call
// would.
func mustSnapshotUsage(t *testing.T, p *Projector, id string) {
	t.Helper()
	ctx := context.Background()
	session, err := p.telemetry.GetSessionByExternalID(ctx, "claude-code", id+"-session")
	if err != nil {
		t.Fatalf("GetSessionByExternalID: %v", err)
	}
	if _, err := p.telemetry.IngestSnapshot(ctx, telemetry.SnapshotRequest{
		SessionID: session.ID, SourceID: "main", Sequence: 1, InputTokens: 5,
	}); err != nil {
		t.Fatalf("IngestSnapshot: %v", err)
	}
}

// queryCounter counts every QueryContext/QueryRowContext call a
// countingReader forwards, so a test can assert ListSummaries makes a
// bounded number of round trips no matter how many deliveries exist.
type queryCounter struct{ n int }

func (c *queryCounter) Count() int { return c.n }

type countingReader struct {
	inner reader
	c     *queryCounter
}

func (r *countingReader) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.c.n++
	return r.inner.QueryContext(ctx, query, args...)
}

func (r *countingReader) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	r.c.n++
	return r.inner.QueryRowContext(ctx, query, args...)
}

// instrumentedProjector seeds n bare ad-hoc deliveries and wraps the
// Projector's own reader in a countingReader, so a test can call
// ListSummaries and then assert on how many queries it actually issued.
func instrumentedProjector(t *testing.T, n int) (*Projector, *queryCounter) {
	t.Helper()
	p, store := newTestProjector(t)
	ctx := context.Background()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("delivery-%03d", i)
		if _, err := store.CreateOrchestration(ctx, "seed-"+id, id, nil); err != nil {
			t.Fatalf("CreateOrchestration(%s): %v", id, err)
		}
	}
	counter := &queryCounter{}
	p.read = &countingReader{inner: p.read, c: counter}
	return p, counter
}

// TestListAndDetailShareProjectionRevision proves the list and detail
// projections never disagree about how fresh a delivery's data is: both
// must report the exact same delivery_projection_versions revision for
// the same delivery.
func TestListAndDetailShareProjectionRevision(t *testing.T) {
	p := seededProjector(t)
	list := mustList(t, p)
	if len(list) != 1 {
		t.Fatalf("list = %+v, want exactly the one seeded delivery", list)
	}
	detail := mustDetail(t, p, list[0].ID)
	if list[0].ProjectionRevision != detail.ProjectionRevision {
		t.Fatalf("list revision %d != detail revision %d", list[0].ProjectionRevision, detail.ProjectionRevision)
	}
}

// TestListUsesBoundedQueries proves ListSummaries' cost does not grow
// with the number of deliveries: it must always issue the same small,
// fixed number of batch queries, never one (or more) per delivery.
func TestListUsesBoundedQueries(t *testing.T) {
	p, queries := instrumentedProjector(t, 100)
	list, err := p.ListSummaries(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(list) != 100 {
		t.Fatalf("len(list) = %d, want 100", len(list))
	}
	if got := queries.Count(); got > 8 {
		t.Fatalf("ListSummaries issued %d queries for 100 deliveries, want at most 8", got)
	}
}

// TestUsageMutationAdvancesProjection proves a mid-session usage
// snapshot - not just a session's final close - advances the delivery's
// projection revision, so a panel watching for updates actually observes
// tokens/cost changing as a session runs rather than only at the end.
func TestUsageMutationAdvancesProjection(t *testing.T) {
	p := seededProjector(t)
	before := mustDetail(t, p, "delivery-1").ProjectionRevision
	mustSnapshotUsage(t, p, "delivery-1")
	after := mustDetail(t, p, "delivery-1").ProjectionRevision
	if after <= before {
		t.Fatalf("ProjectionRevision after usage snapshot = %d, want greater than before (%d)", after, before)
	}
}
