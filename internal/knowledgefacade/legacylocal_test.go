package knowledgefacade

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newTestDB opens the shared SQLite kernel over a fresh temp database file,
// mirroring internal/knowledge and internal/search's own test helpers - no
// external dolt binary, no t.Skip.
func newTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newFixtureRecord(localID, title string) protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/" + localID,
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  title,
		Source: protocol.KnowledgeRecordSource{
			Provider:    "test",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodManual,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateObserved,
		},
	}
}

func newTestIndex(t *testing.T) *search.Index {
	t.Helper()
	ix, err := search.OpenIndex(filepath.Join(t.TempDir(), "bm25"))
	if err != nil {
		t.Fatalf("search.OpenIndex: %v", err)
	}
	t.Cleanup(func() {
		if err := ix.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return ix
}

func putAndIndex(t *testing.T, store *knowledge.Store, ix *search.Index, rec protocol.KnowledgeRecord) {
	t.Helper()
	if err := store.Put(rec); err != nil {
		t.Fatalf("Put %s: %v", rec.Id, err)
	}
	if err := ix.IndexRecord(knowledge.RecordWithUpdatedAt{Record: rec, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("IndexRecord %s: %v", rec.Id, err)
	}
}

func TestLegacyLocalProviderSearchRankedHitsOwnProject(t *testing.T) {
	db := newTestDB(t)
	store := knowledge.New(db, "home")
	ix := newTestIndex(t)

	rec := newFixtureRecord("REQ-1", "Refund an approved order")
	putAndIndex(t, store, ix, rec)

	p := &LegacyLocalKnowledgeProvider{
		Store:   store,
		Project: "home",
		RankedSearch: func(r search.Request) ([]search.Result, error) {
			return search.Search(store, ix, r)
		},
	}

	results, err := p.Search(context.Background(), SearchRequest{Query: "refund approved order"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want exactly one hit", results)
	}
	got := results[0]
	if got.Source != SourceLocal {
		t.Errorf("Source = %q, want %q", got.Source, SourceLocal)
	}
	if got.Ref != rec.Id {
		t.Errorf("Ref = %q, want %q", got.Ref, rec.Id)
	}
	if got.Match.Kind != string(search.MatchKindBM25) {
		t.Errorf("Match.Kind = %q, want %q", got.Match.Kind, search.MatchKindBM25)
	}
	if got.Record == nil || got.Record.Id != rec.Id {
		t.Errorf("Record = %+v, want a hydrated copy of %s", got.Record, rec.Id)
	}
}

func TestLegacyLocalProviderSearchCrossProjectFallsBackToScan(t *testing.T) {
	db := newTestDB(t)
	homeStore := knowledge.New(db, "home")
	otherStore := knowledge.New(db, "other")

	rec := newFixtureRecord("REQ-2", "unique-marker-osprey")
	if err := otherStore.Put(rec); err != nil {
		t.Fatalf("Put: %v", err)
	}

	p := &LegacyLocalKnowledgeProvider{Store: homeStore, Project: "home"}

	results, err := p.Search(context.Background(), SearchRequest{Query: "unique-marker-osprey", ProjectId: "other"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want exactly one cross-project hit", results)
	}
	got := results[0]
	if got.Ref != rec.Id {
		t.Errorf("Ref = %q, want %q", got.Ref, rec.Id)
	}
	if got.Match.Kind != "cross_project_scan" {
		t.Errorf("Match.Kind = %q, want cross_project_scan", got.Match.Kind)
	}
	if got.Record == nil || got.Record.Id != rec.Id {
		t.Errorf("Record = %+v, want a hydrated copy of %s", got.Record, rec.Id)
	}
}

func TestLegacyLocalProviderGetOwnAndSiblingProject(t *testing.T) {
	db := newTestDB(t)
	homeStore := knowledge.New(db, "home")
	otherStore := knowledge.New(db, "other")

	homeRec := newFixtureRecord("OWN-1", "own project record")
	otherRec := newFixtureRecord("OTHER-1", "sibling project record")
	if err := homeStore.Put(homeRec); err != nil {
		t.Fatalf("Put home: %v", err)
	}
	if err := otherStore.Put(otherRec); err != nil {
		t.Fatalf("Put other: %v", err)
	}

	p := &LegacyLocalKnowledgeProvider{Store: homeStore, Project: "home"}

	got, err := p.Get(context.Background(), homeRec.Id)
	if err != nil {
		t.Fatalf("Get own: %v", err)
	}
	if got.Id != homeRec.Id {
		t.Errorf("got.Id = %q, want %q", got.Id, homeRec.Id)
	}

	if _, err := p.Get(context.Background(), otherRec.Id); !errors.Is(err, knowledge.ErrNotFound) {
		t.Fatalf("Get sibling without naming it: err = %v, want ErrNotFound", err)
	}

	sibling := &LegacyLocalKnowledgeProvider{Store: homeStore, Project: "other"}
	got, err = sibling.Get(context.Background(), otherRec.Id)
	if err != nil {
		t.Fatalf("Get sibling explicitly: %v", err)
	}
	if got.Id != otherRec.Id {
		t.Errorf("got.Id = %q, want %q", got.Id, otherRec.Id)
	}
}

func TestLegacyLocalProviderRecordPersistsAndReturnsRef(t *testing.T) {
	db := newTestDB(t)
	store := knowledge.New(db, "home")
	p := &LegacyLocalKnowledgeProvider{Store: store, Project: "home"}

	candidate := newFixtureRecord("CAND-1", "a candidate learning")
	ref, err := p.Record(context.Background(), candidate)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if ref != candidate.Id {
		t.Fatalf("ref = %q, want %q", ref, candidate.Id)
	}

	got, err := store.Get(candidate.Id)
	if err != nil {
		t.Fatalf("Get after Record: %v", err)
	}
	if got.Title != candidate.Title {
		t.Errorf("got.Title = %q, want %q", got.Title, candidate.Title)
	}
}

func TestMomProviderStubsReturnNotConfigured(t *testing.T) {
	p := MomProvider{}
	ctx := context.Background()

	if _, err := p.Search(ctx, SearchRequest{Query: "anything"}); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("Search err = %v, want ErrProviderNotConfigured", err)
	}
	if _, err := p.Get(ctx, "some-ref"); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("Get err = %v, want ErrProviderNotConfigured", err)
	}
	if _, err := p.Record(ctx, newFixtureRecord("X", "x")); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("Record err = %v, want ErrProviderNotConfigured", err)
	}
}

func TestCodepediaProviderStubsReturnNotConfiguredAndIsNotASink(t *testing.T) {
	p := CodepediaProvider{}
	ctx := context.Background()

	if _, err := p.Search(ctx, SearchRequest{Query: "anything"}); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("Search err = %v, want ErrProviderNotConfigured", err)
	}
	if _, err := p.Get(ctx, "some-ref"); !errors.Is(err, ErrProviderNotConfigured) {
		t.Errorf("Get err = %v, want ErrProviderNotConfigured", err)
	}
	if _, ok := any(p).(KnowledgeSink); ok {
		t.Error("CodepediaProvider must not implement KnowledgeSink - it is read-only by design")
	}
}
