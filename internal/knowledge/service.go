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

// Put creates or replaces a knowledge record.
func (s *Store) Put(rec protocol.KnowledgeRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("knowledge: marshal record: %w", err)
	}

	_, err = s.db.Exec(`
INSERT INTO knowledge_records (id, type, status, validity_state, data, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  type = VALUES(type), status = VALUES(status), validity_state = VALUES(validity_state),
  data = VALUES(data), updated_at = VALUES(updated_at)`,
		rec.Id, string(rec.Type), rec.Status, string(rec.Validity.State), data, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("knowledge: put %s: %w", rec.Id, err)
	}
	return nil
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

// Delete removes a knowledge record by id. It does not error if the id does
// not exist.
func (s *Store) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM knowledge_records WHERE id = ?`, id); err != nil {
		return fmt.Errorf("knowledge: delete %s: %w", id, err)
	}
	return nil
}
