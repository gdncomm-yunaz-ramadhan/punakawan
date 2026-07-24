package search

import (
	"fmt"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/ygrip/punakawan/internal/knowledge"
)

// FieldWeights are §11.5's BM25F field weights, applied as per-field query
// boosts (Bleve has no static per-field weight setting on the mapping
// itself - the weighting happens at query time, in buildBM25FQuery).
var FieldWeights = map[string]float64{
	"identifiers": 12.0,
	"aliases":     10.0,
	"symbols":     8.0,
	"title":       7.0,
	"paths":       6.0,
	"tags":        4.0,
	"summary":     3.0,
	"content":     1.0,
}

// buildIndexMapping implements §11.4's document model as a Bleve mapping:
// free-text fields (title/summary/content/aliases/tags/paths/symbols/
// identifiers) use the technical analyzer from analyzer.go so BM25 terms
// match §11.6's tokenization; scope and type fields use Bleve's built-in
// keyword analyzer so "acme-widgets" is matched as one exact token, not
// decomposed.
func buildIndexMapping() mapping.IndexMapping {
	text := bleve.NewTextFieldMapping()
	text.Analyzer = AnalyzerName

	kw := bleve.NewTextFieldMapping()
	kw.Analyzer = keyword.Name

	date := bleve.NewDateTimeFieldMapping()

	doc := bleve.NewDocumentMapping()
	for _, field := range []string{"title", "summary", "content", "aliases", "tags", "paths", "symbols", "identifiers"} {
		doc.AddFieldMappingsAt(field, text)
	}
	for _, field := range []string{"type", "project", "repository", "module", "trustLevel"} {
		doc.AddFieldMappingsAt(field, kw)
	}
	doc.AddFieldMappingsAt("updatedAt", date)

	im := bleve.NewIndexMapping()
	im.DefaultMapping = doc
	im.DefaultAnalyzer = AnalyzerName
	return im
}

// Index wraps a Bleve index over a knowledge.Store's records. Per §11.11,
// canonical knowledge stays in Dolt and JSONL; this index is disposable and
// can always be rebuilt from the Store.
type Index struct {
	bleve bleve.Index

	// syncMu guards the last-synced watermark below. Rebuild is a
	// read-modify-write over the shared index, so two concurrent callers
	// (e.g. two search_knowledge calls) must not interleave it
	// (punokawan-hzp). App-level callers additionally serialize Rebuild+Search
	// under App.searchIndexMu; syncMu keeps Rebuild self-safe for callers that
	// do not (e.g. capsule retrieval).
	syncMu       sync.Mutex
	hasWatermark bool
	syncedCount  int
	syncedNewest time.Time
}

// OpenIndex opens the Bleve index at path, creating it with §11.4's mapping
// if it does not already exist.
func OpenIndex(path string) (*Index, error) {
	idx, err := bleve.Open(path)
	if err == bleve.ErrorIndexPathDoesNotExist {
		idx, err = bleve.New(path, buildIndexMapping())
	}
	if err != nil {
		return nil, fmt.Errorf("search: open index %s: %w", path, err)
	}
	return &Index{bleve: idx}, nil
}

// Close releases the underlying Bleve index's file handles.
func (ix *Index) Close() error {
	return ix.bleve.Close()
}

// IndexRecord upserts one record into the index - the incremental-update
// path §11.11 calls for, used after a single knowledge.Store.Put/Supersede
// rather than rebuilding the whole index.
func (ix *Index) IndexRecord(rec knowledge.RecordWithUpdatedAt) error {
	return ix.bleve.Index(rec.Record.Id, BuildDocument(rec.Record, rec.UpdatedAt))
}

// DeleteRecord removes id from the index, e.g. after knowledge.Store.Delete.
func (ix *Index) DeleteRecord(id string) error {
	return ix.bleve.Delete(id)
}

// Rebuild syncs the index to exactly match store's current records, in one
// batch: every current record is upserted (Bleve's Index() call is an
// upsert, so re-indexing an unchanged record is a no-op in effect), and any
// indexed id no longer present in store - e.g. after knowledge.Store.Delete
// - is removed. Pruning matters as much as upserting here: a stale index
// entry for a deleted record is exactly the "dirty context" a search result
// must not surface. This one code path serves both §11.11's "full rebuild"
// (an empty new index) and ongoing incremental sync, rather than
// maintaining a separate change-diffing mechanism that could drift out of
// sync with the canonical Dolt store.
//
// Rebuild is watermark-gated (punokawan-77q): search_knowledge calls it
// before every query for correctness, but the full scan + per-record
// DetectIdentifiers + batch upsert is O(N) and pure waste when nothing has
// changed. It records the (record count, newest updated_at) it last synced to
// and short-circuits to a no-op when the store still matches, so steady-state
// searches skip the rebuild entirely. Any Put/Supersede bumps updated_at and
// any Delete lowers the count, so a real mutation always breaks the match and
// forces a resync.
func Rebuild(store *knowledge.Store, ix *Index) error {
	records, err := store.AllWithUpdatedAt()
	if err != nil {
		return fmt.Errorf("search: rebuild: list records: %w", err)
	}

	ix.syncMu.Lock()
	defer ix.syncMu.Unlock()

	count, newest := watermarkOf(records)
	if ix.hasWatermark && ix.syncedCount == count && ix.syncedNewest.Equal(newest) {
		return nil
	}

	current := make(map[string]bool, len(records))
	for _, rec := range records {
		current[rec.Record.Id] = true
	}

	indexedIDs, err := ix.allDocIDs()
	if err != nil {
		return fmt.Errorf("search: rebuild: list indexed ids: %w", err)
	}

	batch := ix.bleve.NewBatch()
	for _, id := range indexedIDs {
		if !current[id] {
			batch.Delete(id)
		}
	}
	for _, rec := range records {
		if err := batch.Index(rec.Record.Id, BuildDocument(rec.Record, rec.UpdatedAt)); err != nil {
			return fmt.Errorf("search: rebuild: batch index %s: %w", rec.Record.Id, err)
		}
	}
	if err := ix.bleve.Batch(batch); err != nil {
		return fmt.Errorf("search: rebuild: execute batch: %w", err)
	}

	ix.hasWatermark = true
	ix.syncedCount = count
	ix.syncedNewest = newest
	return nil
}

// watermarkOf summarizes store state as the record count and the newest
// updated_at across all records - the cheap signature Rebuild compares against
// its last sync to decide whether a re-index is needed.
func watermarkOf(records []knowledge.RecordWithUpdatedAt) (int, time.Time) {
	var newest time.Time
	for _, rec := range records {
		if rec.UpdatedAt.After(newest) {
			newest = rec.UpdatedAt
		}
	}
	return len(records), newest
}

// allDocIDs returns every document id currently in the Bleve index.
func (ix *Index) allDocIDs() ([]string, error) {
	count, err := ix.bleve.DocCount()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}

	sr := bleve.NewSearchRequestOptions(bleve.NewMatchAllQuery(), int(count), 0, false)
	res, err := ix.bleve.Search(sr)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(res.Hits))
	for _, hit := range res.Hits {
		ids = append(ids, hit.ID)
	}
	return ids, nil
}
