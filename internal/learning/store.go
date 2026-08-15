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
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Status values mirror the coarse lifecycle a reviewer drives a proposal
// through; acceptance/rejection is recorded here when the underlying artifact
// review is accepted/rejected so the inbox can reflect it without re-reading
// every review. StatusRolledBack marks a previously-accepted proposal whose
// effect has since been undone (Rollback below) — the accepted row itself is
// never edited, so the rolled-back state is a fresh append that folds on top
// of it while the prior row stays in List()'s full history.
const (
	StatusPending    = "pending"
	StatusAccepted   = "accepted"
	StatusRejected   = "rejected"
	StatusRolledBack = "rolled_back"
)

// Artifact-type identifiers for the learning pillars. TypeWorkflow,
// TypeMetadata, and TypeKnowledge match the artifact review type enum values
// added in the original phase. TypeConvention (punokawan-14yn.9 AC4) is a
// fourth pillar for a proposed project convention (e.g. a coding-style
// convention like "no ternary-emulation helpers") - unlike the other three,
// it has no artifact review type of its own; its adapter (ConventionAdapter,
// adapters.go) persists an accepted convention as a namespaced project
// metadata entry, reusing MetadataAdapter's storage rather than inventing a
// new canonical store.
const (
	TypeWorkflow   = "workflow"
	TypeMetadata   = "project_metadata"
	TypeKnowledge  = "knowledge"
	TypeConvention = "convention"
)

// Classification values distinguish how a proposal was produced and gate
// whether it may be accepted automatically or must go through review
// (punokawan-14yn.9 AC4). A detected fact backed by direct evidence, or an
// explicit user correction, may auto-accept. Anything inferred — a
// convention, command, routing rule, or policy the proposer derived rather
// than directly observed or was told — is reviewable-only and stays dormant
// until a reviewer approves it. An empty or unrecognized value is treated the
// same as ClassificationInferred: the safe default is always reviewable,
// never auto-accept.
const (
	ClassificationDetectedFact   = "detected_fact"
	ClassificationUserCorrection = "user_correction"
	ClassificationInferred       = "inferred"
)

// ValidClassification reports whether c is one of the recognized
// Classification values.
func ValidClassification(c string) bool {
	switch c {
	case ClassificationDetectedFact, ClassificationUserCorrection, ClassificationInferred:
		return true
	default:
		return false
	}
}

// AutoAcceptable reports whether a proposal classified c is safe to accept
// without review. Only a directly-observed fact or an explicit user
// correction qualify; an inferred proposal, and any unset or unrecognized
// value, is reviewable-only.
func AutoAcceptable(c string) bool {
	return c == ClassificationDetectedFact || c == ClassificationUserCorrection
}

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

	// Classification gates auto-accept vs reviewable-only (see the
	// Classification* constants above); Confidence is the proposer's
	// best-effort estimate in [0.0, 1.0] of how sure it is. Both are
	// optional/best-effort: a caller that does not set them gets the zero
	// value, and an unset Classification is treated as ClassificationInferred
	// (reviewable-only) everywhere it is consulted.
	Classification string  `json:"classification,omitempty"`
	Confidence     float64 `json:"confidence,omitempty"`

	// ProfileRevision records the project.Project.Revision (internal/project)
	// this proposal was accepted against, so a later profile change can be
	// detected as having potentially invalidated or superseded it. Recording
	// happens at acceptance time; detecting that a later revision invalidated
	// an accepted proposal is not built here.
	ProfileRevision int `json:"profile_revision,omitempty"`

	// Supersedes/SupersededBy form the same forward-pointer supersession
	// chain as KnowledgeAdapter.head (adapters.go): SupersededBy on a
	// rolled-back or replaced proposal names the proposal that replaces it;
	// Supersedes on that replacement names the one it replaced, restoring a
	// prior accepted value. Neither is set by Append itself — callers (e.g.
	// Rollback) populate them explicitly.
	Supersedes   *string `json:"supersedes,omitempty"`
	SupersededBy *string `json:"superseded_by,omitempty"`

	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

