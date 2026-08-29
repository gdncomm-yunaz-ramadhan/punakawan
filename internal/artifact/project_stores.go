package artifact

import "fmt"

// RootResolver maps a project id to that project's workspace root
// directory on disk. It is the single dependency ProjectStores takes on
// the outside world.
//
// It is injected as a plain func rather than by importing the panel's
// project registry directly, so internal/artifact never depends on
// internal/panel/registry (which would be a layering inversion, and which
// two other concurrent phases own). The integrator supplies a resolver
// closed over the registry - e.g. func(id string) (string, error) {
// rec, err := reg.Lookup(id); return rec.Root, err } - when wiring the
// panel; tests supply a resolver backed by a map of temp dirs.
type RootResolver func(projectID string) (root string, err error)

// ProjectStores resolves per-artifact-type stores for ANY project id,
// not just the single startup workspace the panel's App was loaded for -
// so the review/proposal protocol reaches whichever project a request
// names rather than only the workspace the panel started against.
type ProjectStores struct {
	resolve RootResolver
}

// NewProjectStores builds a ProjectStores over a root resolver. A nil
// resolver is permitted (Reviews then returns a descriptive error rather
// than panicking) so a partially-wired panel degrades gracefully.
func NewProjectStores(resolve RootResolver) *ProjectStores {
	return &ProjectStores{resolve: resolve}
}

// root resolves projectID to its workspace root, or a descriptive error
// when no resolver is configured.
func (p *ProjectStores) root(projectID string) (string, error) {
	if p == nil || p.resolve == nil {
		return "", fmt.Errorf("artifact: no project root resolver configured")
	}
	return p.resolve(projectID)
}

// Reviews returns a ReviewStore rooted at projectID's workspace - the
// store the review/proposal/submit handlers use, resolved per request
// rather than held as one startup-rooted value, so that protocol reaches
// any project id a request names.
func (p *ProjectStores) Reviews(projectID string) (*ReviewStore, error) {
	root, err := p.root(projectID)
	if err != nil {
		return nil, err
	}
	return &ReviewStore{WorkspaceRoot: root}, nil
}
