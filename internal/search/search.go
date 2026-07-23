package search

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// timeZero is used when building an IndexedDocument purely for its
// derived text fields (identifiers/symbols/paths) during scoring, where
// UpdatedAt is irrelevant.
var timeZero = time.Time{}

// Scope is §11.12's KnowledgeSearchRequest.scope: the caller's own current
// location, used only as a ranking signal (§11.10's scope bonus), never as
// a hard filter - a result outside the caller's scope is still returned,
// just ranked lower.
type Scope struct {
	Project    string
	Repository string
	Module     string
	Path       string
}

// Request is §11.12's KnowledgeSearchRequest.
type Request struct {
	Query          string
	Scope          Scope
	Types          []string
	Tags           []string
	IncludeRelated bool
	Limit          int
}

// MatchKind is §11.12's KnowledgeSearchResult.match.kind.
type MatchKind string

const (
	MatchKindIdentifier MatchKind = "identifier"
	MatchKindAlias      MatchKind = "alias"
	MatchKindBM25       MatchKind = "bm25"
	MatchKindFuzzy      MatchKind = "fuzzy"
	MatchKindRelated    MatchKind = "related"
)

// Match is §11.12's KnowledgeSearchResult.match.
type Match struct {
	Kind   MatchKind
	Fields []string
	Terms  []string
}

// Result is §11.12's KnowledgeSearchResult, plus Explanation for §11.13.
type Result struct {
	Id      string
	Title   string
	Summary string
	Type    string
	Score   float64

	Match       Match
	Explanation []string

	Record protocol.KnowledgeRecord
}

const defaultLimit = 20

// bonus values are §11.10's suggested ranking bonuses.
const (
	exactIdentifierBonus = 100.0
	exactAliasBonus      = 40.0
	samePathBonus        = 20.0
	sameModuleBonus      = 12.0
	sameRepositoryBonus  = 8.0
	sameProjectBonus     = 5.0
	directlyRelatedBonus = 8.0
	verifiedTrustBonus   = 5.0

	relationMaxDepth    = 1
	relationMaxItems    = 10
	fuzzyMinTokenLength = 4
)

// Search implements §11.2's pipeline: normalize the query, detect
// structured identifiers, run the BM25F search (falling back to fuzzy
// matching only if that returns nothing), score exact-identifier/alias/
// scope/trust/relation bonuses on top of each hit's BM25 score, expand one
// hop of relations for the top results, then dedupe, rerank, and return
// explainable matches.
func Search(store *knowledge.Store, ix *Index, req Request) ([]Result, error) {
	text := strings.TrimSpace(req.Query)
	if text == "" {
		return nil, nil
	}

	identifiers := DetectIdentifiers(text)

	hits, kind, err := runQuery(ix, text, req)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}

	results := make(map[string]*Result, len(hits))
	for id, hit := range hits {
		rec, err := store.Get(id)
		if err != nil {
			// Stale index entry (record deleted from the canonical store
			// since the last Rebuild/IndexRecord) - skip rather than fail
			// the whole search over one dangling id.
			continue
		}
		results[id] = scoreResult(rec, hit, kind, text, identifiers, req.Scope)
	}

	if req.IncludeRelated {
		expandRelations(store, results)
	}

	return finalize(results, req.Limit), nil
}

// hitInfo is one Bleve hit's score plus which fields/terms actually matched
// (from SearchRequest.IncludeLocations), carried through to Result.Match
// for §11.13's explain-match.
type hitInfo struct {
	score  float64
	fields []string
	terms  []string
}

// runQuery executes §11.2's "exact identifier search -> alias search ->
// BM25F search -> optional fuzzy fallback" stages. Exact-identifier and
// alias matching are not separate Bleve queries: they are scored as bonuses
// in scoreResult against whatever BM25F (or, on an empty BM25F result,
// fuzzy) already surfaced, since a record that mentions a query's exact
// identifier or alias almost always also scores on BM25F terms from that
// same text - a separate identifier/alias-only query pass would be
// redundant rather than additive.
func runQuery(ix *Index, text string, req Request) (map[string]hitInfo, MatchKind, error) {
	bm25Query := buildBM25FQuery(text)
	filtered := applyFilters(bm25Query, req)

	hits, err := runSearchRequest(ix, filtered)
	if err != nil {
		return nil, "", err
	}
	if len(hits) > 0 {
		return hits, MatchKindBM25, nil
	}

	fuzzyQuery := buildFuzzyQuery(text)
	if fuzzyQuery == nil {
		return nil, MatchKindBM25, nil
	}
	hits, err = runSearchRequest(ix, applyFilters(fuzzyQuery, req))
	if err != nil {
		return nil, "", err
	}
	return hits, MatchKindFuzzy, nil
}

