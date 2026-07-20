// Package workflow persists WorkflowRun state-machine checkpoints as
// append-only JSONL, per punakawan-go-typescript-detailed-plan.md §18.1
// (run state machine) and §18.2 ("load last durable checkpoint" on
// restart). This mirrors internal/approvals' pattern: history is
// append-only, and Current folds it to the latest record per run id.
package workflow

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Store appends and reads WorkflowRun records for a workspace. Each state
// transition is persisted by appending the run's full, updated state
// (including its growing Checkpoints slice) rather than mutating a prior
// entry, so Current folds to the latest record per id while List returns
// full history — the same durable-checkpoint shape §18.2's recovery
// procedure needs ("load last durable checkpoint").
type Store struct {
	path string
	mu   sync.Mutex
}

// Open ensures .punakawan/workflow/ exists under workspaceRoot and returns
// a Store backed by runs.jsonl within it.
func Open(workspaceRoot string) (*Store, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "workflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("workflow: create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "runs.jsonl")}, nil
}

// Append writes run's current state as a new entry in the run history.
func (s *Store) Append(run protocol.WorkflowRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("workflow: open %s: %w", s.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(run); err != nil {
		return fmt.Errorf("workflow: encode run: %w", err)
	}
	return nil
}

// List returns the full append-only history of run states.
func (s *Store) List() ([]protocol.WorkflowRun, error) {
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

	var runs []protocol.WorkflowRun
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var run protocol.WorkflowRun
		if err := json.Unmarshal(line, &run); err != nil {
			return nil, fmt.Errorf("workflow: decode run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("workflow: scan %s: %w", s.path, err)
	}
	return runs, nil
}

// Current folds the append-only history to the latest state per run id.
func (s *Store) Current() (map[string]protocol.WorkflowRun, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	latest := make(map[string]protocol.WorkflowRun, len(all))
	for _, r := range all {
		latest[r.Id] = r
	}
	return latest, nil
}

// ErrNotFound is returned by Get when no run exists for the given id.
var ErrNotFound = fmt.Errorf("workflow: run not found")

// Get returns the latest known state of the run identified by id.
func (s *Store) Get(id string) (protocol.WorkflowRun, error) {
	current, err := s.Current()
	if err != nil {
		return protocol.WorkflowRun{}, err
	}
	run, ok := current[id]
	if !ok {
		return protocol.WorkflowRun{}, ErrNotFound
	}
	return run, nil
}
