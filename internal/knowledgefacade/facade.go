// Package knowledgefacade lets a caller search or fetch knowledge without
// knowing which backend answered: Punakawan stops being the source of
// truth for generic knowledge and instead fans out to whichever
// KnowledgeProvider(s) it's asked for, keeping each one's own provenance
// on every result rather than normalizing them into one shape.
//
// LegacyLocalKnowledgeProvider is the only provider with a real backend
// today: it wraps the pre-facade internal/knowledge.Store +
// internal/search.Index pair, so existing callers keep working unchanged
// while new code depends on this facade instead of
// knowledge.Store/search.Index/App.OpenKnowledge directly.
//
// MomProvider and CodepediaProvider are registered extension points, not
// working integrations: there is no deployment config today for where a
// Mom or Codepedia instance lives, and this package must not invent one.
// Both satisfy KnowledgeProvider so search_knowledge's source=mom/source=
// codepedia/source=all wiring already exists once a real backend does;
// until then every method returns ErrProviderNotConfigured rather than
// fabricated data, since a caller should get evidence, not invented
// memory.
package knowledgefacade

import (
	"context"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Record is a knowledge record as any provider returns or accepts it. The
// facade does not reinvent the record shape - every provider, including
// future Mom/Codepedia ones, is expected to speak protocol.KnowledgeRecord.
type Record = protocol.KnowledgeRecord

// Provider source tags. Kept as constants so callers merging fan-out
// results compare against one spelling rather than repeating string
// literals.
const (
	SourceLocal     = "local"
	SourceMom       = "mom"
	SourceCodepedia = "codepedia"
)

// ProviderResultMatch explains why a result matched, for providers that can
// say so; a provider unable to explain a match leaves it zero.
type ProviderResultMatch struct {
	Kind   string
	Fields []string
	Terms  []string
}

// ProviderResult is one provider's search hit. Source and Ref are that
// provider's own identifiers and are never normalized away, even when
// results from several providers are merged into one list.
type ProviderResult struct {
	Source  string
	Ref     string
	Title   string
	Summary string
	Score   float64

	// Record is populated when the provider can hydrate full content in the
	// same call; nil is fine for a provider that can only offer a
	// pointer (e.g. a title/summary/ref triple) without a further fetch.
	Record *Record

	// Match/Explanation carry the local search index's own match
	// explanation, so a LegacyLocalKnowledgeProvider result routed through
	// the facade is indistinguishable from one the pre-facade code path
	// produced. A provider with no equivalent concept leaves these zero.
	Match       ProviderResultMatch
	Explanation []string
}

// SearchRequest is a provider-agnostic search_knowledge request: the query
// plus the same scope/type/tag/limit shape the MCP tool's input already
// carries. ProjectId is ADR-0020's hub project filter (which project's
// local store to search); a provider with no such concept ignores it.
type SearchRequest struct {
	Query string

	Project    string
	Repository string
	Module     string
	Path       string

	Types          []string
	Tags           []string
	IncludeRelated bool
	Limit          int

	ProjectId string
}

// KnowledgeProvider is one source of knowledge - local, Mom, Codepedia, or
// any future one. Kept deliberately narrow: a caller wanting more (writes,
// batch fetch, ...) composes it with KnowledgeSink or its own batching
// rather than this interface growing.
type KnowledgeProvider interface {
	Search(ctx context.Context, req SearchRequest) ([]ProviderResult, error)
	Get(ctx context.Context, ref string) (Record, error)
}

// KnowledgeSink is the optional write side: not every provider accepts
// writes (Codepedia, a read-only topology source, never will), so this is
// a separate interface a provider opts into rather than a required method
// on KnowledgeProvider.
type KnowledgeSink interface {
	// Record persists candidate as a new durable record and returns its
	// reference. Named Record/candidate, not Create: this is evidence
	// offered for the sink to decide what to do with, not an assertion of
	// canonical truth - once Mom exists it is the one deciding whether to
	// store or promote a candidate, not this caller.
	Record(ctx context.Context, candidate Record) (ref string, err error)
}
