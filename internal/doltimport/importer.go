package doltimport

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
		if err := json.Unmarshal(raw, &rec); err != nil {
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

	// Validate the commit landed: the destination now holds at least every id
	// we imported (a superset is fine - other records may predate this run),
	// and the kernel passes an integrity check.
	if err := verifyImported(ctx, db, destProjectID, valid); err != nil {
		return nil, err
	}
	if err := storage.IntegrityCheck(ctx, db.Path()); err != nil {
		return nil, fmt.Errorf("doltimport: post-import integrity check failed: %w", err)
	}
	rep.IntegrityOK = true
	rep.CompletedAt = time.Now().UTC()
	return rep, nil
}

// applyRecords upserts every decoded record and rebuilds its relation edges in
// one kernel transaction, so a failure partway through commits nothing rather
// than leaving a partial import. Relations are derived from each record's own
// embedded Relations list - the canonical source the kernel's own write path
// (Store.Put) indexes from - rather than copied from Dolt's derived
// knowledge_relations table, so the destination's relation index stays exactly
// consistent with the records actually imported.
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

// verifyImported confirms every id we imported is now readable in the kernel.
func verifyImported(ctx context.Context, db *storage.DB, projectID string, valid []decoded) error {
	after, err := existingIDs(ctx, db, projectID)
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
