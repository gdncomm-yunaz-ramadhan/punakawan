package recipe

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ErrRecipeNotFound is returned by RecipeStore.Current/Version when no
// retrieval-recipe record exists anywhere in id's lineage.
var ErrRecipeNotFound = errors.New("recipe: artifact not found")

// RecipeStore adapts Repository (backed by internal/knowledge's existing
// Dolt Store) to internal/artifact.Store, so the same artifact-review
// HTTP handlers that already work against *artifact.PlanStore also work
// for retrieval_recipe artifacts, per punokawan-q9r.6's explicit
// instruction to reuse punokawan-apy.6's protocol rather than build a
// second one.
//
// This type deliberately lives in internal/recipe, not internal/artifact:
// internal/artifact must not import internal/recipe (that dependency
// already runs the other way - internal/recipe has no need of
// internal/artifact's Markdown/plan-specific pieces, only its ArtifactStore
// interface and Hash helper), so the adapter sits on the recipe side of
// that boundary and is wired together with *artifact.PlanStore only in
// internal/panel/server.
//
// # Why "version" and "id" do not work like a plan's
//
// A plan is one stable id with an append-only versions/<n>.md sequence
// (internal/artifact.PlanStore). A retrieval recipe is not: per
// Repository.CreateVersion, an accepted correction is a brand-new
// protocol.KnowledgeRecord with its own id, linked back to the record it
// replaces via that old record's SupersededBy pointer (§10's immutable
// versioning, already implemented by punokawan-q9r.5 - this package does
// not change that). There is no single stable id a recipe keeps forever.
//
// RecipeStore reconciles this with ArtifactStore's plan-shaped contract
// by walking the SupersededBy chain: "current" for any id in a lineage is
// whichever record in that chain has no SupersededBy, and "version N" is
// whichever record in the chain has RetrievalRecipe.RecipeVersion == N
// (defaulting to 1 for a record with no explicit RecipeVersion, matching
// how a first-ever version is created). A review's ArtifactReference.Id
// is pinned to whatever record id was current when the review was opened
// - Current/Version both accept any id in the lineage, not only the
// current head, so a review opened against an older id still resolves
// correctly even after a later correction has moved the head elsewhere.
type RecipeStore struct {
	Repo *Repository

	locksOnce sync.Once
	locks     *artifact.KeyedMutex
}

// LockArtifact serializes a caller's read-check-write sequence against
// id's lineage - see artifact.Store's doc comment on why AcceptProposalHandler
// needs this across CreateVersion, not just within it.
func (s *RecipeStore) LockArtifact(id string) func() {
	s.locksOnce.Do(func() { s.locks = artifact.NewKeyedMutex() })
	return s.locks.Lock(id)
}

// Current returns id's current (head) version reference. id may be any
// record in the lineage, not only the current head.
func (s *RecipeStore) Current(id string) (protocol.ArtifactReference, error) {
	head, err := s.head(id)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return s.reference(head)
}

// Version returns the content and reference of whichever record in id's
// lineage has RetrievalRecipe.RecipeVersion == version.
func (s *RecipeStore) Version(id string, version int) ([]byte, protocol.ArtifactReference, error) {
	chain, err := s.lineage(id)
	if err != nil {
		return nil, protocol.ArtifactReference{}, err
	}
	for _, rec := range chain {
		if recipeVersionOf(rec) == version {
			content, err := MarshalCanonical(rec)
			if err != nil {
				return nil, protocol.ArtifactReference{}, err
			}
			ref, err := s.reference(rec)
			if err != nil {
				return nil, protocol.ArtifactReference{}, err
			}
			return content, ref, nil
		}
	}
	return nil, protocol.ArtifactReference{}, fmt.Errorf("%w: version %d of %q", artifact.ErrVersionNotFound, version, id)
}

