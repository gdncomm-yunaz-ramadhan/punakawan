// Package prreview persists review_pr's final output - a run of Semar-
// deduplicated protocol.ReviewFindings for one PR - as append-only JSONL,
// per punakawan-architecture-enhancement-plan.md §8.2's "return final
// review" workflow step. Unlike internal/capsule or internal/approvals,
// there is no fold-latest-per-id concept here: reviewing the same PR twice
// (e.g. after pushing new changes) is two independent, equally valid
// records, not a correction of the first.
package prreview

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Record is one submit_pr_review_findings call's persisted result.
type Record struct {
	RunId             string                   `json:"run_id"`
	RepoId            string                   `json:"repo_id"`
	PullRequestNumber int                      `json:"pull_request_number"`
	Findings          []protocol.ReviewFinding `json:"findings"`
	CreatedAt         time.Time                `json:"created_at"`
}

// Store persists Records as append-only JSONL.
type Store struct {
	path string
	mu   sync.Mutex
}

// OpenStore ensures .punakawan/pr-reviews/ exists under workspaceRoot and
// returns a Store backed by reviews.jsonl within it.
func OpenStore(workspaceRoot string) (*Store, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "pr-reviews")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("prreview: create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "reviews.jsonl")}, nil
}

// Append writes one record to the store.
func (s *Store) Append(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("prreview: open %s: %w", s.path, err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(rec); err != nil {
		return fmt.Errorf("prreview: encode record: %w", err)
	}
	return nil
}

// List returns every record in the store, in append order.
func (s *Store) List() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prreview: open %s: %w", s.path, err)
	}
	defer f.Close()

	var records []Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("prreview: decode record: %w", err)
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("prreview: scan %s: %w", s.path, err)
	}
	return records, nil
}

// ForPullRequest returns every record for repoID/prNumber, in append order.
func (s *Store) ForPullRequest(repoID string, prNumber int) ([]Record, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, rec := range all {
		if rec.RepoId == repoID && rec.PullRequestNumber == prNumber {
			out = append(out, rec)
		}
	}
	return out, nil
}
