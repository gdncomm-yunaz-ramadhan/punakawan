// Package learning is the augment side-store for reviewed project-learning
// proposals (agent-context plan §6.3/§6.4). It holds only the learning-specific
// envelope the shared artifact-review model does not carry — fingerprint,
// support count, source run ids, evidence ids, rationale — and points at the
// artifact ReviewStore review that carries the actual candidate content and
// the accept/reject/apply mechanics. It is deliberately NOT a second review
// engine (plan §13): canonical mutation still happens only through the
// artifact review acceptance path and its typed adapters.
package learning

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status values mirror the coarse lifecycle a reviewer drives a proposal
// through; acceptance/rejection is recorded here when the underlying artifact
// review is accepted/rejected so the inbox can reflect it without re-reading
// every review.
const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"
)

// Artifact-type identifiers for the three learning pillars (match the artifact
// review type enum values added in this phase).
const (
	TypeWorkflow  = "workflow"
	TypeMetadata  = "project_metadata"
	TypeKnowledge = "knowledge"
)

// Proposal is one reviewed-learning proposal envelope (plan §6.3).
type Proposal struct {
	Id           string    `json:"id"`
	ArtifactType string    `json:"artifact_type"`
	TargetId     string    `json:"target_id"`
	Fingerprint  string    `json:"fingerprint"`
	Rationale    string    `json:"rationale,omitempty"`
	EvidenceIds  []string  `json:"evidence_ids,omitempty"`
	SourceRunIds []string  `json:"source_run_ids,omitempty"`
	SupportCount int       `json:"support_count"`
	ReviewId     string    `json:"review_id,omitempty"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"created_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Store persists learning proposals as append-only JSONL, folding to the
// latest record per id (same shape as the workflow-run store).
type Store struct {
	path string
	mu   sync.Mutex
}

// Open ensures .punakawan/learning/ exists under workspaceRoot.
func Open(workspaceRoot string) (*Store, error) {
	dir := filepath.Join(workspaceRoot, ".punakawan", "learning")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("learning: create %s: %w", dir, err)
	}
	return &Store{path: filepath.Join(dir, "proposals.jsonl")}, nil
}

// Append writes a proposal's current state as a new entry.
func (s *Store) Append(p Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("learning: open %s: %w", s.path, err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(p); err != nil {
		return fmt.Errorf("learning: encode proposal: %w", err)
	}
	return nil
}

// List folds the append-only history to the latest state per proposal id,
// newest-updated first.
func (s *Store) List() ([]Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("learning: open %s: %w", s.path, err)
	}
	defer f.Close()

	latest := map[string]Proposal{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var p Proposal
		if err := json.Unmarshal(line, &p); err != nil {
			return nil, fmt.Errorf("learning: decode proposal: %w", err)
		}
		latest[p.Id] = p
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("learning: scan %s: %w", s.path, err)
	}
	out := make([]Proposal, 0, len(latest))
	for _, p := range latest {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Get returns the latest state of one proposal.
func (s *Store) Get(id string) (Proposal, bool, error) {
	all, err := s.List()
	if err != nil {
		return Proposal{}, false, err
	}
	for _, p := range all {
		if p.Id == id {
			return p, true, nil
		}
	}
	return Proposal{}, false, nil
}

// FindPendingByFingerprint returns the pending proposal with the given
// fingerprint, if any — the dedup anchor (plan §6.4).
func (s *Store) FindPendingByFingerprint(fp string) (Proposal, bool, error) {
	all, err := s.List()
	if err != nil {
		return Proposal{}, false, err
	}
	for _, p := range all {
		if p.Status == StatusPending && p.Fingerprint == fp {
			return p, true, nil
		}
	}
	return Proposal{}, false, nil
}

// NormalizeKey lowercases and collapses every run of non-alphanumeric
// characters to a single space, so "Payout.Retry.Max_Attempts" and
// "payout retry max attempts" fingerprint identically (mirrors the
// contradiction ledger's deterministic, no-embeddings normalization).
func NormalizeKey(s string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSpace = false
		} else if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// hash returns "sha256:<hex>" of s.
func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// WorkflowFingerprint = project scope + normalized ordered step
// capability/intent graph (plan §6.4).
func WorkflowFingerprint(projectID string, stepCapabilityIntents []string) string {
	return hash("workflow|" + projectID + "|" + strings.Join(stepCapabilityIntents, ">"))
}

// MetadataFingerprint = project scope + case-normalized key (plan §6.4).
func MetadataFingerprint(projectID, key string) string {
	return hash("metadata|" + projectID + "|" + NormalizeKey(key))
}

// KnowledgeFingerprint = project scope + record type + normalized subject +
// source content hash (plan §6.4).
func KnowledgeFingerprint(projectID, recordType, subject, contentHash string) string {
	return hash("knowledge|" + projectID + "|" + recordType + "|" + NormalizeKey(subject) + "|" + contentHash)
}
