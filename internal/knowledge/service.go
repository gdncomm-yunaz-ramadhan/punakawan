package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

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

	ctx := context.Background()
	key, err := writeKey()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(timeLayout)

	err = s.db.Write(ctx, key, "put knowledge "+rec.Id, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO knowledge_records (project_id, id, type, status, validity_state, data, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(project_id, id) DO UPDATE SET
  type = excluded.type, status = excluded.status, validity_state = excluded.validity_state,
  data = excluded.data, updated_at = excluded.updated_at`,
			s.projectID, rec.Id, string(rec.Type), rec.Status, string(rec.Validity.State), string(data), now); err != nil {
			return fmt.Errorf("knowledge: put %s: %w", rec.Id, err)
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_relations WHERE project_id = ? AND from_id = ?`, s.projectID, rec.Id); err != nil {
			return fmt.Errorf("knowledge: clear relations for %s: %w", rec.Id, err)
		}
		for _, rel := range rec.Relations {
			if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_relations (project_id, from_id, type, to_id) VALUES (?, ?, ?, ?)`,
				s.projectID, rec.Id, string(rel.Type), rel.Target); err != nil {
				return fmt.Errorf("knowledge: index relation %s -> %s: %w", rec.Id, rel.Target, err)
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
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
	err := s.db.Reader().QueryRow(`SELECT data FROM knowledge_records WHERE project_id = ? AND id = ?`, s.projectID, id).Scan(&data)
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
	rows, err := s.db.Reader().Query(`SELECT data FROM knowledge_records WHERE project_id = ? AND type = ? ORDER BY id`, s.projectID, string(recordType))
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
	var recordType string
	err := s.db.Reader().QueryRow(`SELECT type FROM knowledge_records WHERE project_id = ? AND id = ?`, s.projectID, id).Scan(&recordType)
	existed := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("knowledge: delete %s: check existence: %w", id, err)
	}

	ctx := context.Background()
	key, err := writeKey()
	if err != nil {
		return err
	}
	werr := s.db.Write(ctx, key, "delete knowledge "+id, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_records WHERE project_id = ? AND id = ?`, s.projectID, id); err != nil {
			return fmt.Errorf("knowledge: delete %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_relations WHERE project_id = ? AND (from_id = ? OR to_id = ?)`, s.projectID, id, id); err != nil {
			return fmt.Errorf("knowledge: delete relations for %s: %w", id, err)
		}
		return nil
	})
	if werr != nil && !errors.Is(werr, storage.ErrDuplicateWrite) {
		return werr
	}

	if !existed {
		return nil
	}
	return s.emitEvent(Event{
		Type:       EventTypeDelete,
		RecordId:   id,
		RecordType: protocol.KnowledgeRecordType(recordType),
		Timestamp:  time.Now().UTC(),
	})
}

// Related returns every knowledge record that declares a relation targeting
// id. This is the reverse-lookup direction: a record's own outgoing
// relations are already available via its embedded Relations field, but
// finding which other records point at it requires the indexed
// knowledge_relations table rather than a full scan.
func (s *Store) Related(id string) ([]protocol.KnowledgeRecord, error) {
	rows, err := s.db.Reader().Query(`
SELECT r.data FROM knowledge_relations kr
JOIN knowledge_records r ON r.project_id = kr.project_id AND r.id = kr.from_id
WHERE kr.project_id = ? AND kr.to_id = ?
ORDER BY r.id`, s.projectID, id)
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
