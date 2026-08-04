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
	"io"
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

	// cachedRuns/latestByID/readOffset make List/Current/Get incremental
	// (punokawan-g3iv) instead of re-decoding runs.jsonl from scratch on
	// every call: Append is the file's only writer and always adds complete
	// new lines, never rewriting or truncating existing ones, so replaying
	// only the bytes appended since the last read - and folding just the
	// newly-decoded runs into the running latestByID table - stays correct
	// while the file grows with the workspace's full history. Without this,
	// Current()/Get() cost grew with total history size on every single
	// panel request (each run's own Checkpoints slice also grows on every
	// state transition, so a naive full re-decode gets slower on two axes
	// at once).
	cachedRuns   []protocol.WorkflowRun
	latestByID   map[string]protocol.WorkflowRun
	readOffset   int64
	lastFileInfo os.FileInfo
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

// refreshLocked brings cachedRuns/latestByID up to date with runs.jsonl,
// decoding only the bytes appended since the last call. Callers must hold
// s.mu.
func (s *Store) refreshLocked() error {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		s.cachedRuns = nil
		s.latestByID = nil
		s.readOffset = 0
		s.lastFileInfo = nil
		return nil
	}
	if err != nil {
		return fmt.Errorf("workflow: open %s: %w", s.path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("workflow: stat %s: %w", s.path, err)
	}
	// A different file can end up at this path with the same or larger size
	// than what was already read (deleted and recreated by something other
	// than this Store's own Append, e.g. a stray cleanup) - os.SameFile
	// compares file identity (inode), not just size, so this catches that
	// case even when info.Size() alone would not.
	if info.Size() < s.readOffset || (s.lastFileInfo != nil && !os.SameFile(s.lastFileInfo, info)) {
		s.cachedRuns = nil
		s.latestByID = nil
		s.readOffset = 0
	}
	s.lastFileInfo = info
	if info.Size() == s.readOffset {
		return nil
	}

	if _, err := f.Seek(s.readOffset, io.SeekStart); err != nil {
		return fmt.Errorf("workflow: seek %s: %w", s.path, err)
	}
	if s.latestByID == nil {
		s.latestByID = make(map[string]protocol.WorkflowRun)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		s.readOffset += int64(len(line)) + 1 // +1 for the newline json.Encoder wrote
		if len(line) == 0 {
			continue
		}
		var run protocol.WorkflowRun
		if err := json.Unmarshal(line, &run); err != nil {
			return fmt.Errorf("workflow: decode run: %w", err)
		}
		s.cachedRuns = append(s.cachedRuns, run)
		s.latestByID[run.Id] = run
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("workflow: scan %s: %w", s.path, err)
	}
	return nil
}

// List returns the full append-only history of run states.
func (s *Store) List() ([]protocol.WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	out := make([]protocol.WorkflowRun, len(s.cachedRuns))
	copy(out, s.cachedRuns)
	return out, nil
}

// Current folds the append-only history to the latest state per run id.
func (s *Store) Current() (map[string]protocol.WorkflowRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.refreshLocked(); err != nil {
		return nil, err
	}
	out := make(map[string]protocol.WorkflowRun, len(s.latestByID))
	for id, r := range s.latestByID {
		out[id] = r
	}
	return out, nil
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
