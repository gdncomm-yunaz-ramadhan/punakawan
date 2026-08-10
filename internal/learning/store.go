// Package learning is the augment side-store for reviewed project-learning
// proposals (agent-context plan §6.3/§6.4). It holds only the learning-specific
// envelope the shared artifact-review model does not carry — fingerprint,
// support count, source run ids, evidence ids, rationale — and points at the
// artifact ReviewStore review that carries the actual candidate content and
// the accept/reject/apply mechanics. It is deliberately NOT a second review
// engine (plan §13): canonical mutation still happens only through the
// artifact review acceptance path and its typed adapters.
//
// Proposals persist in the shared SQLite storage kernel (internal/storage,
// punokawan-14yn.16), scoped to one project. History stays append-only: each
// state change appends a new row with the same id rather than mutating the
// original, so List folds to the latest row per id.
package learning

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
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

// Store appends and reads learning proposals for one project within the shared
// storage kernel. Schema migration happens once, centrally, when the kernel
// opens (internal/storage/migrations/0010_learning.sql) - a Store never creates
// its own tables. History is append-only: each state change appends a new row
// with the same id rather than mutating the original, so List folds to the
// latest row per id.
type Store struct {
	db        *storage.DB
	projectID string
}

// New wraps db, scoping every read and write to projectID.
func New(db *storage.DB, projectID string) *Store {
	return &Store{db: db, projectID: projectID}
}

// writeKey returns a fresh random idempotency key. Append is a genuine append -
// the same proposal id is written more than once over its lifecycle (created,
// then dedup-reinforced, then accepted/rejected) and every such write must take
// effect - so each call wants a unique key rather than the kernel's replay
// dedup collapsing them.
func writeKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("learning: generate write key: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Append writes a proposal's current state as a new entry.
func (s *Store) Append(p Proposal) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("learning: encode proposal: %w", err)
	}

	ctx := context.Background()
	key, err := writeKey()
	if err != nil {
		return err
	}
	err = s.db.Write(ctx, key, "append learning proposal "+p.Id, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO learning_proposals (project_id, id, data) VALUES (?, ?, ?)`,
			s.projectID, p.Id, string(data)); err != nil {
			return fmt.Errorf("learning: append %s: %w", p.Id, err)
		}
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return err
	}
	return nil
}

// List folds the append-only history to the latest state per proposal id,
// newest-updated first.
func (s *Store) List() ([]Proposal, error) {
	rows, err := s.db.Reader().Query(
		`SELECT data FROM learning_proposals WHERE project_id = ? ORDER BY seq ASC`, s.projectID)
	if err != nil {
		return nil, fmt.Errorf("learning: list: %w", err)
	}
	defer rows.Close()

	latest := map[string]Proposal{}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("learning: scan proposal: %w", err)
		}
		var p Proposal
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("learning: decode proposal: %w", err)
		}
		// Later seq wins: a proposal re-appended with the same id overwrites
		// its earlier state, folding the history to the current record.
		latest[p.Id] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("learning: iterate proposals: %w", err)
	}
	if len(latest) == 0 {
		return nil, nil
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
