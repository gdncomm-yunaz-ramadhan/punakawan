package capsule

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/reconcile"
	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// AdapterGate is the subset of *adapters.Gate's behavior reconciliation
// needs, structurally matching internal/reconcile's own unexported
// gateCaller interface so a live Gate can be passed straight through
// without this package importing internal/adapters. Exported (unlike
// reconcile's private equivalent) so callers can spell it in a
// ReconcileGate func literal's return type.
type AdapterGate interface {
	Call(ctx context.Context, runID, op string, params map[string]any) (json.RawMessage, error)
}

// defaultMaxRetrievedItems bounds automatic selection when RetrievalInput
// gives no TokenBudget, so an unbounded query can't silently fill a capsule
// with hundreds of items.
const defaultMaxRetrievedItems = 30

// estimateTokens is a rough, deliberately simple token-count heuristic
// (~4 characters per token for English prose) - good enough to keep a
// capsule within its budget without pulling in a real tokenizer just for
// sizing. Overestimating slightly is fine; the risk this guards against is
// blowing the budget, not being maximally precise.
func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// RetrievalInput carries what BuildFromRetrieval needs to run Semar's
// knowledge-retrieval-to-capsule pipeline (AEP-M7, punokawan-m2y): search,
// select within a token budget, and build. Everything BuildInput needs
// beyond the retrieved knowledge (requirements, acceptance criteria,
// allowed tools, ...) is still the caller's explicit input - only
// relevant_knowledge is populated automatically here, in addition to
// (not replacing) any KnowledgeIDs the caller already set.
type RetrievalInput struct {
	BuildInput

	// Query is searched via search.Search (§11.2's full pipeline: exact
	// identifiers, BM25F, fuzzy fallback, one-hop relation expansion).
	Query string
	Scope search.Scope
	Types []string

	// ReconcileGate, if set, is called - at most once, and only if some
	// retrieved candidate actually has a reconcilable source (a jira/
	// confluence provider with a stored content_hash) - to resolve the
	// adapter gate reconcile.CheckSourceStale needs. A candidate found
	// stale is excluded rather than handed to a role as trustworthy context
	// (per user decision 2026-07-23, punokawan-m2y's notes). Left nil, no
	// reconciliation happens. The gate is resolved lazily, not eagerly,
	// because *adapters.Registry.Gate spawns the adapter's subprocess on
	// first call - a capsule build with nothing to reconcile should never
	// pay that cost. A reconciliation error is not fatal to the whole
	// capsule build: the candidate is kept, unreconciled, rather than
	// failing every capsule request just because one source (or the
	// adapter itself) is momentarily unreachable.
	ReconcileGate  func() (AdapterGate, error)
	ReconcileRunID string
}

// reconcilableProviders are the only source.provider values
// reconcile.CheckSourceStale currently knows how to re-fetch.
var reconcilableProviders = map[string]bool{"jira": true, "confluence": true}

// needsReconciliation reports whether rec is even eligible for
// reconciliation, without needing a live gate - used to decide whether
// RetrievalInput.ReconcileGate is worth resolving at all.
func needsReconciliation(rec protocol.KnowledgeRecord) bool {
	return reconcilableProviders[rec.Source.Provider] && rec.Source.ContentHash != nil
}

// BuildFromRetrieval runs search.Search for in.Query, filters out results
// this role must not receive (§5's Context Rules) and any Reconcile finds
// stale, greedily selects results in ranked order within in.TokenBudget
// (or defaultMaxRetrievedItems if unset), and calls Build with the
// selection - recording each item's search.Result.Explanation as its
// relevant_knowledge reason.
func BuildFromRetrieval(ctx context.Context, store *knowledge.Store, ix *search.Index, id string, now time.Time, in RetrievalInput) (protocol.ContextCapsule, error) {
	results, err := search.Search(store, ix, search.Request{
		Query:          in.Query,
		Scope:          in.Scope,
		Types:          in.Types,
		IncludeRelated: true,
		Limit:          defaultMaxRetrievedItems,
	})
	if err != nil {
		return protocol.ContextCapsule{}, fmt.Errorf("capsule: retrieve knowledge: %w", err)
	}

	budget := 0
	if in.TokenBudget != nil {
		budget = *in.TokenBudget
	}

	retrievedIDs := make([]string, 0, len(results))
	reasons := make(map[string]string, len(results))
	used := 0
	var gate AdapterGate
	var gateResolved bool
	for _, r := range results {
		if IsForbiddenKnowledgeType(in.Role, protocol.KnowledgeRecordType(r.Type)) {
			continue
		}
		if in.ReconcileGate != nil && needsReconciliation(r.Record) {
			if !gateResolved {
				gate, _ = in.ReconcileGate() // a resolution error just leaves gate nil - see ReconcileGate's doc comment
				gateResolved = true
			}
			if gate != nil && isStale(ctx, store, gate, in.ReconcileRunID, r.Record) {
				continue
			}
		}

		if budget <= 0 && len(retrievedIDs) >= defaultMaxRetrievedItems {
			break
		}
		cost := estimateTokens(r.Title) + estimateTokens(r.Summary)
		if budget > 0 && used+cost > budget {
			// Doesn't fit the remaining budget - skip it, but keep checking
			// lower-ranked (cheaper) results rather than stopping outright.
			continue
		}

		retrievedIDs = append(retrievedIDs, r.Id)
		if len(r.Explanation) > 0 {
			reasons[r.Id] = "Matched because: " + joinExplanation(r.Explanation)
		}
		used += cost
	}

	buildIn := in.BuildInput
	buildIn.KnowledgeIDs = append(append([]string{}, buildIn.KnowledgeIDs...), retrievedIDs...)
	if buildIn.KnowledgeReasons == nil {
		buildIn.KnowledgeReasons = reasons
	} else {
		for id, reason := range reasons {
			buildIn.KnowledgeReasons[id] = reason
		}
	}

	return Build(store, id, now, buildIn)
}

func isStale(ctx context.Context, store *knowledge.Store, gate AdapterGate, runID string, rec protocol.KnowledgeRecord) bool {
	stale, err := reconcile.CheckSourceStale(ctx, store, gate, runID, rec)
	if err != nil {
		return false
	}
	return stale
}

func joinExplanation(explanation []string) string {
	out := explanation[0]
	for _, e := range explanation[1:] {
		out += "; " + e
	}
	return out
}
