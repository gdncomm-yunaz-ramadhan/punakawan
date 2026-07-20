// Package evidence provides the append-only JSONL event journal and the
// evidence bundle directory skeleton, per
// punakawan-go-typescript-detailed-plan.md §7.5, §17, §19.1.
package evidence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Journal is an append-only JSONL event log for a single run.
type Journal struct {
	path string
	mu   sync.Mutex
}

// OpenJournal ensures .punakawan/runs/<runID>/ exists and returns a Journal
// backed by events.jsonl within it.
func OpenJournal(workspaceRoot, runID string) (*Journal, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "runs", runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("evidence: create %s: %w", dir, err)
	}
	return &Journal{path: filepath.Join(dir, "events.jsonl")}, nil
}

// Append writes one event to the journal.
func (j *Journal) Append(event protocol.Event) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("evidence: open %s: %w", j.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(event); err != nil {
		return fmt.Errorf("evidence: encode event: %w", err)
	}
	return nil
}

// List returns every event recorded in the journal, in append order.
func (j *Journal) List() ([]protocol.Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	f, err := os.Open(j.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("evidence: open %s: %w", j.path, err)
	}
	defer f.Close()

	var events []protocol.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e protocol.Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("evidence: decode event: %w", err)
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("evidence: scan %s: %w", j.path, err)
	}
	return events, nil
}
