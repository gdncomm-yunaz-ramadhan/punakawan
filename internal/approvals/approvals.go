// Package approvals persists approval records as append-only JSONL, per
// punakawan-go-typescript-detailed-plan.md §16.2, §6.1.
package approvals

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Store appends and reads approval records for a workspace. History is
// append-only: resolving a request appends a new record with the same Id
// rather than mutating the original, so Current folds to the latest record
// per id while List returns full history.
type Store struct {
	path string
	mu   sync.Mutex
}

// Open ensures .punakawan/approvals/ exists under workspaceRoot and returns
// a Store backed by approvals.jsonl within it.
func Open(workspaceRoot string) (*Store, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "approvals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("approvals: create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "approvals.jsonl")}, nil
}

// Append writes a new approval record (a request, an approval, or a denial).
func (s *Store) Append(rec protocol.ApprovalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("approvals: open %s: %w", s.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(rec); err != nil {
		return fmt.Errorf("approvals: encode record: %w", err)
	}
	return nil
}

// List returns the full append-only history of approval records.
func (s *Store) List() ([]protocol.ApprovalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("approvals: open %s: %w", s.path, err)
	}
	defer f.Close()

	var records []protocol.ApprovalRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec protocol.ApprovalRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("approvals: decode record: %w", err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("approvals: scan %s: %w", s.path, err)
	}
	return records, nil
}

// Current folds the append-only history to the latest record per id.
func (s *Store) Current() (map[string]protocol.ApprovalRecord, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	latest := make(map[string]protocol.ApprovalRecord, len(all))
	for _, r := range all {
		latest[r.Id] = r
	}
	return latest, nil
}

// Pending returns approval records whose latest state is still pending.
func (s *Store) Pending() ([]protocol.ApprovalRecord, error) {
	current, err := s.Current()
	if err != nil {
		return nil, err
	}
	var pending []protocol.ApprovalRecord
	for _, r := range current {
		if r.Status == protocol.ApprovalRecordStatusPending {
			pending = append(pending, r)
		}
	}
	return pending, nil
}