// CreateVersion accepts a proposed recipe correction: content is
// unmarshaled back into a protocol.KnowledgeRecord (the inverse of
// MarshalCanonical - a proposal's content must be a complete, valid
// recipe record, per §9's "complete artifact proposal," not a fragment)
// and persisted via Repository.CreateVersion, which supersedes id's
// current head.
//
// CreateVersion always mints its own new record id, ignoring whatever id
// the proposed content carries (if any) - mirroring how
// *artifact.PlanStore.CreateVersion always computes the next version
// number itself rather than trusting caller input. A reviewing agent
// revising a recipe has no reason to know this package's own
// supersede-chain id scheme, and reusing the current head's id verbatim
// would create a self-referential SupersededBy cycle (a record pointing
// at itself), which head/lineage's cycle guard would then have to detect
// and refuse rather than silently mishandle. The returned reference's Id
// is therefore always the freshly minted one, never the id the caller
// passed in - per this type's own doc comment, a recipe correction
// always mints a new lineage member, it never rewrites one in place.
func (s *RecipeStore) CreateVersion(id, workspaceID string, content []byte, now time.Time) (protocol.ArtifactReference, error) {
	head, err := s.head(id)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}

	var proposed protocol.KnowledgeRecord
	if err := json.Unmarshal(content, &proposed); err != nil {
		return protocol.ArtifactReference{}, fmt.Errorf("recipe: create version: proposed content is not a valid knowledge record: %w", err)
	}
	if proposed.RetrievalRecipe == nil {
		return protocol.ArtifactReference{}, fmt.Errorf("recipe: create version: proposed content has no retrieval_recipe body")
	}
	if proposed.Type == "" {
		proposed.Type = protocol.KnowledgeRecordTypeRetrievalRecipe
	}

	newID, err := nextLineageID(head.Id)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	proposed.Id = newID

	nextVersion := recipeVersionOf(head) + 1
	proposed.RetrievalRecipe.RecipeVersion = &nextVersion
	if proposed.Validity.State == "" {
		proposed.Validity.State = protocol.KnowledgeRecordValidityStateDraft
	}

	saved, err := s.Repo.CreateVersion(proposed, head.Id)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return s.reference(saved)
}

// nextLineageID mints a fresh knowledge-record id for a corrected recipe
// version, derived from the lineage's own head id for traceability (a
// human skimming `bd`/knowledge listings can tell at a glance which
// lineage a corrected id belongs to) plus a random suffix so concurrent
// corrections of the same recipe never collide.
func nextLineageID(headID string) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("recipe: mint new version id: %w", err)
	}
	return fmt.Sprintf("%s+%s", headID, hex.EncodeToString(buf)), nil
}

// head returns the live (non-superseded) end of id's lineage.
func (s *RecipeStore) head(id string) (protocol.KnowledgeRecord, error) {
	rec, err := s.Repo.Store.Get(id)
	if err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("%w: %s", ErrRecipeNotFound, id)
	}
	seen := map[string]bool{rec.Id: true}
	for rec.SupersededBy != nil {
		nextID := *rec.SupersededBy
		if seen[nextID] {
			return protocol.KnowledgeRecord{}, fmt.Errorf("recipe: supersede chain from %q cycles back to %q", id, nextID)
		}
		next, err := s.Repo.Store.Get(nextID)
		if err != nil {
			// A dangling supersede pointer is a data-integrity gap, not a
			// reason to fail the whole lookup: the last record this
			// package could actually read is the best available "current".
			break
		}
		rec = next
		seen[nextID] = true
	}
	return rec, nil
}

