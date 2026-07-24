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

	// relationSeedTopK bounds how many of the highest-scoring hits are used as
	// seeds for one-hop relation expansion, and relationExpandedTotalMax caps
	// the total number of newly-added related candidates across all seeds
	// (punokawan-gi1). Without these, up to a full fetch page of seeds x
	// relationMaxItems each dominates the query with store reads before
	// finalize trims the result down to req.Limit.
	relationSeedTopK         = 20
	relationExpandedTotalMax = 50

	// minFetchCap / fetchCapMultiplier size the Bleve fetch page as a multiple
	// of req.Limit (punokawan-rye) rather than a fixed 200, so a large Limit is
	// not silently truncated before scoring reorders the candidates.
	minFetchCap        = 200
	fetchCapMultiplier = 5
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

	hits, kind, err := runQuery(ix, text, identifiers, req)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}

	// Scoring reads every field it needs (scope, aliases, identifiers,
	// symbols, trust) straight from the fields Bleve stored at index time, so
	// it no longer re-fetches each candidate from the store (punokawan-co7)
	// nor re-runs DetectIdentifiers per hit (punokawan-god). The full
	// protocol.KnowledgeRecord is hydrated later, only for the results that
	// actually survive ranking (see finalize).
	results := make(map[string]*Result, len(hits))
	for id, hit := range hits {
		results[id] = scoreResult(id, hit, kind, text, identifiers, req.Scope)
	}

	if req.IncludeRelated {
		expandRelations(store, results)
	}

	return finalize(store, results, req.Limit), nil
}

// hitInfo is one Bleve hit's score plus which fields/terms actually matched
// (from SearchRequest.IncludeLocations), carried through to Result.Match
// for §11.13's explain-match. doc holds the fields Bleve stored at index time,
// read back so scoring needs neither a per-hit store.Get nor a per-hit
// DetectIdentifiers pass.
type hitInfo struct {
	score  float64
	fields []string
	terms  []string
	doc    storedDoc
}

// storedDoc is the subset of IndexedDocument that scoring consumes, read back
// from a hit's stored Bleve fields. Because DetectIdentifiers already ran at
// index-build time (see BuildDocument), Identifiers/Symbols come back verbatim
// rather than being recomputed from the record text at query time.
type storedDoc struct {
	Title       string
	Summary     string
	Type        string
	Project     string
	Repository  string
	Module      string
	Path        string
	Aliases     []string
	Identifiers []string
	Symbols     []string
	TrustLevel  string
}

// storedFields is the set of stored fields runSearchRequest asks Bleve to
// return on each hit so newStoredDoc can reconstruct a storedDoc.
var storedFields = []string{
	"title", "summary", "type", "project", "repository", "module", "path",
	"aliases", "identifiers", "symbols", "trustLevel",
}

func newStoredDoc(fields map[string]interface{}) storedDoc {
	return storedDoc{
		Title:       stringField(fields, "title"),
		Summary:     stringField(fields, "summary"),
		Type:        stringField(fields, "type"),
		Project:     stringField(fields, "project"),
		Repository:  stringField(fields, "repository"),
		Module:      stringField(fields, "module"),
		Path:        stringField(fields, "path"),
		Aliases:     stringSliceField(fields, "aliases"),
		Identifiers: stringSliceField(fields, "identifiers"),
		Symbols:     stringSliceField(fields, "symbols"),
		TrustLevel:  stringField(fields, "trustLevel"),
	}
}

func stringField(fields map[string]interface{}, key string) string {
	if v, ok := fields[key].(string); ok {
		return v
	}
	return ""
}

