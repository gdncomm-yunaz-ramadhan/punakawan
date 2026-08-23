package delivery

import "github.com/ygrip/punakawan/pkg/protocol"

// RouteEvidence is the exact-match signal already resolved for one
// ParentTask before routing is attempted. Resolve never guesses from a
// single fuzzy name: it only matches evidence exactly against the
// project registry, in priority order, and reports ambiguous when
// nothing matches uniquely so the caller can ask one batched
// project-location question instead.
//
// Only RepositoryURL and Slug are implemented against real
// DeliveryProject fields today (there is no separate multi-signal
// ProjectBinding table yet for Jira project keys, Confluence spaces, or
// URL patterns beyond a project's own repository
// url/slug) - those richer binding sources are deferred rather than
// guessed at here.
type RouteEvidence struct {
	RepositoryURL string
	Slug          string
}

// Resolve returns the single project id RouteEvidence matches exactly,
// or ok=false if evidence is empty or matches zero or more than one
// active project.
func Resolve(evidence RouteEvidence, projects []*protocol.DeliveryProject) (projectID string, ok bool) {
	if evidence.RepositoryURL != "" {
		if id, unique := matchOne(projects, func(p *protocol.DeliveryProject) bool {
			return p.RepositoryUrl == evidence.RepositoryURL
		}); unique {
			return id, true
		}
	}
	if evidence.Slug != "" {
		if id, unique := matchOne(projects, func(p *protocol.DeliveryProject) bool {
			return p.Slug == evidence.Slug
		}); unique {
			return id, true
		}
	}
	return "", false
}

func matchOne(projects []*protocol.DeliveryProject, predicate func(*protocol.DeliveryProject) bool) (string, bool) {
	var id string
	count := 0
	for _, p := range projects {
		if p.Status == protocol.DeliveryProjectStatusActive && predicate(p) {
			id = p.Id
			count++
		}
	}
	return id, count == 1
}
