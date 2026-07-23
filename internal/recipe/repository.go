// Package recipe implements punakawan-procedural-knowledge-retrieval-recipe-plan-final.md's
// §15 RecipeRepository/RecipeResolver over internal/knowledge's existing
// Dolt-backed Store. A retrieval recipe is not a separate persistence
// layer: it is a protocol.KnowledgeRecord of type retrieval-recipe with
// its RetrievalRecipe field populated (Phase 0), so this package only
// adds recipe-shaped querying and ranking on top of the store that
// already exists.
//
// # Adding a second read-only provider (task q9r.7 #7)
//
// This phase implements exactly one provider, Jira, so there is no
// separate provider-agnostic Compiler/Resolver/Validator/Executor
// interface today the way §15 stylizes them - Jira is not "the first
// implementation of a generic seam", it is simply what got built first.
// The package-level structure is, however, already naturally seamed along
// provider lines: two-thirds of it (Repository and Resolver, in
// repository.go/resolver.go) has zero Jira-specific code at all, and the
// discovery state machine (discovery.go) and lifecycle commands
// (lifecycle.go) are provider-agnostic in every way except doc-comment
// prose. A second provider (e.g. a read-only Confluence or GitHub search
// recipe) needs to touch only:
//
//  1. JiraSearchClient (validation.go) - a "run this compiled query
//     against the real provider, return rows" interface. A second
//     provider implements the equivalent shape for its own compiled
//     query type (e.g. a ConfluenceSearchClient running CQL).
//  2. JiraAgileClient (compiler.go) - resolves the dynamic
//     provider-specific metadata a selector's `resolver:`-shaped values
//     need (Jira's board/sprint lookups). A provider with no equivalent
//     dynamic-resolution need (most read-only lookups won't) can skip
//     this seam entirely; NewCompiler already treats a nil Agile client
//     as valid.
//  3. Compiler (compiler.go) - the selector-AST-to-provider-query
//     translation (§5's QueryCompiler). A second provider needs its own
//     Compiler-shaped type with its own field/operator allow-list
//     (fieldOperators) and its own escaping function (quoteJQL's
//     equivalent) - selector/resolver/clarification-error plumbing
//     (ClarificationNeededError, ResolverFunc, the any/all selector walk)
//     is reusable as-is only if the second provider's query language is
//     boolean-clause-shaped like JQL; a provider with a fundamentally
//     different query model (e.g. a graph query) would need its own
//     compiler from the selector AST down, not a copy of this one.
//  4. Validator/Executor (validation.go/operation.go) - both are
//     structurally generic pipelines (schema/compile validation, dry run,
//     sample/match-reason reporting, execute-and-record-evidence) that
//     happen to be typed directly to Jira's client interfaces and result
//     shape rather than a generic one. Concretely: ValidationReport.JQL
//     and ExecutorResult.Issues []JiraIssue are Jira-named fields for
//     what is conceptually "the compiled query text" and "the result
//     rows" - a naming leak, not a logic leak (nothing here parses JQL
//     syntax or assumes Jira semantics beyond the field name). A second
//     provider today would need its own Validator/Executor-shaped type
//     with the same pipeline logic and its own JiraIssue-equivalent
//     result row type, copying the ~80 lines of orchestration rather than
//     sharing it generically.
//
// What stays fully generic and shared unchanged by any additional
// provider: protocol.KnowledgeRecordRetrievalRecipe's schema shape
// (selector/validation/last_execution), Repository (storage/scope
// filtering), Resolver (ranking/ambiguity), DiscoverySession (the
// guided-discovery state machine), and the lifecycle commands
// (dispute/supersede/update/staleness/instance-fingerprint checks in
// lifecycle.go and fingerprint.go).
//
// Generalizing bullets 3-4 into truly provider-agnostic interfaces
// (rather than "copy this file, retype the client and result shape") is
// real, non-trivial refactoring - renaming JQL/JiraIssue and extracting a
// provider-parameterized Compiler/Executor would ripple across every file
// in this package plus every test that references them. That is out of
// this hardening phase's scope; see punokawan-q9r.7's filed follow-up gap
// for generalizing the Validator/Executor pipeline (JQL/JiraIssue naming)
// into a provider-parameterized shape once a second provider is actually
// being added, rather than speculatively generalizing it now.
package recipe

