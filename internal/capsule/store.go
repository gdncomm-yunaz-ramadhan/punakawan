package capsule

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Store persists ContextCapsules as append-only JSONL, mirroring
// internal/approvals.Store's convention. Unlike an approval record, a
// capsule never changes after it is issued (§6.1 "immutable after
// dispatch"), so there is no Resolve-style update-by-append: each id is
// written exactly once.
type Store struct {
	path string
	mu   sync.Mutex
}

// OpenStore ensures .punakawan/capsules/ exists under workspaceRoot and
// returns a Store backed by capsules.jsonl within it.
func OpenStore(workspaceRoot string) (*Store, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "capsules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("capsule: create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "capsules.jsonl")}, nil
}

// Put appends c to the store. It does not check for an existing id with the
// same value - request_capsule always mints a fresh id per call, so a
// collision would indicate a caller-supplied duplicate id, which Get's
// first-match semantics would then silently prefer over any later record
// sharing it; callers should treat capsule ids as opaque and never reuse
// one across Put calls.
func (s *Store) Put(c protocol.ContextCapsule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("capsule: open %s: %w", s.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(c); err != nil {
		return fmt.Errorf("capsule: encode capsule %s: %w", c.Id, err)
	}
	return nil
}

// ErrNotFound is returned by Get when no capsule exists for the given id.
var ErrNotFound = fmt.Errorf("capsule: not found")

// List returns every capsule ever issued, in append order. Capsules key
// by TaskId, not by run/session id (ContextCapsule has no such field), so
// a caller wanting a task's capsules filters this slice itself; there is
// no per-run or per-session capsule index today.
func (s *Store) List() ([]protocol.ContextCapsule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("capsule: open %s: %w", s.path, err)
	}
	defer f.Close()

	var out []protocol.ContextCapsule
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var c protocol.ContextCapsule
		if err := json.Unmarshal(line, &c); err != nil {
			return nil, fmt.Errorf("capsule: decode record: %w", err)
		}
		out = append(out, c)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("capsule: scan %s: %w", s.path, err)
	}
	return out, nil
}

// Get returns the capsule with the given id.
func (s *Store) Get(id string) (protocol.ContextCapsule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return protocol.ContextCapsule{}, ErrNotFound
	}
	if err != nil {
		return protocol.ContextCapsule{}, fmt.Errorf("capsule: open %s: %w", s.path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var c protocol.ContextCapsule
		if err := json.Unmarshal(line, &c); err != nil {
			return protocol.ContextCapsule{}, fmt.Errorf("capsule: decode record: %w", err)
		}
		if c.Id == id {
			return c, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return protocol.ContextCapsule{}, fmt.Errorf("capsule: scan %s: %w", s.path, err)
	}
	return protocol.ContextCapsule{}, ErrNotFound
}
