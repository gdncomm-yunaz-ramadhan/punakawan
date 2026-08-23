package doltimport

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// doltDatetimeLayout is how dolt renders a DATETIME column in -r json output.
const doltDatetimeLayout = "2006-01-02 15:04:05"

// Skipped records one source record that was not imported, with the reason.
type Skipped struct {
	ID     string
	Reason string
}

// Report is the manifest for one import run. It records the source (hub vs
// legacy, its directory and database), the destination project scope, the
// source inventory, what was (or, on a dry run, would be) imported, which ids
// already existed in the kernel and would be overwritten, and every skipped
// obsolete/malformed record with its reason.
type Report struct {
	Kind          SourceKind
	SourceDir     string
	SourceDB      string
	DestProjectID string

	SourceRecordCount   int
	SourceRelationCount int

	RecordsImported   int
	RelationsImported int
	Overwritten       []string
	Skipped           []Skipped

	Applied     bool
	CompletedAt time.Time
	IntegrityOK bool
}

// decoded is one source record that decoded and validated cleanly, carrying
// the normalized form to store.
type decoded struct {
	rec       protocol.KnowledgeRecord
	dataBytes []byte
	updatedAt string
}

// Run discovers nothing itself; the caller passes the already-discovered
// source, the live kernel, the destination project id (the workspace id every
// other subsystem already scopes this project's kernel rows by), and whether
// to apply. On a dry run it mutates nothing - it only reads the source and the
// destination to build the inventory. On apply it upserts every valid record
// and its relations in one kernel transaction, then runs an integrity check.
func Run(ctx context.Context, db *storage.DB, destProjectID string, src Source, apply bool) (*Report, error) {
	rep := &Report{
		Kind:          src.Kind,
		SourceDir:     src.Dir,
		SourceDB:      src.SourceDB,
		DestProjectID: destProjectID,
		Applied:       apply,
	}
	if src.Kind == KindNone {
		rep.CompletedAt = time.Now().UTC()
		return rep, nil
	}
	return runWithQuerier(ctx, db, destProjectID, src, apply, newDoltQuerier(src))
}

// runWithQuerier is the querier-injectable core of Run, so tests can drive it
// without a dolt binary while the exported Run wires the real dolt querier.
func runWithQuerier(ctx context.Context, db *storage.DB, destProjectID string, src Source, apply bool, q Querier) (*Report, error) {
	rep := &Report{
		Kind:          src.Kind,
		SourceDir:     src.Dir,
		SourceDB:      src.SourceDB,
		DestProjectID: destProjectID,
		Applied:       apply,
	}

	relCount, err := countRows(ctx, q, "knowledge_relations")
	if err != nil {
		return nil, err
	}
	rep.SourceRelationCount = relCount

	rows, err := q(ctx, "SELECT id, type, status, validity_state, data, updated_at FROM knowledge_records")
	if err != nil {
		return nil, err
	}
	rep.SourceRecordCount = len(rows)

	// Decode and validate every source record. Malformed data JSON or a record
	// that fails the kernel's provenance rules is recorded and skipped, not
	// fatal to the run and not silently dropped.
	valid := make([]decoded, 0, len(rows))
	for i, row := range rows {
		id, _ := jsonString(row["id"])
		raw := row["data"]
		var rec protocol.KnowledgeRecord
		err := jsonAny(raw, &rec)
		if err != nil && id != "" {
			// Confirmed live against a real per-project store (dolt 2.2.1):
			// dolt's own multi-row JSON scan - this package's one bulk SELECT
			// above - has been observed via direct `dolt sql` reproduction to
			// intermittently return an empty `data` column for complex/large
			// nested-JSON records, even though the identical row queried
			// alone always returns the full, correct content. That is dolt's
			// engine losing column content during the multi-row scan, not a
			// decode-shape problem jsonAny can fix, so the only way to
			// recover it is to re-fetch just this row on its own. Scoped to
			// only the rows that actually failed above, so a project that
			// never hits this dolt bug never pays for the extra query.
			if reRec, reErr := requeryRecord(ctx, q, id); reErr == nil {
				rec = reRec
				err = nil
			} else {
				err = fmt.Errorf("%w (single-row re-query also failed: %v)", err, reErr)
			}
		}
		if err != nil {
			rep.Skipped = append(rep.Skipped, Skipped{ID: fallbackID(id, i), Reason: "malformed data json: " + err.Error()})
			continue
		}
		if err := knowledge.Validate(rec); err != nil {
			rep.Skipped = append(rep.Skipped, Skipped{ID: fallbackID(rec.Id, i), Reason: err.Error()})
			continue
		}
		// Re-marshal to the exact shape Store.Put writes, so an imported record
		// reads back byte-identically to a natively written one and a re-run
		// upserts to the identical state.
		dataBytes, err := json.Marshal(rec)
		if err != nil {
			rep.Skipped = append(rep.Skipped, Skipped{ID: fallbackID(rec.Id, i), Reason: "re-encode record: " + err.Error()})
			continue
		}
		updatedRaw, _ := jsonString(row["updated_at"])
		valid = append(valid, decoded{rec: rec, dataBytes: dataBytes, updatedAt: normalizeUpdatedAt(updatedRaw)})
	}

	// Report which valid ids already exist in the destination scope: on apply
	// they are overwritten (the kernel's write path is an upsert), so a dry run
	// flags them so a human knows what an apply would replace.
	existing, err := existingIDs(ctx, db, destProjectID)
	if err != nil {
		return nil, err
	}
	for _, d := range valid {
		if existing[d.rec.Id] {
			rep.Overwritten = append(rep.Overwritten, d.rec.Id)
		}
	}

	if !apply {
		rep.CompletedAt = time.Now().UTC()
		return rep, nil
	}

	imported, relations, err := applyRecords(ctx, db, destProjectID, valid)
	if err != nil {
		return nil, err
	}
	rep.RecordsImported = imported
	rep.RelationsImported = relations

	// applyRecords already proved every imported id reads back correctly
	// before its transaction committed (see verifyImported), so getting here
	// means that already passed. What is left to check post-commit is general
	// database-file health, which the integrity check's own separate
	// connection cannot see mid-transaction anyway.
	if err := storage.IntegrityCheck(ctx, db.Path()); err != nil {
		return nil, fmt.Errorf("doltimport: post-import integrity check failed: %w", err)
	}
	rep.IntegrityOK = true
	rep.CompletedAt = time.Now().UTC()

	if err := writeManifest(db, destProjectID, rep); err != nil {
		return nil, err
	}
	return rep, nil
}

