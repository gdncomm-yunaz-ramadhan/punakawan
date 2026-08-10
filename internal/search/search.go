package search

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

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
	fuzzyMaxDistance    = 2

	// relationSeedTopK bounds how many of the highest-scoring hits are used as
	// seeds for one-hop relation expansion, and relationExpandedTotalMax caps
	// the total number of newly-added related candidates across all seeds
	// (punokawan-gi1). Without these, up to a full fetch page of seeds x
	// relationMaxItems each dominates the query with store reads before
	// finalize trims the result down to req.Limit.
	relationSeedTopK         = 20
	relationExpandedTotalMax = 50

	// minFetchCap / fetchCapMultiplier size the fetch page as a multiple of
	// req.Limit (punokawan-rye) rather than a fixed 200, so a large Limit is
	// not silently truncated before scoring reorders the candidates.
	minFetchCap        = 200
	fetchCapMultiplier = 5
)

// rowCols is the stored-field projection every query reads back to reconstruct
// a hit's storedDoc (for scoring) and its per-field tokenized content (for
// §11.13 explainability), without a per-hit touch of internal/knowledge.
const rowCols = `s.type, s.project, s.repository, s.module, s.path, s.trust_level, ` +
	`s.title, s.summary, s.content, s.aliases, s.tags, s.paths, s.symbols, s.identifiers`

// bm25Expr is FTS5's built-in weighted BM25 ranking function with §11.5's
// per-field weights, one per knowledge_fts column in declared order. FTS5's
// bm25() is lower-is-better; runQuery negates it so higher wins, matching the
// rest of this codebase's convention.
var bm25Expr = buildBM25Expr()

func buildBM25Expr() string {
	parts := make([]string, len(ftsWeightArgs))
	for i, w := range ftsWeightArgs {
		parts[i] = fmt.Sprintf("%g", w.(float64))
	}
	return "bm25(knowledge_fts, " + strings.Join(parts, ", ") + ")"
}

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
	// symbols, trust) straight from the fields stored at index time, so it no
	// longer re-fetches each candidate from the store (punokawan-co7) nor
	// re-runs DetectIdentifiers per hit (punokawan-god). The full
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

// hitInfo is one hit's score plus which fields/terms actually matched
// (computed in Go from the hit's stored text, since FTS5 exposes no term
// locations), carried through to Result.Match for §11.13's explain-match. doc
// holds the fields stored at index time, read back so scoring needs neither a
// per-hit store.Get nor a per-hit DetectIdentifiers pass.
type hitInfo struct {
	score  float64
	fields []string
	terms  []string
	doc    storedDoc
}

// storedDoc is the subset of the stored row that scoring consumes. Because
// DetectIdentifiers already ran at index-build time (see BuildDocument),
// Identifiers/Symbols come back verbatim rather than being recomputed from the
// record text at query time.
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

// indexedRow is one knowledge_search row's stored fields, holding the raw
// (un-tokenized) text so scoring can exact-match aliases/identifiers and
// explainability can re-tokenize each field independently.
type indexedRow struct {
	typ         string
	project     string
	repository  string
	module      string
	path        string
	trustLevel  string
	title       string
	summary     string
	content     string
	aliases     []string
	tags        []string
	paths       []string
	symbols     []string
	identifiers []string
}

// scanHitRow scans one query row: id, score, then rowCols in declared order.
func scanHitRow(rows *sql.Rows) (id string, score float64, row indexedRow, err error) {
	var aliasesJSON, tagsJSON, pathsJSON, symbolsJSON, identifiersJSON string
	err = rows.Scan(&id, &score,
		&row.typ, &row.project, &row.repository, &row.module, &row.path, &row.trustLevel,
		&row.title, &row.summary, &row.content,
		&aliasesJSON, &tagsJSON, &pathsJSON, &symbolsJSON, &identifiersJSON)
	if err != nil {
		return "", 0, indexedRow{}, err
	}
	row.aliases = parseJSONArray(aliasesJSON)
	row.tags = parseJSONArray(tagsJSON)
	row.paths = parseJSONArray(pathsJSON)
	row.symbols = parseJSONArray(symbolsJSON)
	row.identifiers = parseJSONArray(identifiersJSON)
	return id, score, row, nil
}

