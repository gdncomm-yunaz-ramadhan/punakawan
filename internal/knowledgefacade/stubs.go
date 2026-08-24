package knowledgefacade

import (
	"context"
	"errors"
	"fmt"
)

// ErrProviderNotConfigured is returned by every MomProvider/CodepediaProvider
// method: neither has a real backend or deployment config yet. An explicit
// error, not an empty result set, because a stub silently returning "no
// matches" is indistinguishable from a real provider that legitimately
// found nothing - failing loudly beats pretending to work even though these
// providers are more "not yet built" than "misconfigured". A caller fanning
// out across providers (source=all) is expected to catch this per-provider
// and keep going rather than let one stub abort the whole search.
var ErrProviderNotConfigured = errors.New("knowledgefacade: provider has no backend configured")

// MomProvider is the future authority for durable agent memory (decisions,
// learnings, history, constraints). It satisfies KnowledgeSink too, since
// delivery outcomes are expected to eventually feed candidate learnings to
// Mom - but, like Search/Get, Record is wholly inert until a real Mom
// instance exists to point at.
type MomProvider struct{}

var _ KnowledgeProvider = MomProvider{}
var _ KnowledgeSink = MomProvider{}

func (MomProvider) Search(ctx context.Context, req SearchRequest) ([]ProviderResult, error) {
	return nil, fmt.Errorf("knowledgefacade: mom: %w", ErrProviderNotConfigured)
}

func (MomProvider) Get(ctx context.Context, ref string) (Record, error) {
	return Record{}, fmt.Errorf("knowledgefacade: mom: %w", ErrProviderNotConfigured)
}

func (MomProvider) Record(ctx context.Context, candidate Record) (string, error) {
	return "", fmt.Errorf("knowledgefacade: mom: %w", ErrProviderNotConfigured)
}

// CodepediaProvider is the future authority for software topology (repos,
// dependencies, APIs, schemas, environments, blast radius). Read-only by
// design - Codepedia is never a KnowledgeSink - and, like MomProvider,
// wholly inert until a real backend exists.
type CodepediaProvider struct{}

var _ KnowledgeProvider = CodepediaProvider{}

func (CodepediaProvider) Search(ctx context.Context, req SearchRequest) ([]ProviderResult, error) {
	return nil, fmt.Errorf("knowledgefacade: codepedia: %w", ErrProviderNotConfigured)
}

func (CodepediaProvider) Get(ctx context.Context, ref string) (Record, error) {
	return Record{}, fmt.Errorf("knowledgefacade: codepedia: %w", ErrProviderNotConfigured)
}