// requeryRecord re-fetches one record by id with a single-row point query and
// decodes its data column, for the targeted dolt-multi-row-scan-bug fallback
// in runWithQuerier above. id is embedded via sqlQuote since this Querier has
// no parameterized-query support (it shells out to the dolt CLI, not a
// driver). Only called for a row whose bulk-query decode already failed, so
// it is one extra query per genuinely affected row, never per row.
func requeryRecord(ctx context.Context, q Querier, id string) (protocol.KnowledgeRecord, error) {
	rows, err := q(ctx, "SELECT id, type, status, validity_state, data, updated_at FROM knowledge_records WHERE id = "+sqlQuote(id))
	if err != nil {
		return protocol.KnowledgeRecord{}, err
	}
	if len(rows) == 0 {
		return protocol.KnowledgeRecord{}, fmt.Errorf("no row for id %s", id)
	}
	var rec protocol.KnowledgeRecord
	if err := jsonAny(rows[0]["data"], &rec); err != nil {
		return protocol.KnowledgeRecord{}, err
	}
	return rec, nil
}

// writeManifest persists rep as the import manifest for destProjectID,
// overwriting any prior manifest for that project - a re-run's manifest
// reflects only the latest run, since the import itself is already
// idempotent and auditable via git-tracked backups elsewhere. Only called
// after a successful apply, never on a dry run.
func writeManifest(db *storage.DB, destProjectID string, rep *Report) error {
	path := manifestPath(db, destProjectID)
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("doltimport: encode manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("doltimport: write manifest %s: %w", path, err)
	}
	return nil
}

// manifestPath is the on-disk location of a project's import manifest: next
// to the kernel database file itself.
func manifestPath(db *storage.DB, destProjectID string) string {
	return filepath.Join(filepath.Dir(db.Path()), fmt.Sprintf("doltimport-manifest-%s.json", destProjectID))
}