func (r indexedRow) stored() storedDoc {
	return storedDoc{
		Title:       r.title,
		Summary:     r.summary,
		Type:        r.typ,
		Project:     r.project,
		Repository:  r.repository,
		Module:      r.module,
		Path:        r.path,
		Aliases:     r.aliases,
		Identifiers: r.identifiers,
		Symbols:     r.symbols,
		TrustLevel:  r.trustLevel,
	}
}

// runQuery executes §11.2's "BM25F search -> optional fuzzy fallback" stages.
// BM25F is the primary pass; a cheap exact identifier/alias/symbol recall query
// is unioned on top so a record carrying the query's exact identifier still
// enters the candidate set even when it ranks below the BM25F fetch cap
// (punokawan-rye). Fuzzy matching fires only when the combined set is empty.
func runQuery(ix *Index, text string, identifiers []Identifier, req Request) (map[string]hitInfo, MatchKind, error) {
	fetchCap := fetchLimit(req.Limit)

	hits, err := executeBM25(ix, text, identifiers, req, fetchCap)
	if err != nil {
		return nil, "", err
	}

	// Union in any record holding one of the query's exact identifiers/aliases
	// (punokawan-rye). The BM25F pass only surfaces the fetchCap top-scoring
	// hits, so a record that carries the exact identifier but ranks below that
	// cut never enters the candidate set and never receives scoreResult's
	// +identifier / +alias bonus. An exact match against the identifiers/
	// symbols/aliases JSON columns pulls those docs in directly; merge keeps any
	// existing BM25 hit's richer score rather than overwriting it.
	if values := buildIdentifierQuery(identifiers); values != nil {
		idHits, err := executeIdentifierRecall(ix, values, text, identifiers, req, fetchCap)
		if err != nil {
			return nil, "", err
		}
		mergeHits(hits, idHits)
	}

	if len(hits) > 0 {
		return hits, MatchKindBM25, nil
	}

	fuzzyHits, ran, err := fuzzyScan(ix, text, identifiers, req)
	if err != nil {
		return nil, "", err
	}
	if !ran {
		return hits, MatchKindBM25, nil
	}
	return fuzzyHits, MatchKindFuzzy, nil
}

// fetchLimit sizes the fetch page as a multiple of the caller's limit, floored
// at minFetchCap so small limits still gather enough candidates for
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
// preserving dst's existing hitInfo (its BM25 score and matched fields/terms)
// on collision.
func mergeHits(dst, src map[string]hitInfo) {
	for id, h := range src {
		if _, exists := dst[id]; !exists {
			dst[id] = h
		}
	}
}

// executeBM25 runs the weighted BM25F pass: a single MATCH over knowledge_fts
// with OR semantics across the tokenized query terms, ranked by the per-field
// bm25() weights. A bare MATCH matches when ANY column contains ANY term, which
// replaces Bleve's per-field boosted DisjunctionQuery; the weighting now lives
// entirely in the bm25() ORDER BY expression.
func executeBM25(ix *Index, text string, identifiers []Identifier, req Request, fetchCap int) (map[string]hitInfo, error) {
	matchExpr, ok := buildMatchExpr(text)
	if !ok {
		return map[string]hitInfo{}, nil
	}

	clauses := []string{"knowledge_fts MATCH ?"}
	args := []interface{}{matchExpr}
	fc, fa := buildFilters(req)
	clauses = append(clauses, fc...)
	args = append(args, fa...)
	args = append(args, fetchCap)

	q := `SELECT s.id, -` + bm25Expr + ` AS score, ` + rowCols + `
		FROM knowledge_fts
		JOIN knowledge_search s ON s.rowid = knowledge_fts.rowid
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY score DESC
		LIMIT ?`

	return queryHits(ix, q, args, text, identifiers)
}

