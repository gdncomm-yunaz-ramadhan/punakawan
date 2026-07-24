package api

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/recipe"
	"github.com/ygrip/punakawan/internal/revision"
)

// ProjectArtifactStores resolves the per-project ArtifactStores +
// ReviewStore for a {projectId} through artifact.ProjectStores, so the
// generic artifact review/proposal protocol can serve ANY registered
// project instead of the single startup workspace the flat handlers are
// pinned to (server.go's primary-rooted PlanStore/ReviewStore).
//
// It is deliberately ADDITIVE and composes the existing flat handlers
// rather than replacing them: every method below is a thin
// resolve-then-delegate shell that reads {projectId} from the path,
// builds the stores rooted at that project's workspace, and invokes the
// SAME exported flat handler with those stores. Nothing in
// artifact_stores.go or the flat handler files changes, so their existing
// behavior (and every existing test) is untouched. The integrator mounts
// BOTH: the flat /api/v1/artifacts/... and /api/v1/reviews/... routes as
// today, plus these variants under /api/v1/projects/{projectId}/... where
// a project id is in scope.
//
// A review created through the project-scoped CreateReview lands in that
// project's .punakawan/reviews and is stamped with the project id as its
// workspace id, so a project A review writes to project A's tree and
// project B is unaffected. Because a review id alone does not carry its
// project, the follow-on review-by-id operations (comment, submit,
// proposal, accept, ...) only resolve to the right tree when mounted
// project-scoped too - hence this type exposes the full set, not just
// create/list.
type ProjectArtifactStores struct {
	projects   *artifact.ProjectStores
	recipes    func() (*recipe.RecipeStore, error)
	dispatcher ProjectDispatcherFunc
	logger     *slog.Logger
}

// ProjectDispatcherFunc yields the revision.Dispatcher rooted at
// projectID's workspace, for the two project-scoped handlers that create
// BD task graphs (Submit, RequestChanges). It is only ever called after
// forProject has already validated projectID, so the factory may assume
// the id resolves (e.g. close over the server's resolveRoot and ignore
// its now-known-nil error). A nil dispatcher degrades only those two
// handlers to a 500, never the rest.
type ProjectDispatcherFunc func(projectID string) revision.Dispatcher

// NewProjectArtifactStores builds a project-scoped handler set over the
// (already-constructed) artifact.ProjectStores resolver, the shared lazy
// Recipes factory (same closure the flat ArtifactStores uses - it may be
// nil, degrading only retrieval_recipe requests), the per-project
// dispatcher factory (may be nil, degrading only Submit/RequestChanges),
// and an optional logger for Fail (nil -> slog.Default()).
func NewProjectArtifactStores(projects *artifact.ProjectStores, recipes func() (*recipe.RecipeStore, error), dispatcher ProjectDispatcherFunc, logger *slog.Logger) *ProjectArtifactStores {
	return &ProjectArtifactStores{projects: projects, recipes: recipes, dispatcher: dispatcher, logger: logger}
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
	return ArtifactStores{Plans: plans, Recipes: p.recipes}, reviews, nil
}

// resolved is the shared shell for every project-scoped variant that does
// not need a dispatcher: read {projectId}, resolve the per-project stores
// (404 on an unresolvable id), then delegate to the flat handler fn builds
// from those stores. projectID is passed through so CreateReview can use
// it as the review's workspace id.
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

// ArtifactCurrent serves the project-scoped GET current-content endpoint.
func (p *ProjectArtifactStores) ArtifactCurrent() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, _ *artifact.ReviewStore) http.HandlerFunc {
		return ArtifactCurrentHandler(stores)
	})
}

// CreateReview serves the project-scoped create-review endpoint. The
// project id doubles as the review's workspace id, so the review is
// attributed to the project it was created under.
func (p *ProjectArtifactStores) CreateReview() http.HandlerFunc {
	return p.resolved(func(projectID string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return CreateReviewHandler(stores, reviews, projectID)
	})
}

// Review serves the project-scoped get-review-by-id endpoint.
func (p *ProjectArtifactStores) Review() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return ReviewHandler(reviews)
	})
}

