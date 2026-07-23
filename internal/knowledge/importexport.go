package knowledge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Export writes every knowledge record currently in the store, across all
// types, as JSONL: one protocol.KnowledgeRecord JSON object per line. Records
// are ordered by id so that repeated exports of an unchanged store are
// byte-identical, keeping the output diffable and suitable for backup or
// transport, per §7.5 ("Import and export"; "JSONL is not the canonical
// relation graph" - Dolt remains canonical, this is a transport format).
func (s *Store) Export(w io.Writer) error {
	records, err := s.allRecords()
	if err != nil {
		return err
	}

	enc := json.NewEncoder(w)
	for _, rec := range records {
		if err := enc.Encode(rec); err != nil {
			return fmt.Errorf("knowledge: export encode %s: %w", rec.Id, err)
		}
	}
	return nil
}

// allRecords returns every knowledge record currently in the store, ordered
// by id, shared by Export and ExportYAML so both formats stay in sync.
func (s *Store) allRecords() ([]protocol.KnowledgeRecord, error) {
	rows, err := s.db.Query(`SELECT data FROM knowledge_records ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("knowledge: export query: %w", err)
	}
	defer rows.Close()

	var records []protocol.KnowledgeRecord
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("knowledge: export scan record: %w", err)
		}
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("knowledge: export decode record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: export iterate records: %w", err)
	}
	return records, nil
}

// RecordWithUpdatedAt pairs a KnowledgeRecord with the updated_at timestamp
// Dolt tracks on its row - not itself part of the JSON payload - for
// callers like internal/search that need recency as a ranking signal.
type RecordWithUpdatedAt struct {
	Record    protocol.KnowledgeRecord
	UpdatedAt time.Time
}

// AllWithUpdatedAt returns every knowledge record currently in the store,
// ordered by id, alongside its Dolt-tracked updated_at.
func (s *Store) AllWithUpdatedAt() ([]RecordWithUpdatedAt, error) {
	rows, err := s.db.Query(`SELECT data, updated_at FROM knowledge_records ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("knowledge: export query: %w", err)
	}
	defer rows.Close()

	var out []RecordWithUpdatedAt
	for rows.Next() {
		var data []byte
		var updatedAt time.Time
		if err := rows.Scan(&data, &updatedAt); err != nil {
			return nil, fmt.Errorf("knowledge: export scan record: %w", err)
		}
		var rec protocol.KnowledgeRecord
		if err := json.Unmarshal(data, &rec); err != nil {
			return nil, fmt.Errorf("knowledge: export decode record: %w", err)
		}
		out = append(out, RecordWithUpdatedAt{Record: rec, UpdatedAt: updatedAt})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: export iterate records: %w", err)
	}
	return out, nil
}

// PortableExport is the top-level shape of .punakawan/portable/knowledge.yaml
// per punakawan-architecture-enhancement-plan.md §10.1/§10.2: a human-
// readable export a person (not just Punakawan) can open and read, distinct
// from Export's JSONL transport format.
type PortableExport struct {
	Records []protocol.KnowledgeRecord `yaml:"records"`
}

// ExportYAML writes every knowledge record as a single portable YAML
// document, ordered by id for the same reproducibility guarantee as Export.
func (s *Store) ExportYAML(w io.Writer) error {
	records, err := s.allRecords()
	if err != nil {
		return err
	}
	if records == nil {
		records = []protocol.KnowledgeRecord{}
	}

	enc := yaml.NewEncoder(w)
	defer enc.Close()
	if err := enc.Encode(PortableExport{Records: records}); err != nil {
		return fmt.Errorf("knowledge: export yaml: %w", err)
	}
	return nil
}

// ImportYAML reads a PortableExport document produced by ExportYAML (or any
// equivalent hand-authored YAML of the same shape) and calls Put for each
// record, with the same fail-fast provenance enforcement as Import.
func (s *Store) ImportYAML(r io.Reader) error {
	var doc PortableExport
	if err := yaml.NewDecoder(r).Decode(&doc); err != nil {
		return fmt.Errorf("knowledge: import yaml: decode: %w", err)
	}

	for i, rec := range doc.Records {
		if err := s.Put(rec); err != nil {
			return fmt.Errorf("knowledge: import yaml record %d (id %s): %w (%d record(s) imported before failure)", i+1, rec.Id, err, i)
		}
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
