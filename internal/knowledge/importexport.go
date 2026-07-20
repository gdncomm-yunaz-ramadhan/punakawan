package knowledge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Export writes every knowledge record currently in the store, across all
// types, as JSONL: one protocol.KnowledgeRecord JSON object per line. Records
// are ordered by id so that repeated exports of an unchanged store are
// byte-identical, keeping the output diffable and suitable for backup or
// transport, per §7.5 ("Import and export"; "JSONL is not the canonical
// relation graph" - Dolt remains canonical, this is a transport format).
func (s *Store) Export(w io.Writer) error {
	rows, err := s.db.Query(`SELECT data FROM knowledge_records ORDER BY id`)
	if err != nil {
		return fmt.Errorf("knowledge: export query: %w", err)
	}
	defer rows.Close()

	enc := json.NewEncoder(w)
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return fmt.Errorf("knowledge: export scan record: %w", err)
		}
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return fmt.Errorf("knowledge: export decode record: %w", err)
		}
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("knowledge: export encode %s: %w", rec.Id, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("knowledge: export iterate records: %w", err)
	}
	return nil
}

// Import reads JSONL produced by Export (or any equivalent JSONL of
// protocol.KnowledgeRecord objects), one record per line, and calls Put for
// each so the §7.3/§7.4 provenance rules are enforced uniformly for
// imported data exactly as they are for records created any other way.
//
// Import fails fast: if a line is malformed JSON or a record fails Put's
// validation, Import stops and returns an error identifying the 1-based
// line number, the record id (when it could be parsed), and how many
// records were successfully imported before the failure. Store does not
// expose a cross-Put transaction (each Put is its own Dolt transaction), so
// a partial import is possible when Import fails partway through; the error
// message reports the count so the caller knows how far it got. A
// skip-and-continue mode was considered and rejected: silently dropping
// invalid provenance on import would undermine the same rules Put enforces
// on every other write path.
func (s *Store) Import(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNo := 0
	imported := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return fmt.Errorf("knowledge: import line %d: decode record: %w (%d record(s) imported before failure)", lineNo, err, imported)
		}
		if err := s.Put(rec); err != nil {
			return fmt.Errorf("knowledge: import line %d (id %s): %w (%d record(s) imported before failure)", lineNo, rec.Id, err, imported)
		}
		imported++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("knowledge: import scan: %w (%d record(s) imported before failure)", err, imported)
	}
	return nil
}