// executeIdentifierRecall implements punokawan-rye's exact-recall pass as a
// plain SQL query over the identifiers/symbols/aliases JSON columns, restricted
// to exactly the fields scoreResult bonuses on, so a hit it surfaces always
// earns its +identifier or +alias boost. These hits carry no BM25 score (0);
// scoreResult adds the bonuses on top.
func executeIdentifierRecall(ix *Index, values []string, text string, identifiers []Identifier, req Request, fetchCap int) (map[string]hitInfo, error) {
	in := placeholders(len(values))
	idClause := "(" +
		"EXISTS (SELECT 1 FROM json_each(s.identifiers) WHERE value IN (" + in + ")) OR " +
		"EXISTS (SELECT 1 FROM json_each(s.symbols) WHERE value IN (" + in + ")) OR " +
		"EXISTS (SELECT 1 FROM json_each(s.aliases) WHERE value IN (" + in + ")))"

	args := make([]interface{}, 0, len(values)*3+len(req.Types)+len(req.Tags)+1)
	for i := 0; i < 3; i++ {
		for _, v := range values {
			args = append(args, v)
		}
	}
	clauses := []string{idClause}
	fc, fa := buildFilters(req)
	clauses = append(clauses, fc...)
	args = append(args, fa...)
	args = append(args, fetchCap)

	q := `SELECT s.id, 0.0 AS score, ` + rowCols + `
		FROM knowledge_search s
		WHERE ` + strings.Join(clauses, " AND ") + `
		LIMIT ?`

	return queryHits(ix, q, args, text, identifiers)
}

// queryHits runs q and turns each row into a hitInfo, computing the matched
// fields/terms in Go from the hit's stored text.
func queryHits(ix *Index, q string, args []interface{}, text string, identifiers []Identifier) (map[string]hitInfo, error) {
	rows, err := ix.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	hits := map[string]hitInfo{}
	for rows.Next() {
		id, score, row, err := scanHitRow(rows)
		if err != nil {
			return nil, err
		}
		fields, terms := matchFieldsTerms(row, text, identifiers)
		hits[id] = hitInfo{score: score, fields: fields, terms: terms, doc: row.stored()}
	}
	return hits, rows.Err()
}

// fuzzyScan is §11.8's fallback-only fuzzy matching, run in Go since FTS5 has
// no edit-distance operator. It fires (via runQuery) only when the BM25F +
// identifier-recall set is empty, and considers only tokens long enough that a
// small edit distance is meaningful. It scans every stored row for this index -
// bounded, since this is a per-project local cache and the fallback was already
// an unindexed correctness-over-speed path - and keeps a row when any qualifying
// query token is within fuzzyMaxDistance of a token in title/summary/content/
// aliases/tags/symbols (the same field set Bleve's fuzzy query used). ran is
// false when the query yielded no fuzzy-eligible tokens, mirroring the old
// "buildFuzzyQuery returned nil" short-circuit.
func fuzzyScan(ix *Index, text string, identifiers []Identifier, req Request) (map[string]hitInfo, bool, error) {
	var queryTokens []string
	for _, tok := range Tokenize(text) {
		if len(tok) >= fuzzyMinTokenLength {
			queryTokens = append(queryTokens, toLowerASCII(tok))
		}
	}
	if len(queryTokens) == 0 {
		return map[string]hitInfo{}, false, nil
	}

	clauses, args := buildFilters(req)
	q := `SELECT s.id, 0.0 AS score, ` + rowCols + ` FROM knowledge_search s`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}

	rows, err := ix.db.Query(q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	hits := map[string]hitInfo{}
	for rows.Next() {
		id, score, row, err := scanHitRow(rows)
		if err != nil {
			return nil, false, err
		}
		if !fuzzyRowMatches(row, queryTokens) {
			continue
		}
		fields, terms := matchFieldsTerms(row, text, identifiers)
		hits[id] = hitInfo{score: score, fields: fields, terms: terms, doc: row.stored()}
	}
	return hits, true, rows.Err()
}

// fuzzyRowMatches reports whether any fuzzy-eligible query token is within
// fuzzyMaxDistance of a token in the row's fuzzy field set.
func fuzzyRowMatches(row indexedRow, queryTokens []string) bool {
	fieldTexts := []string{
		row.title, row.summary, row.content,
		strings.Join(row.aliases, " "), strings.Join(row.tags, " "), strings.Join(row.symbols, " "),
	}
	for _, ft := range fieldTexts {
		for _, docTok := range Tokenize(ft) {
			dt := toLowerASCII(docTok)
			for _, qt := range queryTokens {
				if levenshteinWithin(qt, dt, fuzzyMaxDistance) {
					return true
				}
			}
		}
	}
	return false
}

