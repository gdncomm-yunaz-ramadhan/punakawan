package recipe

import (
	"context"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Dispute marks a recipe disputed (§11's `dispute` command: "prevents
// automatic use"). A disputed recipe is excluded from
// Repository.Search's autoReusableStates until a correction or explicit
// revalidation clears it.
func (r *Repository) Dispute(recipeID, reason string) error {
	return r.MarkState(recipeID, protocol.KnowledgeRecordValidityStateDisputed, reason)
}

// Supersede links recipeID to an already-accepted replacement (§11's
// `supersede` command: "points to an accepted replacement"). Unlike
// CreateVersion, this does not create a new record - it is for pointing
// an old recipe at a *different*, already-existing recipe that replaces
// it (e.g. after two independently discovered recipes turn out to serve
// the same intent), reusing knowledge.Store's own Supersede rather than
// a second linking mechanism.
func (r *Repository) Supersede(recipeID, replacementID string) error {
	return r.Store.Supersede(recipeID, replacementID)
}

// BeginUpdate starts §11's `update` command: "starts guided discovery
// using the current recipe as the baseline." It moves the recipe to
// validating - per §12's reuse table, a validating recipe is never
// auto-reused - so nothing selects it mid-edit, and returns the current
// recipe unchanged for the caller to seed a DiscoverySession's Answers
// from. It does not itself run discovery: this package's discovery loop
// only has Answers as free-form key/value pairs, not a reversible
// mapping back from a compiled selector, so reconstructing "the
// questions that would produce this selector" is left to whatever UI
// (chat, panel) actually talks to the user - an honest gap, not an
// oversight.
func (r *Repository) BeginUpdate(recipeID string) (protocol.KnowledgeRecord, error) {
	rec, err := r.Store.Get(recipeID)
	if err != nil {
		return protocol.KnowledgeRecord{}, fmt.Errorf("recipe: begin update: %w", err)
	}
	if err := r.MarkState(recipeID, protocol.KnowledgeRecordValidityStateValidating, "update started"); err != nil {
		return protocol.KnowledgeRecord{}, err
	}
	rec.Validity.State = protocol.KnowledgeRecordValidityStateValidating
	return rec, nil
}

// CompileOnlyValidate implements §11's `validate` command's compile-time
// half: "recompiles and tests without changing the rule." It runs
// Compiler.Compile (schema/capability/field/resolver validation, §9
// steps 1-5) without a provider dry run - a live JiraSearchClient wiring
// is the same honest gap documented on Validator, and this entry point
// exists specifically for callers (like the CLI) that have no such
// client configured and still want a fast, safe check.
func CompileOnlyValidate(ctx context.Context, c *Compiler, rr *protocol.KnowledgeRecordRetrievalRecipe, bindings map[string]interface{}) (CompiledQuery, error) {
	return c.Compile(ctx, rr, bindings)
}

// StalenessReason enumerates §12's stale triggers this package can
// actually detect on its own (a compile failure, or an explicit
// caller-supplied signal like a rejected query or user-reported bad
// result). Provider-side triggers (project/board/sprint disappearing,
// adapter version changes) require a live provider connection this
// package does not have yet - callers with one are expected to call
// MarkState(..., Stale, ...) directly when they observe those.
type StalenessReason string

const (
	StalenessCompileFailed    StalenessReason = "compile_failed"
	StalenessProviderRejected StalenessReason = "provider_rejected_query"
	StalenessUserReported     StalenessReason = "user_reported_incorrect"
	StalenessRevalidationDue  StalenessReason = "revalidation_period_expired"
)

// Verify moves a recipe to verified and sets verified_by. Store.Put's
// own §7.4 consistency check requires verified_by whenever state is
// verified, so plain MarkState cannot perform this specific transition
// on its own - only Verify, which is explicit about who (or what)
// vouched for it, ever promotes a recipe to verified.
func (r *Repository) Verify(recipeID, verifiedBy, reason string) error {
	rec, err := r.Store.Get(recipeID)
	if err != nil {
		return fmt.Errorf("recipe: verify: %w", err)
	}
	rec.Validity.State = protocol.KnowledgeRecordValidityStateVerified
	rec.Validity.VerifiedBy = []string{verifiedBy}
	// Two callers racing to revalidate the same stale recipe (task q9r.7
	// #5's concurrency question) both reach this same Put; knowledge.Store.Put
	// itself retries the transient Dolt conflict that can produce, rather
	// than letting one caller's otherwise-successful revalidation fail
	// outright.
	if err := r.Store.Put(rec); err != nil {
		return fmt.Errorf("recipe: verify: %w", err)
	}
	return nil
}

// MarkStale is a thin, self-documenting wrapper over MarkState for the
// staleness triggers above - a zero-result query is deliberately not
// one of them (§12: "a valid next sprint may genuinely contain no work
// items").
func (r *Repository) MarkStale(recipeID string, reason StalenessReason) error {
	return r.MarkState(recipeID, protocol.KnowledgeRecordValidityStateStale, string(reason))
}

// RevalidationDue reports whether a verified recipe's configured
// revalidation period has elapsed since it was last accepted (§12's
// "its configured revalidation period expires"). A recipe with no
// accepted_at (never validated) or period<=0 (no configured period) is
// never considered due.
func RevalidationDue(rec protocol.KnowledgeRecord, period time.Duration, now time.Time) bool {
	if period <= 0 || rec.RetrievalRecipe == nil || rec.RetrievalRecipe.Validation == nil || rec.RetrievalRecipe.Validation.AcceptedAt == nil {
		return false
	}
	return now.Sub(*rec.RetrievalRecipe.Validation.AcceptedAt) >= period
}
