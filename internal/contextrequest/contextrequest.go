// Package contextrequest persists protocol.MissingContextRequest records
// as append-only JSONL, the round trip punakawan-architecture-enhancement-
// plan.md §6.4 describes: a subagent requests context its capsule didn't
// include, and Semar - the calling agent, not punakawan itself (ADR-0016)
// - decides whether to search for it, add it to a new capsule revision,
// reject it, or ask the user. Resolving a request appends a new record
// with the same Id rather than mutating the original, mirroring
// internal/approvals.Store's fold-latest-per-id convention.
package contextrequest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Store appends and reads MissingContextRequest records for a workspace.
type Store struct {
	path string
	mu   sync.Mutex
}

// OpenStore ensures .punakawan/context-requests/ exists under
// workspaceRoot and returns a Store backed by requests.jsonl within it.
func OpenStore(workspaceRoot string) (*Store, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "context-requests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("contextrequest: create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "requests.jsonl")}, nil
}

// Append writes a new record (an initial submission, or a later
// resolution of the same id) to the store.
func (s *Store) Append(rec protocol.MissingContextRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("contextrequest: open %s: %w", s.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(rec); err != nil {
		return fmt.Errorf("contextrequest: encode record %s: %w", rec.Id, err)
	}
	return nil
}

// List returns every record in the store, in append order (full history,
// including superseded resolutions of the same id).
func (s *Store) List() ([]protocol.MissingContextRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("contextrequest: open %s: %w", s.path, err)
	}
	defer f.Close()

	var out []protocol.MissingContextRequest
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec protocol.MissingContextRequest
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("contextrequest: decode record: %w", err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("contextrequest: scan %s: %w", s.path, err)
	}
	return out, nil
}

// Current folds List's full history down to the latest record per id, the
// current state of every request ever submitted.
func (s *Store) Current() (map[string]protocol.MissingContextRequest, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	current := make(map[string]protocol.MissingContextRequest, len(all))
	for _, rec := range all {
		current[rec.Id] = rec
	}
	return current, nil
}

// ErrNotFound is returned by Get when no record exists for the given id.
var ErrNotFound = fmt.Errorf("contextrequest: not found")

// Get returns the latest record for id.
func (s *Store) Get(id string) (protocol.MissingContextRequest, error) {
	current, err := s.Current()
	if err != nil {
		return protocol.MissingContextRequest{}, err
	}
	rec, ok := current[id]
	if !ok {
		return protocol.MissingContextRequest{}, ErrNotFound
	}
	return rec, nil
}