// UpdateReview serves the project-scoped patch-review endpoint.
func (p *ProjectArtifactStores) UpdateReview() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return UpdateReviewHandler(reviews)
	})
}

// Cancel serves the project-scoped cancel-review endpoint.
func (p *ProjectArtifactStores) Cancel() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return CancelHandler(reviews)
	})
}

// Timeline serves the project-scoped review-timeline endpoint.
func (p *ProjectArtifactStores) Timeline() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return TimelineHandler(reviews)
	})
}

// CreateComment serves the project-scoped create-comment endpoint.
func (p *ProjectArtifactStores) CreateComment() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return CreateCommentHandler(reviews, stores)
	})
}

// Comments serves the project-scoped list-comments endpoint.
func (p *ProjectArtifactStores) Comments() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return CommentsHandler(reviews)
	})
}

// UpdateComment serves the project-scoped patch-comment endpoint.
func (p *ProjectArtifactStores) UpdateComment() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return UpdateCommentHandler(reviews)
	})
}

// DeleteComment serves the project-scoped delete-comment endpoint.
func (p *ProjectArtifactStores) DeleteComment() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return DeleteCommentHandler(reviews)
	})
}

// CreateProposal serves the project-scoped create-proposal endpoint.
func (p *ProjectArtifactStores) CreateProposal() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return CreateProposalHandler(reviews, stores)
	})
}

// ListProposals serves the project-scoped list-proposals endpoint.
func (p *ProjectArtifactStores) ListProposals() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return ListProposalsHandler(reviews)
	})
}

// Proposal serves the project-scoped get-proposal endpoint.
func (p *ProjectArtifactStores) Proposal() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return ProposalHandler(reviews)
	})
}

// ProposalDiff serves the project-scoped proposal-diff endpoint.
func (p *ProjectArtifactStores) ProposalDiff() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return ProposalDiffHandler(reviews, stores)
	})
}

// ProposalValidation serves the project-scoped proposal-validation endpoint.
func (p *ProjectArtifactStores) ProposalValidation() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return ProposalValidationHandler(reviews, stores)
	})
}

// AcceptProposal serves the project-scoped accept-proposal endpoint - the
// mutation that writes a new canonical version into the project's own
// PlanStore.
func (p *ProjectArtifactStores) AcceptProposal() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return AcceptProposalHandler(reviews, stores)
	})
}

// RejectProposal serves the project-scoped reject-proposal endpoint.
func (p *ProjectArtifactStores) RejectProposal() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return RejectProposalHandler(reviews)
	})
}

// Rebase serves the project-scoped rebase-review endpoint.
func (p *ProjectArtifactStores) Rebase() http.HandlerFunc {
	return p.resolved(func(_ string, stores ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return RebaseHandler(reviews, stores)
	})
}

// Fail serves the project-scoped report-failure endpoint. FailHandler
// tolerates a nil logger (falls back to slog.Default()), so a resolver
// built without one still works.
func (p *ProjectArtifactStores) Fail() http.HandlerFunc {
	return p.resolved(func(_ string, _ ArtifactStores, reviews *artifact.ReviewStore) http.HandlerFunc {
		return FailHandler(reviews, p.logger)
	})
}

// Submit serves the project-scoped submit endpoint. Unlike the store-only
// variants it needs a per-project dispatcher: a nil dispatcher factory
// degrades this one endpoint to a 500 rather than affecting the rest.
func (p *ProjectArtifactStores) Submit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("projectId")
		_, reviews, err := p.forProject(projectID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if p.dispatcher == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("api: no dispatcher configured for project-scoped submit"))
			return
		}
		SubmitHandler(reviews, p.dispatcher(projectID))(w, r)
	}
}

// RequestChanges serves the project-scoped request-changes endpoint. Like
// Submit it dispatches a new BD attempt and therefore needs the
// per-project dispatcher factory.
func (p *ProjectArtifactStores) RequestChanges() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("projectId")
		_, reviews, err := p.forProject(projectID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if p.dispatcher == nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("api: no dispatcher configured for project-scoped request-changes"))
			return
		}
		RequestChangesHandler(reviews, p.dispatcher(projectID))(w, r)
	}
}
