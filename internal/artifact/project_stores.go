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
// not just the single startup workspace the panel's App was loaded for.
// This is the project-scoped resolution plan §9 asks for - "resolve the
// artifact review protocol through the selected project rather than a
// single startup workspace."
//
// It is deliberately a thin, reusable resolver rather than a rewiring of
// the existing review/proposal flow: today the panel builds one PlanStore
// (and one ReviewStore) at startup rooted at the primary workspace and
// hands them to the review/proposal/submit handlers. The integrator can
// feed the SAME resolver into that existing wiring later - calling
// Plans(projectID)/Reviews(projectID) per request instead of holding a
// single startup-rooted store - to make the whole protocol
// project-scoped, without this package having to know anything about the
// registry.
type ProjectStores struct {
	resolve RootResolver
}

// NewProjectStores builds a ProjectStores over a root resolver. A nil
// resolver is permitted (Plans/Reviews then return a descriptive error
// rather than panicking) so a partially-wired panel degrades gracefully.
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

// Plans returns a PlanStore rooted at projectID's workspace. The error
// from an unknown/unresolvable project id is propagated verbatim from the
// injected resolver so the caller can map it to the right HTTP status.
func (p *ProjectStores) Plans(projectID string) (*PlanStore, error) {
	root, err := p.root(projectID)
	if err != nil {
		return nil, err
	}
	return &PlanStore{WorkspaceRoot: root}, nil
}

// Reviews returns a ReviewStore rooted at projectID's workspace. It is
// provided alongside Plans because ReviewStore is likewise a trivial
// {WorkspaceRoot} value - this is the store the integrator will point the
// existing review/proposal/submit handlers at, per this type's doc
// comment, to make that flow project-scoped.
func (p *ProjectStores) Reviews(projectID string) (*ReviewStore, error) {
	root, err := p.root(projectID)
	if err != nil {
		return nil, err
	}
	return &ReviewStore{WorkspaceRoot: root}, nil
}
