package workflow

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CapabilityEvent is one run-scoped record of a capability being exercised
// (agent-context plan §4.3/§6): which run and (optional) step used which
// capability, in which role, and how it turned out. The append-only log gives
// both definition-backed and ad hoc runs a structured trace, which is what a
// later workflow-learning proposal references instead of mining a transcript
// (plan §6.2).
type CapabilityEvent struct {
	RunId      string    `json:"run_id"`
	StepId     string    `json:"step_id,omitempty"`
	Capability string    `json:"capability"`
	Role       string    `json:"role,omitempty"`
	Result     string    `json:"result"`
	DurationMs int64     `json:"duration_ms,omitempty"`
	At         time.Time `json:"at"`
}

// EventStore appends and reads capability events for a workspace. Same
// append-only JSONL shape as the run store; separate file so a run's state
// history and its capability trace stay independently readable.
type EventStore struct {
	path string
	mu   sync.Mutex
}

// OpenEvents ensures .punakawan/workflow/ exists and returns an EventStore
// backed by capability_events.jsonl within it.
func OpenEvents(workspaceRoot string) (*EventStore, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("workflow: create %s: %w", dir, err)
	}
	return &EventStore{path: filepath.Join(dir, "capability_events.jsonl")}, nil
}

// Append records one capability event.
func (s *EventStore) Append(ev CapabilityEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("workflow: open %s: %w", s.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(ev); err != nil {
		return fmt.Errorf("workflow: encode capability event: %w", err)
	}
	return nil
}

// ForRun returns every capability event recorded for a run, in append order.
func (s *EventStore) ForRun(runID string) ([]CapabilityEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("workflow: open %s: %w", s.path, err)
	}
	defer f.Close()

	var events []CapabilityEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev CapabilityEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("workflow: decode capability event: %w", err)
		}
		if ev.RunId == runID {
			events = append(events, ev)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("workflow: scan %s: %w", s.path, err)
	}
	return events, nil
}