// lineage returns every record connected to id by a SupersededBy edge (in
// either direction), oldest first. It scans every retrieval-recipe record
// once rather than requiring a reverse index, matching Repository.Search's
// own list-then-filter approach - recipe counts are small enough that this
// is not a real cost, and no reverse ("who points at me") index exists
// yet to do better.
func (s *RecipeStore) lineage(id string) ([]protocol.KnowledgeRecord, error) {
	all, err := s.Repo.Store.ListByType(protocol.KnowledgeRecordTypeRetrievalRecipe)
	if err != nil {
		return nil, fmt.Errorf("recipe: lineage: %w", err)
	}
	byID := make(map[string]protocol.KnowledgeRecord, len(all))
	for _, rec := range all {
		byID[rec.Id] = rec
	}
	start, ok := byID[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrRecipeNotFound, id)
	}

	// Walk backward via a predecessor index (build once), then forward via
	// SupersededBy, collecting the whole connected chain.
	predecessorOf := make(map[string]string, len(all))
	for _, rec := range all {
		if rec.SupersededBy != nil {
			predecessorOf[*rec.SupersededBy] = rec.Id
		}
	}

	var chain []protocol.KnowledgeRecord
	cur := start
	for {
		predID, ok := predecessorOf[cur.Id]
		if !ok {
			break
		}
		pred, ok := byID[predID]
		if !ok {
			break
		}
		cur = pred
	}
	seen := map[string]bool{}
	for {
		if seen[cur.Id] {
			break
		}
		seen[cur.Id] = true
		chain = append(chain, cur)
		if cur.SupersededBy == nil {
			break
		}
		next, ok := byID[*cur.SupersededBy]
		if !ok {
			break
		}
		cur = next
	}
	return chain, nil
}

// reference builds an ArtifactReference for rec. Unlike a plan version,
// a recipe version has no canonical_location file path - its canonical
// form lives in durable knowledge (the plan's own §7 layout note), so
// CanonicalLocation is left nil.
func (s *RecipeStore) reference(rec protocol.KnowledgeRecord) (protocol.ArtifactReference, error) {
	content, err := MarshalCanonical(rec)
	if err != nil {
		return protocol.ArtifactReference{}, err
	}
	return protocol.ArtifactReference{
		Type:         protocol.ArtifactReferenceTypeRetrievalRecipe,
		Id:           rec.Id,
		Version:      recipeVersionOf(rec),
		RevisionHash: artifact.Hash(content),
		WorkspaceId:  workspaceIDOf(rec),
		Format:       protocol.ArtifactReferenceFormatJson,
	}, nil
}

// recipeVersionOf returns rec's RetrievalRecipe.RecipeVersion, defaulting
// to 1 for a record that predates this field being set (every recipe's
// first accepted version).
func recipeVersionOf(rec protocol.KnowledgeRecord) int {
	if rec.RetrievalRecipe != nil && rec.RetrievalRecipe.RecipeVersion != nil {
		return *rec.RetrievalRecipe.RecipeVersion
	}
	return 1
}

// workspaceIDOf returns the first workspace id rec's AppliesTo declares,
// or "" for a globally scoped recipe - ArtifactReference.WorkspaceId is
// required by schema, but a global recipe has no single owning workspace,
// so an empty string (rather than fabricating one) is the honest value.
func workspaceIDOf(rec protocol.KnowledgeRecord) string {
	if rec.RetrievalRecipe == nil || rec.RetrievalRecipe.AppliesTo == nil || len(rec.RetrievalRecipe.AppliesTo.WorkspaceIds) == 0 {
		return ""
	}
	return rec.RetrievalRecipe.AppliesTo.WorkspaceIds[0]
}

// MarshalCanonical renders rec as this plan's canonical recipe text
// format: indented JSON, matching `punakawan knowledge recipe show`'s
// existing rendering (cmd/punakawan/knowledge_cmd.go) rather than
// inventing a second recipe text format. This is deliberately
// line-oriented and stable so internal/artifact.DiffLines' line-based
// LCS diff produces a meaningful, human-readable comparison between two
// versions.
func MarshalCanonical(rec protocol.KnowledgeRecord) ([]byte, error) {
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("recipe: marshal canonical: %w", err)
	}
	return data, nil
}

var _ artifact.Store = (*RecipeStore)(nil)
