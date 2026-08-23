package search

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ygrip/punakawan/internal/knowledge"
)

// FieldWeights are §11.5's BM25F field weights. They are no longer applied as
// per-field query boosts (that was Bleve's model); with FTS5 the weighting
// happens entirely in the bm25() ranking function's per-column weight
// arguments (see ftsWeightArgs), which reuses exactly these numbers.
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

// ftsWeightArgs is §11.5's field weights in knowledge_fts column order
// (title, summary, content, aliases, tags, paths, symbols, identifiers),
// passed straight to FTS5's bm25(knowledge_fts, w0, w1, ...) ranking function.
// FTS5's bm25() is lower-is-better, so runQuery negates it to keep this
// codebase's "higher score wins" convention.
var ftsWeightArgs = []interface{}{
	FieldWeights["title"], FieldWeights["summary"], FieldWeights["content"],
	FieldWeights["aliases"], FieldWeights["tags"], FieldWeights["paths"],
	FieldWeights["symbols"], FieldWeights["identifiers"],
}

// schema is this index's standalone DDL. Per §11.11 the index is a disposable,
// rebuildable cache, so it is NOT part of internal/storage's checksummed
// migration kernel; it runs its own idempotent CREATE ... IF NOT EXISTS here.
//
// knowledge_search holds the stored/returned fields (raw, un-tokenized), read
// back to reconstruct a storedDoc for scoring and to recompute the tokenized
// FTS values when a row is deleted or re-indexed. knowledge_fts is an
// external-content FTS5 table over the pre-tokenized text of each field; being
// external-content, it is maintained manually here (there are only three
// mutation entry points: IndexRecord, DeleteRecord, Rebuild).
const schema = `
CREATE TABLE IF NOT EXISTS knowledge_search (
  rowid       INTEGER PRIMARY KEY,
  id          TEXT NOT NULL UNIQUE,
  type        TEXT NOT NULL,
  project     TEXT NOT NULL DEFAULT '',
  repository  TEXT NOT NULL DEFAULT '',
  module      TEXT NOT NULL DEFAULT '',
  path        TEXT NOT NULL DEFAULT '',
  trust_level TEXT NOT NULL DEFAULT '',
  title       TEXT NOT NULL DEFAULT '',
  summary     TEXT NOT NULL DEFAULT '',
  content     TEXT NOT NULL DEFAULT '',
  aliases     TEXT NOT NULL DEFAULT '[]',
  tags        TEXT NOT NULL DEFAULT '[]',
  identifiers TEXT NOT NULL DEFAULT '[]',
  symbols     TEXT NOT NULL DEFAULT '[]',
  paths       TEXT NOT NULL DEFAULT '[]',
  updated_at  TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_fts USING fts5(
  title, summary, content, aliases, tags, paths, symbols, identifiers,
  content='knowledge_search', content_rowid='rowid'
);`

// dsnParams mirrors internal/storage's known-good modernc.org/sqlite settings.
const dsnParams = "_foreign_keys=1&_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000"

// Index is a standalone SQLite (FTS5) full-text index over a knowledge.Store's
// records. Per §11.11 canonical knowledge stays in the store; this index is
// disposable and can always be rebuilt from the Store.
type Index struct {
	db *sql.DB

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

// OpenIndex opens the SQLite FTS5 index at path, creating the file and its
// schema if they do not already exist. path is a single database file (the
// parent directory is created if missing), one index per workspace/project.
//
// Pre-migration deployments had Bleve write its on-disk index as a directory
// (index_meta.json + a store/ subdir) at this exact path. sql.Open happily
// opens a path shaped like that, but the schema Exec below then fails with
// SQLITE_CANTOPEN because sqlite cannot treat a directory as a database file.
// Since this index is disposable and always rebuildable from the knowledge
// Store (see App.OpenSearchIndex's doc comment), a leftover directory here is
// dead weight, not data to preserve, so it is removed before proceeding.
func OpenIndex(path string) (*Index, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		if err := os.RemoveAll(path); err != nil {
			return nil, fmt.Errorf("search: remove stale index directory %s: %w", path, err)
		}
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("search: create index dir %s: %w", dir, err)
		}
	}
	dsn := "file:" + url.PathEscape(path) + "?" + dsnParams
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("search: open index %s: %w", path, err)
	}
	// Serialize access to the single file: all mutations already go through
	// this package's three entry points, and reads never nest, so one
	// connection sidesteps SQLITE_BUSY without giving up concurrency callers
	// actually use.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("search: init index schema %s: %w", path, err)
	}
	return &Index{db: db}, nil
}