// applyRecords upserts every decoded record and rebuilds its relation edges in
// one kernel transaction, so a failure partway through commits nothing rather
// than leaving a partial import. Relations are derived from each record's own
// embedded Relations list - the canonical source the kernel's own write path
// (Store.Put) indexes from - rather than copied from Dolt's derived
// knowledge_relations table, so the destination's relation index stays exactly
// consistent with the records actually imported.
//
// Before the callback returns, it reads every imported id back through the
// same *sql.Tx (verifyImported) - not through a separate connection - so a
// read-back failure returns an error from inside db.Write's callback. That
// makes db.Write roll back the entire transaction: nothing partial or
// unverified is ever committed to the live kernel.
func applyRecords(ctx context.Context, db *storage.DB, projectID string, valid []decoded) (records, relations int, err error) {
	key, err := writeKey()
	if err != nil {
		return 0, 0, err
	}
	summary := fmt.Sprintf("dolt->sqlite import: %d knowledge records into %s", len(valid), projectID)

	werr := db.Write(ctx, key, summary, func(tx *sql.Tx) error {
		for _, d := range valid {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_records (project_id, id, type, status, validity_state, data, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, id) DO UPDATE SET
  type = excluded.type, status = excluded.status, validity_state = excluded.validity_state,
  data = excluded.data, updated_at = excluded.updated_at`,
				projectID, d.rec.Id, string(d.rec.Type), d.rec.Status, string(d.rec.Validity.State), string(d.dataBytes), d.updatedAt); err != nil {
				return fmt.Errorf("doltimport: upsert %s: %w", d.rec.Id, err)
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_relations WHERE project_id = ? AND from_id = ?`, projectID, d.rec.Id); err != nil {
				return fmt.Errorf("doltimport: clear relations for %s: %w", d.rec.Id, err)
			}
			for _, rel := range d.rec.Relations {
				if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_relations (project_id, from_id, type, to_id) VALUES (?, ?, ?, ?)`,
					projectID, d.rec.Id, string(rel.Type), rel.Target); err != nil {
					return fmt.Errorf("doltimport: index relation %s -> %s: %w", d.rec.Id, rel.Target, err)
				}
				relations++
			}
			records++
		}
		// Read every imported id back through this same transaction, before
		// it commits. If any is missing, return the error here so db.Write
		// rolls back the whole transaction instead of committing a partial
		// or unverified import.
		if err := verifyImported(ctx, tx, projectID, valid); err != nil {
			return err
		}
		return nil
	})
	if werr != nil && !errors.Is(werr, storage.ErrDuplicateWrite) {
		return 0, 0, werr
	}
	return records, relations, nil
}

// existingIDs returns the set of knowledge record ids already present in the
// destination project scope.
func existingIDs(ctx context.Context, db *storage.DB, projectID string) (map[string]bool, error) {
	rows, err := db.Reader().QueryContext(ctx, `SELECT id FROM knowledge_records WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("doltimport: read existing ids: %w", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("doltimport: scan existing id: %w", err)
		}
		set[id] = true
	}
	return set, rows.Err()
}

// existingIDsTx is existingIDs' transaction-scoped counterpart: it reads
// through tx rather than db.Reader()'s separate connection, so it sees
// uncommitted writes made earlier in the same transaction.
func existingIDsTx(ctx context.Context, tx *sql.Tx, projectID string) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM knowledge_records WHERE project_id = ?`, projectID)
	if err != nil {
		return nil, fmt.Errorf("doltimport: read existing ids: %w", err)
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("doltimport: scan existing id: %w", err)
		}
		set[id] = true
	}
	return set, rows.Err()
}

// verifyImported confirms every id we imported is now readable, by querying
// through the same in-flight transaction that wrote it (tx), not a separate
// connection: a separate connection cannot see this transaction's uncommitted
// writes, and querying after commit would be too late to prevent a bad commit
// from landing. Called from inside applyRecords' db.Write callback, so a
// failure here still rolls back the whole transaction.
func verifyImported(ctx context.Context, tx *sql.Tx, projectID string, valid []decoded) error {
	after, err := existingIDsTx(ctx, tx, projectID)
	if err != nil {
		return err
	}
	for _, d := range valid {
		if !after[d.rec.Id] {
			return fmt.Errorf("doltimport: post-import read-back missing %s", d.rec.Id)
		}
	}
	return nil
}

// normalizeUpdatedAt converts Dolt's DATETIME rendering into the kernel's
// fixed-width layout, treating a zone-less Dolt timestamp as UTC. An
// unparseable or empty value falls back to now, which is still a valid,
// correctly-ordered kernel timestamp.
func normalizeUpdatedAt(raw string) string {
	if raw != "" {
		if t, err := time.Parse(doltDatetimeLayout, raw); err == nil {
			return t.UTC().Format(knowledge.TimeLayout)
		}
		if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return t.UTC().Format(knowledge.TimeLayout)
		}
	}
	return time.Now().UTC().Format(knowledge.TimeLayout)
}

// fallbackID gives a skipped record a human-locatable id when its own id is
// missing or unusable (e.g. its data blob failed to decode).
func fallbackID(id string, index int) string {
	if id != "" {
		return id
	}
	return fmt.Sprintf("<row %d, id unavailable>", index)
}

// writeKey returns a fresh random idempotency key. A fresh key per apply lets
// the import always run its transaction; the row-level upsert (and preserved
// source updated_at) make a re-run a true no-op on the data, so a unique key
// does not risk double-counting.
func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("doltimport: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
