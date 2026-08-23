package api

import (
	"fmt"
	"net/http"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/recipe"
)

// ProjectArtifactStores resolves the per-project ArtifactStores +
// ReviewStore for a {projectId} through artifact.ProjectStores. It backs
// only the project-scoped Context Improvements inbox (ContextImprovements) -
// the review/proposal mutation protocol it used to also serve was removed
// along with the flat handlers it delegated to.
type ProjectArtifactStores struct {
	projects  *artifact.ProjectStores
	recipes   func() (*recipe.RecipeStore, error)
	knowledge func() (*knowledge.Store, error)
	learning  func() (*learning.Store, error)
}

// NewProjectArtifactStores builds a project-scoped handler set over the
// (already-constructed) artifact.ProjectStores resolver and the shared lazy
// Recipes/Knowledge/Learning factories (may be nil, degrading only the
// features that need them).
func NewProjectArtifactStores(projects *artifact.ProjectStores, recipes func() (*recipe.RecipeStore, error), knowledge func() (*knowledge.Store, error), learning func() (*learning.Store, error)) *ProjectArtifactStores {
	return &ProjectArtifactStores{projects: projects, recipes: recipes, knowledge: knowledge, learning: learning}
}

// forProject resolves the ArtifactStores + ReviewStore rooted at
// projectID's workspace. The Recipes factory is shared verbatim (recipe
// content is not per-project storage - it lives behind the one knowledge
// store the factory opens lazily), while Plans/Reviews are re-rooted per
// request via ProjectStores. An unresolvable project id is the caller's
// mistake and is propagated so the handlers can answer 404.
func (p *ProjectArtifactStores) forProject(projectID string) (ArtifactStores, *artifact.ReviewStore, error) {
	if p == nil || p.projects == nil {
		return ArtifactStores{}, nil, fmt.Errorf("api: no project stores configured")
	}
	plans, err := p.projects.Plans(projectID)
	if err != nil {
		return ArtifactStores{}, nil, err
	}
	reviews, err := p.projects.Reviews(projectID)
	if err != nil {
		return ArtifactStores{}, nil, err
	}
	return ArtifactStores{Plans: plans, Recipes: p.recipes, Root: plans.WorkspaceRoot, Knowledge: p.knowledge, Learning: p.learning}, reviews, nil
}

// resolved is the shared shell for every project-scoped variant: read
// {projectId}, resolve the per-project stores (404 on an unresolvable id),
// then delegate to the flat handler fn builds from those stores.
func (p *ProjectArtifactStores) resolved(fn func(projectID string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("projectId")
		stores, reviews, err := p.forProject(projectID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		fn(projectID, stores, reviews)(w, r)
	}
}

// ContextImprovements serves the project-scoped Context Improvements inbox
// (agent-context plan §8): every learning proposal for the project with its
// live review status.
func (p *ProjectArtifactStores) ContextImprovements() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return ContextImprovementsHandler(stores.Learning, reviews)
	})
}

// AcceptProposal serves the project-scoped accept action a Context
// Improvement's inbox row drives (a learning proposal is itself an
// artifact review) - the mutation that writes a new canonical version into
// the project's own store.
func (p *ProjectArtifactStores) AcceptProposal() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return AcceptProposalHandler(reviews, stores)
	})
}

// RejectProposal serves the project-scoped reject action a Context
// Improvement's inbox row drives.
func (p *ProjectArtifactStores) RejectProposal() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return RejectProposalHandler(reviews)
	})
}

