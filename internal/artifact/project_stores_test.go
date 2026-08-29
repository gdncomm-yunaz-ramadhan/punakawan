package artifact

import (
	"fmt"
	"testing"
)

// mapResolver builds a RootResolver backed by a fixed id->root map,
// erroring on an unknown id the way a real registry would.
func mapResolver(roots map[string]string) RootResolver {
	return func(projectID string) (string, error) {
		root, ok := roots[projectID]
		if !ok {
			return "", fmt.Errorf("unknown project %q", projectID)
		}
		return root, nil
	}
}

func TestProjectStoresReviewsResolvesRoot(t *testing.T) {
	root := t.TempDir()
	ps := NewProjectStores(mapResolver(map[string]string{"proj-a": root}))

	reviews, err := ps.Reviews("proj-a")
	if err != nil {
		t.Fatalf("Reviews: %v", err)
	}
	if reviews.WorkspaceRoot != root {
		t.Fatalf("WorkspaceRoot = %q, want %q", reviews.WorkspaceRoot, root)
	}
}

func TestProjectStoresUnknownProjectErrors(t *testing.T) {
	ps := NewProjectStores(mapResolver(map[string]string{"proj-a": t.TempDir()}))
	if _, err := ps.Reviews("nope"); err == nil {
		t.Fatal("expected an error for an unknown project id")
	}
}

func TestProjectStoresNilResolverErrors(t *testing.T) {
	ps := NewProjectStores(nil)
	if _, err := ps.Reviews("anything"); err == nil {
		t.Fatal("expected an error when no resolver is configured")
	}
}
