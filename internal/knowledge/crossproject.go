package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// GetInProject is Get scoped to an explicitly named project instead of this
// Store's own: with project empty it behaves exactly like Get. Now that every
// project's records live in one shared database keyed by project_id, reaching
// another project is just a different value in the WHERE clause - no separate
// connection or database qualification, and no id-validation ceremony, since
// project is a bound parameter, not an interpolated SQL identifier.
func (s *Store) GetInProject(project, id string) (protocol.KnowledgeRecord, error) {
	if project == "" {
		project = s.projectID
	}

	var data []byte
	err := s.db.Reader().QueryRow(`SELECT data FROM knowledge_records WHERE project_id = ? AND id = ?`, project, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.KnowledgeRecord{}, ErrNotFound
	}
	if err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("knowledge: get %s in project %q: %w", id, project, err)
	}
	var rec protocol.KnowledgeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("knowledge: decode %s in project %q: %w", id, project, err)
	}
	return rec, nil
}

// SearchInProject is a plain substring scan of another project's records, the
// cross-project fallback for search_knowledge: the BM25 index a same-project
// search queries is built only from the calling project's records, so it
// cannot rank another project's at all. This is honest about being a scan, not
// a ranked search, but sufficient to answer "does this term appear anywhere in
// that project's records". types, if non-empty, restricts to those record
// types. Results are ordered most-recently-updated first and capped at limit
// (default 20). project empty means this Store's own project.
func (s *Store) SearchInProject(project, text string, types []string, limit int) ([]protocol.KnowledgeRecord, error) {
	if project == "" {
		project = s.projectID
	}
	if limit <= 0 {
		limit = 20
	}

	query := "SELECT data FROM knowledge_records WHERE project_id = ? AND data LIKE ?"
	args := []any{project, "%" + text + "%"}
	if len(types) > 0 {
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		query += " AND type IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT %d", limit)

	rows, err := s.db.Reader().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge: search in project %q: %w", project, err)
	}
	defer rows.Close()

	var records []protocol.KnowledgeRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("knowledge: scan record in project %q: %w", project, err)
		}
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("knowledge: decode record in project %q: %w", project, err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: iterate records in project %q: %w", project, err)
	}
	return records, nil
}
