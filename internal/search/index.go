package search

import (
	"fmt"

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

// Rebuild re-indexes every record currently in store, in one batch. This is
// both §11.11's "full rebuild" (called once against an empty new index) and
// the simplest correct way to keep the index in sync incrementally (Bleve's
// Index() call is an upsert, so re-indexing an unchanged record is a no-op
// in effect) - Rebuild is deliberately the one code path both cases share,
// rather than maintaining a separate change-diffing mechanism that could
// drift out of sync with the canonical Dolt store.
func Rebuild(store *knowledge.Store, ix *Index) error {
	records, err := store.AllWithUpdatedAt()
	if err != nil {
		return fmt.Errorf("search: rebuild: list records: %w", err)
	}

	batch := ix.bleve.NewBatch()
	for _, rec := range records {
		if err := batch.Index(rec.Record.Id, BuildDocument(rec.Record, rec.UpdatedAt)); err != nil {
			return fmt.Errorf("search: rebuild: batch index %s: %w", rec.Record.Id, err)
		}
	}
	if err := ix.bleve.Batch(batch); err != nil {
		return fmt.Errorf("search: rebuild: execute batch: %w", err)
	}
	return nil
}