import (
	"fmt"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Query narrows Repository.Search to structurally compatible candidates,
// per §6's candidate filtering order steps 1-5 (capability, provider,
// resource, operation, scope). It intentionally has no Intent field:
// intent-based ranking (alias match, relevance) is Resolver's job, not
// Search's - Search only answers "could this recipe possibly apply here,"
// never "is this the best match."
type Query struct {
	Capability   string
	Provider     string
	Resource     string
	Operation    string
	WorkspaceID  string
	RepositoryID string
}

// autoReusableStates are the only validity states Search returns, per
// §12's automatic-reuse table: verified recipes reuse outright, stale
// recipes reuse after revalidation, and every other state (draft,
// validating, disputed, superseded, invalid) requires going through
// discovery or explicit correction first and must never be offered as an
// automatic-resolution candidate. Draft/validating recipes remain
// browsable through the ordinary knowledge list/search paths (Phase 5);
// this allow-list only governs what Resolver is allowed to see.
var autoReusableStates = map[protocol.KnowledgeRecordValidityState]bool{
	protocol.KnowledgeRecordValidityStateVerified: true,
	protocol.KnowledgeRecordValidityStateStale:    true,
}

// Repository implements §15's RecipeRepository interface. The interface
// there names its methods' domain type RetrievalRecipe; here that type is
// simply protocol.KnowledgeRecord (with a non-nil RetrievalRecipe field),
// matching how Phase 0 actually persists recipes rather than introducing
// a parallel domain type that would need its own marshaling to/from the
// record Store.Put/Get already handle.
type Repository struct {
	Store *knowledge.Store
}

// Search returns every retrieval-recipe record compatible with q,
// filtered structurally (capability/provider/resource/operation/scope)
// and restricted to autoReusableStates. It does not rank results.
//
// Scope compatibility: a candidate whose RetrievalRecipe.AppliesTo
// declares specific workspace_ids is excluded unless q.WorkspaceID is one
// of them; a candidate with no AppliesTo (or an AppliesTo with no
// workspace_ids) is treated as globally scoped and always compatible.
// Repository scope is not filtered here (only ranked in Resolver): a
// recipe scoped to a different repository within the same workspace may
// still be a legitimate, if lower-ranked, candidate - unlike workspace
// scope, which the plan's own example treats as a hard boundary between
// unrelated customers/projects, not a preference.
func (r *Repository) Search(q Query) ([]protocol.KnowledgeRecord, error) {
	all, err := r.Store.ListByType(protocol.KnowledgeRecordTypeRetrievalRecipe)
	if err != nil {
		return nil, fmt.Errorf("recipe: search: %w", err)
	}

	out := make([]protocol.KnowledgeRecord, 0, len(all))
	for _, rec := range all {
		if rec.RetrievalRecipe == nil {
			// A retrieval-recipe-typed record with no recipe body is
			// malformed (should never happen through this package's own
			// CreateVersion), but Search degrades by skipping it rather
			// than failing the whole call for every other candidate.
			continue
		}
		if !autoReusableStates[rec.Validity.State] {
			continue
		}
		if q.Capability != "" && rec.RetrievalRecipe.Capability != q.Capability {
			continue
		}
		if q.Provider != "" && rec.RetrievalRecipe.Provider != q.Provider {
			continue
		}
		if q.Resource != "" && rec.RetrievalRecipe.Resource != q.Resource {
			continue
		}
		if q.Operation != "" && rec.RetrievalRecipe.Operation != q.Operation {
			continue
		}
		if q.WorkspaceID != "" && !workspaceCompatible(rec, q.WorkspaceID) {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// workspaceCompatible reports whether rec's declared workspace scope
// (if any) includes workspaceID. An unscoped recipe (no applies_to, or
// applies_to with no workspace_ids) is always compatible.
func workspaceCompatible(rec protocol.KnowledgeRecord, workspaceID string) bool {
	if rec.RetrievalRecipe.AppliesTo == nil || len(rec.RetrievalRecipe.AppliesTo.WorkspaceIds) == 0 {
		return true
	}
	for _, id := range rec.RetrievalRecipe.AppliesTo.WorkspaceIds {
		if id == workspaceID {
			return true
		}
	}
	return false
}

// CreateVersion persists a new recipe version, per §10's immutable
// versioning: it never overwrites an existing verified version in place.
// If previousID is non-empty, the previous record is superseded (its
// superseded_by set, its state moved to superseded) via the same
// Store.Supersede path every other knowledge correction uses - no
// separate versioning mechanism exists. previousID must already exist if
// given; pass "" for a brand-new recipe with no prior version.
func (r *Repository) CreateVersion(rec protocol.KnowledgeRecord, previousID string) (protocol.KnowledgeRecord, error) {
	if rec.RetrievalRecipe == nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("recipe: create version: record %q has no retrieval_recipe body", rec.Id)
	}
	if err := r.Store.Put(rec); err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("recipe: create version: %w", err)
	}
	if previousID != "" {
		if err := r.Store.Supersede(previousID, rec.Id); err != nil {
			return protocol.KnowledgeRecord{}, fmt.Errorf("recipe: supersede previous version %q: %w", previousID, err)
		}
	}
	return rec, nil
}

// MarkState transitions recipeID's validity.state, per §11's explicit
// update/validate/dispute commands (supersede goes through CreateVersion
// above instead, since it involves a replacement record, not just a
// state flip). reason is not yet persisted anywhere structured - Phase 0
// added no state-change-reason field, and events.go's audit log only
// records that a put happened, not why (an honest gap carried over from
// Phase 0/5, not fabricated here) - so it is only logged by the caller's
// own logger, not stored durably. A later phase should add a real
// evidence-linked audit trail if this gap matters in practice.
func (r *Repository) MarkState(recipeID string, state protocol.KnowledgeRecordValidityState, reason string) error {
	rec, err := r.Store.Get(recipeID)
	if err != nil {
		return fmt.Errorf("recipe: mark state: %w", err)
	}
	rec.Validity.State = state
	if err := r.Store.Put(rec); err != nil {
		return fmt.Errorf("recipe: mark state: %w", err)
	}
	return nil
}