// stringSliceField normalizes a stored multi-value Bleve field, which comes
// back as a bare string when it held a single value and as []interface{} when
// it held several.
func stringSliceField(fields map[string]interface{}, key string) []string {
	switch v := fields[key].(type) {
	case string:
		return []string{v}
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// runQuery executes §11.2's "exact identifier search -> alias search ->
// BM25F search -> optional fuzzy fallback" stages. BM25F is the primary pass;
// a cheap identifier/alias term query is unioned on top so a record carrying
// the query's exact identifier still enters the candidate set even when it
// ranks below the BM25F fetch cap (punokawan-rye). Fuzzy matching fires only
// when the combined set is empty.
func runQuery(ix *Index, text string, identifiers []Identifier, req Request) (map[string]hitInfo, MatchKind, error) {
	fetchCap := fetchLimit(req.Limit)

	bm25Query := buildBM25FQuery(text)
	hits, err := runSearchRequest(ix, applyFilters(bm25Query, req), fetchCap)
	if err != nil {
		return nil, "", err
	}

	// Union in any record holding one of the query's exact identifiers/aliases
	// (punokawan-rye). BM25F only surfaces the fetchCap top-scoring hits, so a
	// record that carries the exact identifier but ranks below that cut never
	// enters the candidate set and never receives scoreResult's +identifier /
	// +alias bonus. A cheap term query over the identifiers/symbols/aliases
	// fields pulls those docs in directly; merge keeps any existing BM25 hit's
	// richer score/locations rather than overwriting them.
	if idQuery := buildIdentifierQuery(identifiers); idQuery != nil {
		idHits, err := runSearchRequest(ix, applyFilters(idQuery, req), fetchCap)
		if err != nil {
			return nil, "", err
		}
		mergeHits(hits, idHits)
	}

	if len(hits) > 0 {
		return hits, MatchKindBM25, nil
	}

	fuzzyQuery := buildFuzzyQuery(text)
	if fuzzyQuery == nil {
		return hits, MatchKindBM25, nil
	}
	hits, err = runSearchRequest(ix, applyFilters(fuzzyQuery, req), fetchCap)
	if err != nil {
		return nil, "", err
	}
	return hits, MatchKindFuzzy, nil
}

// fetchLimit sizes the Bleve fetch page as a multiple of the caller's limit,
// floored at minFetchCap so small limits still gather enough candidates for
// scoreResult's bonuses to reorder them meaningfully.
func fetchLimit(limit int) int {
	if limit <= 0 {
		limit = defaultLimit
	}
	c := limit * fetchCapMultiplier
	if c < minFetchCap {
		c = minFetchCap
	}
	return c
}

// mergeHits adds every entry of src to dst that dst does not already hold,
// preserving dst's existing hitInfo (its BM25 score and match locations) on
// collision.
func mergeHits(dst, src map[string]hitInfo) {
	for id, h := range src {
		if _, exists := dst[id]; !exists {
			dst[id] = h
		}
	}
}

func runSearchRequest(ix *Index, q query.Query, fetchCap int) (map[string]hitInfo, error) {
	sr := bleve.NewSearchRequestOptions(q, fetchCap, 0, false)
	sr.IncludeLocations = true
	sr.Fields = storedFields
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
		hits[hit.ID] = hitInfo{
			score:  hit.Score,
			fields: sortedKeys(fieldSet),
			terms:  sortedKeys(termSet),
			doc:    newStoredDoc(hit.Fields),
		}
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

// buildIdentifierQuery builds a disjunction that matches any record carrying
// one of the detected identifiers in its identifiers/symbols/aliases fields
// (punokawan-rye's candidate-recall pass). It is deliberately restricted to
// exactly the fields scoreResult bonuses on, so a hit it surfaces always earns
// its +identifier or +alias boost; broader fields would surface docs that then
// score no differently from a plain BM25 hit. Returns nil when the query
// carried no structured identifiers.
func buildIdentifierQuery(identifiers []Identifier) query.Query {
	if len(identifiers) == 0 {
		return nil
	}
	var disjuncts []query.Query
	for _, id := range identifiers {
		for _, field := range []string{"identifiers", "symbols", "aliases"} {
			mq := bleve.NewMatchQuery(id.Value)
			mq.SetField(field)
			disjuncts = append(disjuncts, mq)
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

// scoreResult applies §11.10's ranking formula on top of the hit's raw BM25F
// (or fuzzy) score, using the fields Bleve stored at index time rather than
// re-fetching or re-deriving anything. Result.Record is left unset here and is
// hydrated later for the surviving results (see finalize).
func scoreResult(id string, hit hitInfo, kind MatchKind, queryText string, identifiers []Identifier, scope Scope) *Result {
	doc := hit.doc
	score := hit.score
	var explanation []string

	if matched, ok := matchedIdentifier(identifiers, doc); ok {
		score += exactIdentifierBonus
		kind = MatchKindIdentifier
		explanation = append(explanation, fmt.Sprintf("Exact identifier: %q", matched))
	}
	if alias, ok := matchedAlias(queryText, doc.Aliases); ok {
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

	if doc.TrustLevel == string(protocol.KnowledgeRecordValidityStateVerified) {
		score += verifiedTrustBonus
		explanation = append(explanation, "Verified")
	}

	explanation = append(explanation, fmt.Sprintf("Type: %s", doc.Type))

	return &Result{
		Id:          id,
		Title:       doc.Title,
		Summary:     doc.Summary,
		Type:        doc.Type,
		Score:       score,
		Match:       Match{Kind: kind, Fields: hit.fields, Terms: hit.terms},
		Explanation: explanation,
	}
}

func matchedIdentifier(identifiers []Identifier, doc storedDoc) (string, bool) {
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
// outgoing relations (rec.Relations) and any other record whose relations
// point at it (store.Related, the reverse direction) - bounded to
// relationMaxItems combined, and adds them as new candidates if not already
// present. Scored with only the flat relation bonus, since this schema's
// KnowledgeRecordRelation carries no confidence value to compare against
// §11.9's minimumConfidence, so every direct relation qualifies.
func expandRelations(store *knowledge.Store, results map[string]*Result) {
	// Expand only the highest-scoring seeds, and cap the total number of
	// newly-added related candidates (punokawan-gi1). Seeds are snapshotted
	// before any addition, so the newly-added related records never themselves
	// become seeds - keeping expansion to a single hop by construction.
	seeds := make([]*Result, 0, len(results))
	for _, r := range results {
		seeds = append(seeds, r)
	}
	sort.Slice(seeds, func(i, j int) bool {
		if seeds[i].Score != seeds[j].Score {
			return seeds[i].Score > seeds[j].Score
		}
		return seeds[i].Id < seeds[j].Id
	})
	if len(seeds) > relationSeedTopK {
		seeds = seeds[:relationSeedTopK]
	}

	total := 0
	for _, seed := range seeds {
		if total >= relationExpandedTotalMax {
			break
		}
		// The seed's own outgoing relations live on its record, which scoring
		// did not fetch; get it now (also hydrating Record so finalize need
		// not re-fetch it). Both this and store.Related are now bounded to the
		// top-K seeds rather than every hit (punokawan-co7).
		seedRec, err := store.Get(seed.Id)
		if err != nil {
			continue
		}
		seed.Record = seedRec

		candidateIDs := make([]string, 0, len(seedRec.Relations)+relationMaxItems)
		for _, rel := range seedRec.Relations {
			candidateIDs = append(candidateIDs, rel.Target)
		}
		if reverseRelated, err := store.Related(seed.Id); err == nil {
			for _, rec := range reverseRelated {
				candidateIDs = append(candidateIDs, rec.Id)
			}
		}

		added := 0
		for _, id := range candidateIDs {
			if added >= relationMaxItems || total >= relationExpandedTotalMax {
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
				Explanation: []string{fmt.Sprintf("Directly related to %s", seed.Id), fmt.Sprintf("Type: %s", rec.Type)},
				Record:      rec,
			}
			added++
			total++
		}
	}
	_ = relationMaxDepth // depth is 1 by construction: expandRelations never recurses into the newly-added records.
}

// finalize ranks the scored candidates, then hydrates the full
// protocol.KnowledgeRecord for the surviving results in rank order, stopping
// once limit valid results are collected. Hydration is bounded to ~limit
// store.Get calls rather than one per candidate (punokawan-co7); a candidate
// whose record has vanished from the store since the last index sync is a
// stale entry and is skipped rather than surfaced.
func finalize(store *knowledge.Store, results map[string]*Result, limit int) []Result {
	if limit <= 0 {
		limit = defaultLimit
	}

	ranked := make([]*Result, 0, len(results))
	for _, r := range results {
		ranked = append(ranked, r)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Id < ranked[j].Id
	})

	out := make([]Result, 0, limit)
	for _, r := range ranked {
		if len(out) >= limit {
			break
		}
		if r.Record.Id == "" {
			rec, err := store.Get(r.Id)
			if err != nil {
				continue
			}
			r.Record = rec
		}
		out = append(out, *r)
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