func runSearchRequest(ix *Index, q query.Query) (map[string]hitInfo, error) {
	sr := bleve.NewSearchRequestOptions(q, 200, 0, false)
	sr.IncludeLocations = true
	res, err := ix.bleve.Search(sr)
	if err != nil {
		return nil, err
	}
	hits := make(map[string]hitInfo, len(res.Hits))
	for _, hit := range res.Hits {
		fieldSet := map[string]bool{}
		termSet := map[string]bool{}
		for field, terms := range hit.Locations {
			fieldSet[field] = true
			for term := range terms {
				termSet[term] = true
			}
		}
		hits[hit.ID] = hitInfo{score: hit.Score, fields: sortedKeys(fieldSet), terms: sortedKeys(termSet)}
	}
	return hits, nil
}

// buildBM25FQuery combines a per-field MatchQuery for every §11.5-weighted
// field into one DisjunctionQuery, boosted per FieldWeights - Bleve's BM25F
// equivalent, since the mapping itself carries no static field weight.
func buildBM25FQuery(text string) query.Query {
	disjuncts := make([]query.Query, 0, len(FieldWeights))
	for field, weight := range FieldWeights {
		mq := bleve.NewMatchQuery(text)
		mq.SetField(field)
		mq.SetBoost(weight)
		disjuncts = append(disjuncts, mq)
	}
	return bleve.NewDisjunctionQuery(disjuncts...)
}

// buildFuzzyQuery is §11.8's fallback-only fuzzy matching: it only fires
// (via runQuery) when BM25F returns nothing, and only considers tokens long
// enough that a small edit distance is meaningful.
func buildFuzzyQuery(text string) query.Query {
	var disjuncts []query.Query
	for _, tok := range Tokenize(text) {
		if len(tok) < fuzzyMinTokenLength {
			continue
		}
		for _, field := range []string{"title", "summary", "content", "aliases", "tags", "symbols"} {
			fq := bleve.NewFuzzyQuery(tok)
			fq.SetField(field)
			fq.SetFuzziness(2)
			disjuncts = append(disjuncts, fq)
		}
	}
	if len(disjuncts) == 0 {
		return nil
	}
	return bleve.NewDisjunctionQuery(disjuncts...)
}

// applyFilters ANDs req.Types/req.Tags onto q as hard filters (§11.12's
// KnowledgeSearchRequest.types/tags) - unlike Scope, these narrow the
// result set rather than merely ranking it.
func applyFilters(q query.Query, req Request) query.Query {
	musts := []query.Query{q}
	if len(req.Types) > 0 {
		musts = append(musts, termsDisjunction("type", req.Types))
	}
	if len(req.Tags) > 0 {
		musts = append(musts, termsDisjunction("tags", req.Tags))
	}
	if len(musts) == 1 {
		return q
	}
	return bleve.NewConjunctionQuery(musts...)
}

func termsDisjunction(field string, values []string) query.Query {
	disjuncts := make([]query.Query, 0, len(values))
	for _, v := range values {
		tq := bleve.NewTermQuery(v)
		tq.SetField(field)
		disjuncts = append(disjuncts, tq)
	}
	return bleve.NewDisjunctionQuery(disjuncts...)
}

