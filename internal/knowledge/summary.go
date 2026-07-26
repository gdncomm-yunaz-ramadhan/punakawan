package knowledge

import (
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// maxSummaryLen bounds a rendered summary so a capsule/context stays token-
// bounded even for a record with a long body (agent-context plan §4.5:
// "a useful bounded summary from its actual content").
const maxSummaryLen = 320

// BoundedSummary produces a useful, length-bounded summary of a knowledge
// record from its actual content, replacing the old behavior of reducing every
// record to just its Title (agent-context plan §4.5). Resolution order,
// most-informative first:
//
//  1. the record's own indexed Summary field, if set;
//  2. a type-specific one-line summary drawn from the record's structured
//     payload (a Semar synthesis' goal, a Petruk plan's recommended solution,
//     a review's verdict/honest summary, a recipe's capability/intent, …);
//  3. the free-text Content body;
//  4. the Title, as a last resort.
//
// It returns nil only when the record carries no usable text at all, so a
// caller can omit the field rather than store an empty string.
func BoundedSummary(r protocol.KnowledgeRecord) *string {
	if s := clip(deref(r.Summary)); s != "" {
		return &s
	}
	if s := clip(typedSummary(r)); s != "" {
		return &s
	}
	if s := clip(deref(r.Content)); s != "" {
		return &s
	}
	if s := clip(r.Title); s != "" {
		return &s
	}
	return nil
}

// typedSummary reads whichever payload matches the record's populated pointer
// and renders one bounded line from its most representative field. An empty
// return falls through to Content/Title in BoundedSummary.
func typedSummary(r protocol.KnowledgeRecord) string {
	switch {
	case r.SemarSynthesis != nil:
		return firstNonEmpty(deref(r.SemarSynthesis.Goal), deref(r.SemarSynthesis.Scope), joinFirst(r.SemarSynthesis.KnownFacts))
	case r.PetrukPlan != nil:
		return firstNonEmpty(deref(r.PetrukPlan.RecommendedSolution), joinFirst(r.PetrukPlan.ImplementationSteps))
	case r.FinalPlan != nil:
		return firstNonEmpty(deref(r.FinalPlan.ArchitectureDecision), joinFirst(r.FinalPlan.AcceptanceCriteria))
	case r.BagongReview != nil:
		return firstNonEmpty(deref(r.BagongReview.HonestSummary), deref(r.BagongReview.Verdict))
	case r.GarengReview != nil:
		return firstNonEmpty(deref(r.GarengReview.Verdict), joinFirst(r.GarengReview.Risks))
	case r.ContextDossier != nil:
		return firstNonEmpty(deref(r.ContextDossier.DesiredBehavior), deref(r.ContextDossier.BusinessOrUserValue), deref(r.ContextDossier.CurrentBehavior))
	case r.RetrievalRecipe != nil:
		cap := r.RetrievalRecipe.Capability
		intent := r.RetrievalRecipe.Intent
		if cap == "" && intent == "" {
			return ""
		}
		return strings.TrimSpace("retrieval recipe " + cap + " " + intent)
	default:
		return ""
	}
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// joinFirst returns the first entry of a string slice, used when a payload's
// most representative field is a list (e.g. implementation steps).
func joinFirst(vals []string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// clip trims whitespace and truncates to maxSummaryLen on a rune boundary,
// appending an ellipsis when it cuts.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxSummaryLen {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxSummaryLen {
		return s
	}
	return strings.TrimSpace(string(runes[:maxSummaryLen])) + "…"
}