// buildMatchExpr turns the query into an FTS5 MATCH expression: each §11.6
// token is quoted (so structural characters cannot be read as FTS operators)
// and the tokens are OR-ed, giving the "match if any column holds any term"
// semantics of the old per-field disjunction. ok is false when the query
// tokenizes to nothing.
func buildMatchExpr(text string) (string, bool) {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return "", false
	}
	quoted := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(tok, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " OR "), true
}

// buildIdentifierQuery returns the distinct identifier values to look up in the
// exact-recall pass (punokawan-rye), or nil when the query carried no
// structured identifiers.
func buildIdentifierQuery(identifiers []Identifier) []string {
	if len(identifiers) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var values []string
	for _, id := range identifiers {
		if id.Value == "" || seen[id.Value] {
			continue
		}
		seen[id.Value] = true
		values = append(values, id.Value)
	}
	return values
}

// buildFilters ANDs req.Types/req.Tags onto a query as hard filters (§11.12's
// KnowledgeSearchRequest.types/tags) - unlike Scope, these narrow the candidate
// set before the fetch cap rather than merely ranking it. It returns the SQL
// clauses (against the knowledge_search alias s) and their bound args.
func buildFilters(req Request) ([]string, []interface{}) {
	var clauses []string
	var args []interface{}
	if len(req.Types) > 0 {
		clauses = append(clauses, "s.type IN ("+placeholders(len(req.Types))+")")
		for _, t := range req.Types {
			args = append(args, t)
		}
	}
	if len(req.Tags) > 0 {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM json_each(s.tags) WHERE value IN ("+placeholders(len(req.Tags))+"))")
		for _, t := range req.Tags {
			args = append(args, t)
		}
	}
	return clauses, args
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// matchFieldsTerms computes §11.13's explainability - which fields matched and
// which terms - in Go, since FTS5 exposes no term locations. For each §11.6
// query token and each detected identifier value, it checks case-insensitive
// membership against the tokenized content of each field (the same eight field
// names the Bleve locations reported). This is a reasonable-fidelity stand-in:
// it reports exact token membership, so it cannot reproduce a stemmed/partial
// match Bleve might have surfaced, but the pipeline never asserts on these
// beyond §11.13's display.
func matchFieldsTerms(row indexedRow, text string, identifiers []Identifier) ([]string, []string) {
	fieldTokens := map[string]map[string]bool{
		"title":       lowerTokenSet(row.title),
		"summary":     lowerTokenSet(row.summary),
		"content":     lowerTokenSet(row.content),
		"aliases":     lowerTokenSet(strings.Join(row.aliases, " ")),
		"tags":        lowerTokenSet(strings.Join(row.tags, " ")),
		"paths":       lowerTokenSet(strings.Join(row.paths, " ")),
		"symbols":     lowerTokenSet(strings.Join(row.symbols, " ")),
		"identifiers": lowerTokenSet(strings.Join(row.identifiers, " ")),
	}

	candidates := map[string]bool{}
	for _, tok := range Tokenize(text) {
		candidates[toLowerASCII(tok)] = true
	}
	for _, id := range identifiers {
		candidates[toLowerASCII(id.Value)] = true
	}

	fieldSet := map[string]bool{}
	termSet := map[string]bool{}
	for cand := range candidates {
		for field, toks := range fieldTokens {
			if toks[cand] {
				fieldSet[field] = true
				termSet[cand] = true
			}
		}
	}
	return sortedKeys(fieldSet), sortedKeys(termSet)
}

func lowerTokenSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, tok := range Tokenize(text) {
		set[toLowerASCII(tok)] = true
	}
	return set
}

// scoreResult applies §11.10's ranking formula on top of the hit's raw BM25F
// (or fuzzy) score, using the fields stored at index time rather than
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

// levenshteinWithin reports whether the Levenshtein edit distance between a and
// b is at most max. It uses a rolling two-row DP and an early exit when the row
// minimum exceeds max, so the common no-match case stays cheap.
func levenshteinWithin(a, b string, max int) bool {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d > max || -d > max {
		return false
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > max {
			return false
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)] <= max
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
