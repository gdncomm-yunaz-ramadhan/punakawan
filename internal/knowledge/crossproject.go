package knowledge

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// qualifiedTable returns table, qualified with project's database name only
// when project names a database other than this Store's own (OwnProject) -
// ADR-0020's project filter, letting a single hub connection reach a sibling
// project's database via Dolt/MySQL's `db`.`table` syntax rather than opening
// a second connection. project must already be validated against
// projectIDPattern before reaching here: it cannot be parameterized as a SQL
// identifier.
func (s *Store) qualifiedTable(project, table string) string {
	if project == "" || project == s.project {
		return "`" + table + "`"
	}
	return "`" + project + "`.`" + table + "`"
}

// GetInProject is Get scoped to project's database instead of this Store's
// own (ADR-0020): with project empty or equal to OwnProject, it behaves
// exactly like Get. Naming a different project only works when that project
// is served by the same dolt sql-server as this Store (true of any two
// projects registered on the same hub) - reaching a legacy, non-hub Store, or
// a project not on this Store's hub, surfaces Dolt's own "database not
// found" error rather than a bespoke one, since that is exactly what
// happened: there is nothing else to read from.
func (s *Store) GetInProject(project, id string) (protocol.KnowledgeRecord, error) {
	if project != "" && project != s.project && !projectIDPattern.MatchString(project) {
		return protocol.KnowledgeRecord{}, fmt.Errorf("knowledge: GetInProject: invalid project %q", project)
	}
	table := s.qualifiedTable(project, "knowledge_records")

	var data []byte
	err := s.db.QueryRow(`SELECT data FROM `+table+` WHERE id = ?`, id).Scan(&data)
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

// SearchInProject is a lower-fidelity fallback for reaching a project other
// than this Store's own from search_knowledge (ADR-0020's project filter):
// the BM25 index a Store's own, same-project search normally queries is
// built only from that Store's own project, so it cannot rank another
// project's records at all. This scans the other project's knowledge_records
// directly for a plain substring match instead - honest about being a scan,
// not a ranked search, but sufficient to answer "does this term appear
// anywhere in that project's records", which is what a deliberate
// cross-project lookup is asking. types, if non-empty, restricts to those
// record types. Results are ordered most-recently-updated first and capped
// at limit (default 20).
func (s *Store) SearchInProject(project, text string, types []string, limit int) ([]protocol.KnowledgeRecord, error) {
	if project != "" && project != s.project && !projectIDPattern.MatchString(project) {
		return nil, fmt.Errorf("knowledge: SearchInProject: invalid project %q", project)
	}
	if limit <= 0 {
		limit = 20
	}
	table := s.qualifiedTable(project, "knowledge_records")

	query := "SELECT data FROM " + table + " WHERE data LIKE ?"
	args := []any{"%" + text + "%"}
	if len(types) > 0 {
		placeholders := make([]string, len(types))
		for i, t := range types {
			placeholders[i] = "?"
			args = append(args, t)
		}
		query += " AND type IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += fmt.Sprintf(" ORDER BY updated_at DESC LIMIT %d", limit)

	rows, err := s.db.Query(query, args...)
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
