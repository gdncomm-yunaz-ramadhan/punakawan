package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrNotFound is returned by Get when no record exists for the given id.
var ErrNotFound = errors.New("knowledge: record not found")

// Put creates or replaces a knowledge record, enforcing the §7.3/§7.4
// provenance rules and keeping the knowledge_relations index in sync with
// the record's embedded relations list.
func (s *Store) Put(rec protocol.KnowledgeRecord) error {
	if err := Validate(rec); err != nil {
		return err
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("knowledge: marshal record: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("knowledge: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
INSERT INTO knowledge_records (id, type, status, validity_state, data, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  type = VALUES(type), status = VALUES(status), validity_state = VALUES(validity_state),
  data = VALUES(data), updated_at = VALUES(updated_at)`,
		rec.Id, string(rec.Type), rec.Status, string(rec.Validity.State), data, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("knowledge: put %s: %w", rec.Id, err)
	}

	if _, err := tx.Exec(`DELETE FROM knowledge_relations WHERE from_id = ?`, rec.Id); err != nil {
		return fmt.Errorf("knowledge: clear relations for %s: %w", rec.Id, err)
	}
	for _, rel := range rec.Relations {
		if _, err := tx.Exec(`INSERT INTO knowledge_relations (from_id, type, to_id) VALUES (?, ?, ?)`,
			rec.Id, string(rel.Type), rel.Target); err != nil {
			return fmt.Errorf("knowledge: index relation %s -> %s: %w", rec.Id, rel.Target, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return s.emitEvent(Event{
		Type:       EventTypePut,
		RecordId:   rec.Id,
		RecordType: rec.Type,
		Timestamp:  time.Now().UTC(),
	})
}

// Supersede marks id's record as superseded by supersededBy without deleting
// it: it sets SupersededBy and validity.state=superseded, then Puts the
// record back through the same §7.3/§7.4 provenance checks as any other
// write. The record referenced by supersededBy is not required to already
// exist - Supersede does not itself create it, mirroring how a "supersedes"
// relation on the new record is the caller's own separate write.
func (s *Store) Supersede(id, supersededBy string) error {
	rec, err := s.Get(id)
	if err != nil {
		return fmt.Errorf("knowledge: supersede %s: %w", id, err)
	}
	rec.SupersededBy = &supersededBy
	rec.Validity.State = protocol.KnowledgeRecordValidityStateSuperseded
	if err := s.Put(rec); err != nil {
		return err
	}

	return s.emitEvent(Event{
		Type:         EventTypeSupersede,
		RecordId:     rec.Id,
		RecordType:   rec.Type,
		SupersededBy: supersededBy,
		Timestamp:    time.Now().UTC(),
	})
}

// Get returns a single knowledge record by id.
func (s *Store) Get(id string) (protocol.KnowledgeRecord, error) {
	var data []byte
	err := s.db.QueryRow(`SELECT data FROM knowledge_records WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.KnowledgeRecord{}, ErrNotFound
	}
	if err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("knowledge: get %s: %w", id, err)
	}
	var rec protocol.KnowledgeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("knowledge: decode %s: %w", id, err)
	}
	return rec, nil
}

// ListByType returns every knowledge record of the given type.
func (s *Store) ListByType(recordType protocol.KnowledgeRecordType) ([]protocol.KnowledgeRecord, error) {
	rows, err := s.db.Query(`SELECT data FROM knowledge_records WHERE type = ? ORDER BY id`, string(recordType))
	if err != nil {
		return nil, fmt.Errorf("knowledge: list by type %s: %w", recordType, err)
	}
	defer rows.Close()

	var records []protocol.KnowledgeRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("knowledge: scan record: %w", err)
		}
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("knowledge: decode record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: iterate records: %w", err)
	}
	return records, nil
}

// Delete removes a knowledge record by id, along with any relation edges
// pointing to or from it. It does not error if the id does not exist.
func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("knowledge: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM knowledge_records WHERE id = ?`, id); err != nil {
		return fmt.Errorf("knowledge: delete %s: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM knowledge_relations WHERE from_id = ? OR to_id = ?`, id, id); err != nil {
		return fmt.Errorf("knowledge: delete relations for %s: %w", id, err)
	}
	return tx.Commit()
}

// Related returns every knowledge record that declares a relation targeting
// id. This is the reverse-lookup direction: a record's own outgoing
// relations are already available via its embedded Relations field, but
// finding which other records point at it requires the indexed
// knowledge_relations table rather than a full scan.
func (s *Store) Related(id string) ([]protocol.KnowledgeRecord, error) {
	rows, err := s.db.Query(`
SELECT r.data FROM knowledge_relations kr
JOIN knowledge_records r ON r.id = kr.from_id
WHERE kr.to_id = ?
ORDER BY r.id`, id)
	if err != nil {
		return nil, fmt.Errorf("knowledge: related %s: %w", id, err)
	}
	defer rows.Close()

	var records []protocol.KnowledgeRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("knowledge: scan related record: %w", err)
		}
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("knowledge: decode related record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: iterate related records: %w", err)
	}
	return records, nil
}