// scoreResult applies §11.10's ranking formula on top of rec's raw BM25F
// (or fuzzy) score.
func scoreResult(rec protocol.KnowledgeRecord, hit hitInfo, kind MatchKind, queryText string, identifiers []Identifier, scope Scope) *Result {
	doc := BuildDocument(rec, timeZero)
	score := hit.score
	var explanation []string

	if id, ok := matchedIdentifier(identifiers, doc); ok {
		score += exactIdentifierBonus
		kind = MatchKindIdentifier
		explanation = append(explanation, fmt.Sprintf("Exact identifier: %q", id))
	}
	if alias, ok := matchedAlias(queryText, rec.Aliases); ok {
		score += exactAliasBonus
		if kind == MatchKindBM25 || kind == MatchKindFuzzy {
			kind = MatchKindAlias
		}
		explanation = append(explanation, fmt.Sprintf("Exact alias: %q", alias))
	}

	switch {
	case scope.Path != "" && scope.Path == doc.Path:
		score += samePathBonus
		explanation = append(explanation, fmt.Sprintf("Same path: %s", scope.Path))
	case scope.Module != "" && scope.Module == doc.Module:
		score += sameModuleBonus
		explanation = append(explanation, fmt.Sprintf("Same module: %s", scope.Module))
	case scope.Repository != "" && scope.Repository == doc.Repository:
		score += sameRepositoryBonus
		explanation = append(explanation, fmt.Sprintf("Same repository: %s", scope.Repository))
	case scope.Project != "" && scope.Project == doc.Project:
		score += sameProjectBonus
		explanation = append(explanation, fmt.Sprintf("Same project: %s", scope.Project))
	}

	if rec.Validity.State == protocol.KnowledgeRecordValidityStateVerified {
		score += verifiedTrustBonus
		explanation = append(explanation, "Verified")
	}

	explanation = append(explanation, fmt.Sprintf("Type: %s", rec.Type))

	return &Result{
		Id:          rec.Id,
		Title:       rec.Title,
		Summary:     doc.Summary,
		Type:        string(rec.Type),
		Score:       score,
		Match:       Match{Kind: kind, Fields: hit.fields, Terms: hit.terms},
		Explanation: explanation,
		Record:      rec,
	}
}

func matchedIdentifier(identifiers []Identifier, doc IndexedDocument) (string, bool) {
	for _, id := range identifiers {
		for _, docID := range doc.Identifiers {
			if id.Value == docID {
				return id.Value, true
			}
		}
		for _, docSym := range doc.Symbols {
			if id.Value == docSym {
				return id.Value, true
			}
		}
	}
	return "", false
}

func matchedAlias(queryText string, aliases []string) (string, bool) {
	q := strings.ToLower(strings.TrimSpace(queryText))
	for _, alias := range aliases {
		if strings.ToLower(alias) == q {
			return alias, true
		}
	}
	return "", false
}

// expandRelations implements §11.9's one-hop relation expansion: for each
// already-matched result, pull every directly-linked record - both its own
// outgoing relations (rec.Relations, already in hand) and any other record
// whose relations point at it (store.Related, the reverse direction) -
// bounded to relationMaxItems combined, and adds them as new candidates if
// not already present. Scored with only the flat relation bonus, since this
// schema's KnowledgeRecordRelation carries no confidence value to compare
// against §11.9's minimumConfidence, so every direct relation qualifies.
func expandRelations(store *knowledge.Store, results map[string]*Result) {
	seedIDs := make([]string, 0, len(results))
	for id := range results {
		seedIDs = append(seedIDs, id)
	}

	for _, seedID := range seedIDs {
		seed := results[seedID]
		candidateIDs := make([]string, 0, len(seed.Record.Relations)+relationMaxItems)
		for _, rel := range seed.Record.Relations {
			candidateIDs = append(candidateIDs, rel.Target)
		}
		reverseRelated, err := store.Related(seedID)
		if err == nil {
			for _, rec := range reverseRelated {
				candidateIDs = append(candidateIDs, rec.Id)
			}
		}

		added := 0
		for _, id := range candidateIDs {
			if added >= relationMaxItems {
				break
			}
			if _, exists := results[id]; exists {
				continue
			}
			rec, err := store.Get(id)
			if err != nil {
				continue
			}
			doc := BuildDocument(rec, timeZero)
			results[rec.Id] = &Result{
				Id:          rec.Id,
				Title:       rec.Title,
				Summary:     doc.Summary,
				Type:        string(rec.Type),
				Score:       directlyRelatedBonus,
				Match:       Match{Kind: MatchKindRelated},
				Explanation: []string{fmt.Sprintf("Directly related to %s", seedID), fmt.Sprintf("Type: %s", rec.Type)},
				Record:      rec,
			}
			added++
		}
	}
	_ = relationMaxDepth // depth is 1 by construction: expandRelations never recurses into the newly-added records.
}

func finalize(results map[string]*Result, limit int) []Result {
	if limit <= 0 {
		limit = defaultLimit
	}

	out := make([]Result, 0, len(results))
	for _, r := range results {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Id < out[j].Id
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
