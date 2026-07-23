package knowledge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// EventType distinguishes what kind of write produced a knowledge-events.jsonl
// line.
type EventType string

const (
	EventTypePut       EventType = "put"
	EventTypeSupersede EventType = "supersede"
	EventTypeDelete    EventType = "delete"
)

// Event is one line of .punakawan/events/knowledge-events.jsonl, per
// punakawan-architecture-enhancement-plan.md §10.1/§10.2: an append-only
// audit trail distinct from the Dolt-canonical knowledge_records table,
// meant for external tailing rather than as a source of truth Punakawan
// itself reads back.
type Event struct {
	Type         EventType                    `json:"type"`
	RecordId     string                       `json:"record_id"`
	RecordType   protocol.KnowledgeRecordType `json:"record_type"`
	SupersededBy string                       `json:"superseded_by,omitempty"`
	Timestamp    time.Time                    `json:"timestamp"`
}

// emitEvent appends ev to the store's knowledge-events.jsonl. A failure here
// does not roll back the record write that triggered it (already committed
// to Dolt by the time this runs) but is still returned as an error: the
// event log is the only externally-tailable audit trail of this write, so a
// silently-dropped line would be a real gap, not just cosmetic.
func (s *Store) emitEvent(ev Event) error {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()

	f, err := os.OpenFile(s.eventsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("knowledge: open %s: %w", s.eventsPath, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(ev); err != nil {
		return fmt.Errorf("knowledge: encode event for %s: %w", ev.RecordId, err)
	}
	return nil
}

// Events returns every event ever emitted by this store, in append order,
// for tests and any operator tooling that wants to inspect the audit trail
// without shelling out to read knowledge-events.jsonl directly.
func (s *Store) Events() ([]Event, error) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()

	f, err := os.Open(s.eventsPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge: open %s: %w", s.eventsPath, err)
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("knowledge: decode event: %w", err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: scan %s: %w", s.eventsPath, err)
	}
	return events, nil
}
