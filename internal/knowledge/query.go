package knowledge

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Count returns the total number of knowledge records in the store. It is the
// cheap path for the panel's corpus size: a single aggregate that never
// decodes a JSON blob, replacing the old "load everything, then len()" cost
// (punokawan-rit, Phase 4 §11).
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := withConnRetry(func() error {
		return s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_records`).Scan(&n)
	})
	if err != nil {
		return 0, fmt.Errorf("knowledge: count records: %w", err)
	}
	return n, nil
}

// KnowledgeListQuery narrows and paginates ListRecords. Empty string fields
// mean "no filter" for that column (via the `? = '' OR col = ?` pattern, so
// the same prepared statement serves filtered and unfiltered browses). Type,
// Status and ValidityState map to indexed columns; Repository and Source are
// filtered against the record's JSON payload (scope.repository and
// source.provider respectively). Limit <= 0 means unbounded. Cursor is an
// opaque token returned as ListRecords' nextCursor; pass it back to fetch the
// following page.
type KnowledgeListQuery struct {
	Type          string
	Status        string
	ValidityState string
	Repository    string
	Source        string
	Limit         int
	Cursor        string
}

// ListRecords returns a filtered, keyset-paginated page of knowledge records
// ordered by updated_at DESC, id ASC. Filtering on the indexed
// type/status/validity_state columns and on the JSON scope.repository /
// source.provider paths happens in SQL, before any JSON blob is decoded, so a
// first-page browse touches only its page instead of the whole corpus
// (punokawan-rit, Phase 4 §11).
//
// Pagination is keyset (seek), not OFFSET: the cursor encodes the last row's
// (updated_at, id), and the next page selects rows ordered strictly after it.
// When Limit > 0 the query fetches Limit+1 rows to detect whether a further
// page exists; if so, nextCursor is non-empty and points at the last returned
// record. An empty nextCursor means the final page was returned.
func (s *Store) ListRecords(ctx context.Context, q KnowledgeListQuery) (records []protocol.KnowledgeRecord, nextCursor string, err error) {
	var where []string
	var args []any

	addEq := func(col, val string) {
		where = append(where, fmt.Sprintf("(? = '' OR %s = ?)", col))
		args = append(args, val, val)
	}
	addEq("type", q.Type)
	addEq("status", q.Status)
	addEq("validity_state", q.ValidityState)
	// Repository and Source live only in the JSON payload, not in a dedicated
	// column, so they are extracted per row. JSON_UNQUOTE strips the quotes
	// JSON_EXTRACT would otherwise return, letting the value compare as a plain
	// string; a missing path yields SQL NULL, which never equals a non-empty
	// filter value - exactly the "record has no repository" case.
	addEq("JSON_UNQUOTE(JSON_EXTRACT(data, '$.scope.repository'))", q.Repository)
	addEq("JSON_UNQUOTE(JSON_EXTRACT(data, '$.source.provider'))", q.Source)

	if q.Cursor != "" {
		cursorTime, cursorID, decErr := decodeKnowledgeCursor(q.Cursor)
		if decErr != nil {
			return nil, "", decErr
		}
		// Seek predicate for ORDER BY updated_at DESC, id ASC: everything that
		// sorts strictly after (cursorTime, cursorID).
		where = append(where, "(updated_at < ? OR (updated_at = ? AND id > ?))")
		args = append(args, cursorTime, cursorTime, cursorID)
	}

	query := `SELECT id, data, updated_at FROM knowledge_records`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY updated_at DESC, id ASC"
	if q.Limit > 0 {
		// Over-fetch by one to learn whether another page follows.
		query += fmt.Sprintf(" LIMIT %d", q.Limit+1)
	}

	var rows *sql.Rows
	err = withConnRetry(func() (err error) {
		rows, err = s.db.QueryContext(ctx, query, args...)
		return err
	})
	if err != nil {
		return nil, "", fmt.Errorf("knowledge: list records: %w", err)
	}
	defer rows.Close()

	type scanned struct {
		id        string
		data      []byte
		updatedAt time.Time
	}
	var raw []scanned
	for rows.Next() {
		var row scanned
		if err := rows.Scan(&row.id, &row.data, &row.updatedAt); err != nil {
			return nil, "", fmt.Errorf("knowledge: scan record: %w", err)
		}
		raw = append(raw, row)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("knowledge: iterate records: %w", err)
	}

	if q.Limit > 0 && len(raw) > q.Limit {
		last := raw[q.Limit-1]
		nextCursor = encodeKnowledgeCursor(last.updatedAt, last.id)
		raw = raw[:q.Limit]
	}

	records = make([]protocol.KnowledgeRecord, 0, len(raw))
	for _, row := range raw {
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(row.data, &rec); err != nil {
			return nil, "", fmt.Errorf("knowledge: decode record %s: %w", row.id, err)
		}
		records = append(records, rec)
	}
	return records, nextCursor, nil
}

// encodeKnowledgeCursor packs a row's keyset position (updated_at, id) into an
// opaque, URL-safe token. Both components come from a prior DB read, so their
// precision already matches what a follow-up comparison will see.
func encodeKnowledgeCursor(updatedAt time.Time, id string) string {
	payload := updatedAt.UTC().Format(time.RFC3339Nano) + "\x00" + id
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

// decodeKnowledgeCursor reverses encodeKnowledgeCursor.
func decodeKnowledgeCursor(cursor string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("knowledge: invalid cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), "\x00", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("knowledge: invalid cursor payload")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("knowledge: invalid cursor timestamp: %w", err)
	}
	return t, parts[1], nil
}
