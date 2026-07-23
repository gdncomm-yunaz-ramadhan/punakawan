package recipe

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// OperationRequest is a typed operation request, per §6/§15: what the
// current task needs, not which recipe should answer it.
type OperationRequest struct {
	Capability    string
	Intent        string
	WorkspaceID   string
	RepositoryIDs []string
}

// Outcome is Resolution's disposition, per §7's five candidate-selection
// cases.
type Outcome string

const (
	// OutcomeResolved: one clear verified candidate - reuse automatically.
	OutcomeResolved Outcome = "resolved"
	// OutcomeStale: the best candidate is stale - revalidate before reuse,
	// never execute it as-is.
	OutcomeStale Outcome = "stale"
	// OutcomeAmbiguous: multiple materially different candidates - show
	// scope/conditions/age/match-reason for each, let the caller (or user)
	// choose.
	OutcomeAmbiguous Outcome = "ambiguous"
	// OutcomeNotFound: no valid candidate - enter guided discovery
	// (Phase 3).
	OutcomeNotFound Outcome = "not_found"
)

// Candidate is one scored recipe, with a human-readable explanation of
// how its score was built - §6's "Punakawan can explain each condition"
// extended to explaining why a recipe was (or wasn't) selected.
type Candidate struct {
	Record      protocol.KnowledgeRecord
	Score       int
	Explanation []string
}

// Resolution is Resolver.Resolve's result.
type Resolution struct {
	Outcome Outcome
	// Selected is non-nil only for OutcomeResolved and OutcomeStale.
	Selected *Candidate
	// Candidates lists every scored candidate, highest score first,
	// always populated when at least one structurally compatible recipe
	// existed (including for OutcomeResolved, so a caller can show "why
	// this one and not the runner-up" if asked).
	Candidates []Candidate
}

// Ranking weights, per §6's table. BM25 textual relevance is
// approximated by a simple token-overlap score (relevanceScore below)
// scaled into the same 0-15 range the plan specifies, rather than
// reusing internal/search's Bleve index - that index is built for
// ranking a whole corpus against a query, not for scoring a handful of
// already-known candidates against one intent string, and bolting the
// two together would add indirection this phase doesn't need. A later
// phase can swap this out for genuine BM25 without changing Resolver's
// interface if the heuristic proves too weak in practice.
const (
	weightExactWorkspace  = 100
	weightExactCapability = 100
	weightExactIntent     = 80
	weightExactRepository = 40
	weightVerified        = 30
	weightPriorSuccess    = 20
	weightIntentAlias     = 15
	weightRelevanceMax    = 15
	weightStale           = -30
	// weightGlobalScope is awarded instead of weightExactWorkspace when a
	// recipe declares no workspace scope at all (universally applicable)
	// - compatible, but not as strong a signal as an exact declared match.
	weightGlobalScope = 50
	// ambiguityMargin: candidates scoring within this many points of the
	// top candidate are reported as materially tied rather than picking
	// one arbitrarily. Roughly one alias-match's worth of score, a
	// deliberate threshold choice the plan itself doesn't specify a
	// number for.
	ambiguityMargin = 10
)

// Resolver implements §15's RecipeResolver over a Repository.
type Resolver struct {
	Repo *Repository
}

// Resolve finds and ranks recipes compatible with req, per §6/§7.
func (r *Resolver) Resolve(req OperationRequest) (Resolution, error) {
	candidates, err := r.Repo.Search(Query{Capability: req.Capability, WorkspaceID: req.WorkspaceID})
	if err != nil {
		return Resolution{}, fmt.Errorf("recipe: resolve: %w", err)
	}
	if len(candidates) == 0 {
		return Resolution{Outcome: OutcomeNotFound}, nil
	}

	scored := make([]Candidate, 0, len(candidates))
	for _, rec := range candidates {
		scored = append(scored, score(rec, req))
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })

	top := scored[0]
	if len(scored) == 1 || top.Score-scored[1].Score >= ambiguityMargin {
		if top.Record.Validity.State == protocol.KnowledgeRecordValidityStateStale {
			return Resolution{Outcome: OutcomeStale, Selected: &top, Candidates: scored}, nil
		}
		return Resolution{Outcome: OutcomeResolved, Selected: &top, Candidates: scored}, nil
	}
	return Resolution{Outcome: OutcomeAmbiguous, Candidates: scored}, nil
}