// Close releases the underlying database's file handles.
func (ix *Index) Close() error {
	return ix.db.Close()
}

// IndexRecord upserts one record into the index - the incremental-update path
// §11.11 calls for, used after a single knowledge.Store.Put/Supersede rather
// than rebuilding the whole index.
func (ix *Index) IndexRecord(rec knowledge.RecordWithUpdatedAt) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("search: index record: begin: %w", err)
	}
	if err := upsertTx(tx, rec); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: index record: commit: %w", err)
	}
	return nil
}

// DeleteRecord removes id from the index, e.g. after knowledge.Store.Delete.
func (ix *Index) DeleteRecord(id string) error {
	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("search: delete record: begin: %w", err)
	}
	if err := deleteTx(tx, id); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: delete record: commit: %w", err)
	}
	return nil
}

// upsertTx inserts or replaces one record's stored fields and its
// external-content FTS row within tx. An external-content FTS5 table is not
// kept in sync automatically, so an existing row's old tokenized values are
// removed with the special 'delete' command (recomputed from the currently
// stored raw fields so they exactly match what was indexed) before the fresh
// values are inserted.
func upsertTx(tx *sql.Tx, rec knowledge.RecordWithUpdatedAt) error {
	doc := BuildDocument(rec.Record, rec.UpdatedAt)

	oldRowid, ok, err := deleteFTSIfPresent(tx, doc.Id)
	if err != nil {
		return err
	}

	newVals := ftsValues(doc.Title, doc.Summary, doc.Content, doc.Aliases, doc.Tags, doc.Paths, doc.Symbols, doc.Identifiers)
	updatedAt := rec.UpdatedAt.UTC().Format(time.RFC3339Nano)

	var rowid int64
	if ok {
		rowid = oldRowid
		if _, err := tx.Exec(`UPDATE knowledge_search SET
			type=?, project=?, repository=?, module=?, path=?, trust_level=?,
			title=?, summary=?, content=?, aliases=?, tags=?, identifiers=?, symbols=?, paths=?, updated_at=?
			WHERE rowid=?`,
			doc.Type, doc.Project, doc.Repository, doc.Module, doc.Path, doc.TrustLevel,
			doc.Title, doc.Summary, doc.Content,
			jsonArray(doc.Aliases), jsonArray(doc.Tags), jsonArray(doc.Identifiers), jsonArray(doc.Symbols), jsonArray(doc.Paths),
			updatedAt, rowid); err != nil {
			return fmt.Errorf("search: index record: update stored fields: %w", err)
		}
	} else {
		res, err := tx.Exec(`INSERT INTO knowledge_search
			(id, type, project, repository, module, path, trust_level, title, summary, content, aliases, tags, identifiers, symbols, paths, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			doc.Id, doc.Type, doc.Project, doc.Repository, doc.Module, doc.Path, doc.TrustLevel,
			doc.Title, doc.Summary, doc.Content,
			jsonArray(doc.Aliases), jsonArray(doc.Tags), jsonArray(doc.Identifiers), jsonArray(doc.Symbols), jsonArray(doc.Paths),
			updatedAt)
		if err != nil {
			return fmt.Errorf("search: index record: insert stored fields: %w", err)
		}
		rowid, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("search: index record: last rowid: %w", err)
		}
	}

	if _, err := tx.Exec(
		`INSERT INTO knowledge_fts(rowid, title, summary, content, aliases, tags, paths, symbols, identifiers)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append([]interface{}{rowid}, newVals...)...); err != nil {
		return fmt.Errorf("search: index record: insert fts: %w", err)
	}
	return nil
}

// deleteTx removes a record's stored fields and its FTS row within tx. Deleting
// an id absent from the index is a no-op, matching Bleve's Delete.
func deleteTx(tx *sql.Tx, id string) error {
	rowid, ok, err := deleteFTSIfPresent(tx, id)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if _, err := tx.Exec(`DELETE FROM knowledge_search WHERE rowid=?`, rowid); err != nil {
		return fmt.Errorf("search: delete record: delete stored fields: %w", err)
	}
	return nil
}

// deleteFTSIfPresent removes id's row from the external-content FTS index using
// the 'delete' command, recomputing the tokenized values from the currently
// stored raw fields so they match exactly what was indexed. It leaves the
// knowledge_search row in place (callers update or delete it next) and returns
// the row's rowid. ok is false when no such id is indexed.
func deleteFTSIfPresent(tx *sql.Tx, id string) (rowid int64, ok bool, err error) {
	var (
		title, summary, content          string
		aliasesJSON, tagsJSON, pathsJSON string
		symbolsJSON, identifiersJSON     string
	)
	row := tx.QueryRow(
		`SELECT rowid, title, summary, content, aliases, tags, paths, symbols, identifiers
		 FROM knowledge_search WHERE id=?`, id)
	if err := row.Scan(&rowid, &title, &summary, &content, &aliasesJSON, &tagsJSON, &pathsJSON, &symbolsJSON, &identifiersJSON); err != nil {
		if err == sql.ErrNoRows {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("search: lookup existing row: %w", err)
	}

	oldVals := ftsValues(title, summary, content,
		parseJSONArray(aliasesJSON), parseJSONArray(tagsJSON), parseJSONArray(pathsJSON),
		parseJSONArray(symbolsJSON), parseJSONArray(identifiersJSON))
	args := append([]interface{}{rowid}, oldVals...)
	if _, err := tx.Exec(
		`INSERT INTO knowledge_fts(knowledge_fts, rowid, title, summary, content, aliases, tags, paths, symbols, identifiers)
		 VALUES ('delete', ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
		return 0, false, fmt.Errorf("search: remove stale fts row: %w", err)
	}
	return rowid, true, nil
}

// ftsValues derives the eight tokenized FTS column values (in ftsColumns
// order) from a record's raw fields. §11.6's Tokenize runs at index time and
// the SPACE-JOINED token stream is stored, so FTS5's default unicode61
// tokenizer only has to split on whitespace to recover the identifier-aware
// tokens (camelCase/snake/kebab/dotted/slash splits) Tokenize produced. The
// same derivation is used on insert and on delete, so the 'delete' command's
// values always match what was indexed.
func ftsValues(title, summary, content string, aliases, tags, paths, symbols, identifiers []string) []interface{} {
	return []interface{}{
		tokenizedStream(title),
		tokenizedStream(summary),
		tokenizedStream(content),
		tokenizedStream(strings.Join(aliases, " ")),
		tokenizedStream(strings.Join(tags, " ")),
		tokenizedStream(strings.Join(paths, " ")),
		tokenizedStream(strings.Join(symbols, " ")),
		tokenizedStream(strings.Join(identifiers, " ")),
	}
}

func tokenizedStream(text string) string {
	return strings.Join(Tokenize(text), " ")
}

func jsonArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func parseJSONArray(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// Rebuild syncs the index to exactly match store's current records, in one
// transaction: every current record is upserted and any indexed id no longer
// present in store is removed. Pruning matters as much as upserting: a stale
// entry for a deleted record is exactly the "dirty context" a search result
// must not surface. This one path serves both §11.11's full rebuild (an empty
// new index) and ongoing incremental sync.
//
// Rebuild is watermark-gated (punokawan-77q): search_knowledge calls it before
// every query for correctness, but a full scan + per-record DetectIdentifiers +
// upsert is O(N) and pure waste when nothing changed. It records the
// (record count, newest updated_at) it last synced to and short-circuits when
// the store still matches. Any Put/Supersede bumps updated_at and any Delete
// lowers the count, so a real mutation always breaks the match and forces a
// resync.
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

	tx, err := ix.db.Begin()
	if err != nil {
		return fmt.Errorf("search: rebuild: begin: %w", err)
	}
	for _, id := range indexedIDs {
		if !current[id] {
			if err := deleteTx(tx, id); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	for _, rec := range records {
		if err := upsertTx(tx, rec); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("search: rebuild: commit: %w", err)
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

// allDocIDs returns every document id currently in the index.
func (ix *Index) allDocIDs() ([]string, error) {
	rows, err := ix.db.Query(`SELECT id FROM knowledge_search`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