// Rollback marks proposal id as rolled back, appending a fresh history row
// carrying the same id (per this store's append-only idiom — never an
// in-place update) so List continues to fold to this new row while the prior
// accepted row it replaces stays intact in the full history. supersededBy
// optionally names the proposal that replaces the rolled-back one (e.g. one
// that restores an earlier accepted value); pass "" when there is none yet.
func (s *Store) Rollback(id, supersededBy string) error {
	cur, ok, err := s.Get(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("learning: rollback %s: not found", id)
	}
	cur.Status = StatusRolledBack
	if supersededBy != "" {
		cur.SupersededBy = &supersededBy
	}
	cur.UpdatedAt = time.Now().UTC()
	if err := s.Append(cur); err != nil {
		return fmt.Errorf("learning: rollback %s: %w", id, err)
	}
	return nil
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

// ConventionFingerprint = project scope + normalized convention id (mirrors
// MetadataFingerprint: a convention proposal dedups by scope+id like a
// metadata entry does, not by content hash like a knowledge record - there is
// exactly one pending proposal per convention id at a time, and a repeated
// detection reinforces it via FindPendingByFingerprint rather than opening a
// second one).
func ConventionFingerprint(projectID, id string) string {
	return hash("convention|" + projectID + "|" + NormalizeKey(id))
}

// KnowledgeFingerprint = project scope + record type + normalized subject +
// source content hash (plan §6.4).
func KnowledgeFingerprint(projectID, recordType, subject, contentHash string) string {
	return hash("knowledge|" + projectID + "|" + recordType + "|" + NormalizeKey(subject) + "|" + contentHash)
}

// GitCapabilitiesTargetId scopes a detected-git-capabilities proposal
// (punokawan-14yn.9 AC3) to one repository within the project, so a
// multi-repo workspace records one fact per repository rather than one
// clobbering another. repoID defaults to "default" for a call site with no
// repo id to give.
func GitCapabilitiesTargetId(repoID string) string {
	if repoID == "" {
		repoID = "default"
	}
	return "git.capabilities:" + repoID
}

// gitCapabilitiesDigest is the subset of protocol.GitCapabilities that
// GitCapabilitiesFingerprint hashes: the remote/base/tool facts
// gitops.Inspector.DetectCapabilities actually derives from inspecting the
// remote, its default branch, and push access. It deliberately excludes
// working-tree-transient state (current branch, uncommitted/untracked
// files, detached HEAD, worktree-ness, repository root) that changes on
// nearly every run regardless of whether the remote/base/tool facts
// themselves did - fingerprinting only this stable subset is what keeps
// re-detecting an unchanged repository from looking like a new fact every
// time.
type gitCapabilitiesDigest struct {
	Remotes          []protocol.GitCapabilitiesRemotesElem `json:"remotes"`
	Provider         *protocol.GitCapabilitiesProvider     `json:"provider,omitempty"`
	DefaultBranch    *string                                `json:"default_branch,omitempty"`
	IsBareRepository *bool                                  `json:"is_bare_repository,omitempty"`
	Capabilities     protocol.GitCapabilitiesCapabilities  `json:"capabilities"`
}

// GitCapabilitiesFingerprint = project scope + repo scope + the stable
// remote/base/tool digest above (plan §6.4's per-pillar fingerprint idiom,
// extended to gitops-detected git capability facts, punokawan-14yn.9 AC3).
func GitCapabilitiesFingerprint(projectID, repoID string, caps protocol.GitCapabilities) string {
	digest := gitCapabilitiesDigest{
		Remotes:          caps.Remotes,
		Provider:         caps.Provider,
		DefaultBranch:    caps.DefaultBranch,
		IsBareRepository: caps.IsBareRepository,
		Capabilities:     caps.Capabilities,
	}
	b, _ := json.Marshal(digest) // marshaling plain structs/slices/pointers never fails
	return hash("git_capabilities|" + projectID + "|" + repoID + "|" + string(b))
}