// score computes one candidate's rank and a human-readable explanation
// of every signal that contributed, per §6's ranking table. Capability
// compatibility is not scored here even though the table lists it at
// weight 100: Repository.Search already guarantees every candidate it
// returns matches req.Capability exactly, so awarding it here would just
// add a constant to every score without discriminating between them.
func score(rec protocol.KnowledgeRecord, req OperationRequest) Candidate {
	c := Candidate{Record: rec}
	rr := rec.RetrievalRecipe

	switch {
	case rr.AppliesTo == nil || len(rr.AppliesTo.WorkspaceIds) == 0:
		c.Score += weightGlobalScope
		c.Explanation = append(c.Explanation, fmt.Sprintf("globally scoped (+%d)", weightGlobalScope))
	case contains(rr.AppliesTo.WorkspaceIds, req.WorkspaceID):
		c.Score += weightExactWorkspace
		c.Explanation = append(c.Explanation, fmt.Sprintf("exact workspace scope (+%d)", weightExactWorkspace))
	}

	if req.Intent != "" && rr.Intent == req.Intent {
		c.Score += weightExactIntent
		c.Explanation = append(c.Explanation, fmt.Sprintf("exact intent match (+%d)", weightExactIntent))
	}

	if rr.AppliesTo != nil && repositoryMatch(rr.AppliesTo.RepositoryIds, req.RepositoryIDs) {
		c.Score += weightExactRepository
		c.Explanation = append(c.Explanation, fmt.Sprintf("exact repository scope (+%d)", weightExactRepository))
	}

	if rec.Validity.State == protocol.KnowledgeRecordValidityStateVerified {
		c.Score += weightVerified
		c.Explanation = append(c.Explanation, fmt.Sprintf("verified (+%d)", weightVerified))
	}

	if rr.LastExecution != nil && rr.LastExecution.Status != nil &&
		*rr.LastExecution.Status == protocol.KnowledgeRecordRetrievalRecipeLastExecutionStatusSuccess {
		c.Score += weightPriorSuccess
		c.Explanation = append(c.Explanation, fmt.Sprintf("succeeded before (+%d)", weightPriorSuccess))
	}

	if req.Intent != "" && aliasMatch(rec.Aliases, req.Intent) {
		c.Score += weightIntentAlias
		c.Explanation = append(c.Explanation, fmt.Sprintf("intent alias match (+%d)", weightIntentAlias))
	}

	if rel := relevanceScore(rec, req.Intent); rel > 0 {
		c.Score += rel
		c.Explanation = append(c.Explanation, fmt.Sprintf("textual relevance (+%d)", rel))
	}

	if rec.Validity.State == protocol.KnowledgeRecordValidityStateStale {
		c.Score += weightStale
		c.Explanation = append(c.Explanation, fmt.Sprintf("stale (%d)", weightStale))
	}

	return c
}

func contains(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func repositoryMatch(candidateRepos, requestRepos []string) bool {
	for _, r := range requestRepos {
		if contains(candidateRepos, r) {
			return true
		}
	}
	return false
}

func aliasMatch(aliases []string, intent string) bool {
	intent = strings.ToLower(intent)
	for _, a := range aliases {
		if strings.ToLower(a) == intent {
			return true
		}
	}
	return false
}

// relevanceScore approximates BM25's role (§6) with a token-overlap ratio
// between req's intent text and the candidate's title/aliases/intent,
// scaled to weightRelevanceMax. See the Ranking weights doc comment above
// for why this isn't real BM25.
func relevanceScore(rec protocol.KnowledgeRecord, intent string) int {
	if intent == "" {
		return 0
	}
	queryTokens := tokenSet(intent)
	if len(queryTokens) == 0 {
		return 0
	}

	corpus := rec.Title + " " + rec.RetrievalRecipe.Intent + " " + strings.Join(rec.Aliases, " ")
	corpusTokens := tokenSet(corpus)

	matched := 0
	for t := range queryTokens {
		if corpusTokens[t] {
			matched++
		}
	}
	return matched * weightRelevanceMax / len(queryTokens)
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
}

func tokenSet(s string) map[string]bool {
	set := make(map[string]bool)
	for _, t := range tokenize(s) {
		set[t] = true
	}
	return set
}
