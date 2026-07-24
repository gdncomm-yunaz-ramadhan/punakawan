package artifact

import (
	"fmt"
	"testing"
	"time"
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

func TestProjectStoresPlansResolvesRoot(t *testing.T) {
	root := t.TempDir()
	ps := NewProjectStores(mapResolver(map[string]string{"proj-a": root}))

	plans, err := ps.Plans("proj-a")
	if err != nil {
		t.Fatalf("Plans: %v", err)
	}
	if plans.WorkspaceRoot != root {
		t.Fatalf("WorkspaceRoot = %q, want %q", plans.WorkspaceRoot, root)
	}

	// The resolved store must be rooted at the right place: a version
	// written through it lands under that project's root.
	if _, err := plans.CreateVersion("plan-1", "proj-a", []byte("# hi"), time.Now()); err != nil {
		t.Fatalf("CreateVersion via resolved store: %v", err)
	}
	ids, err := plans.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(ids) != 1 || ids[0] != "plan-1" {
		t.Fatalf("ListPlans = %v, want [plan-1]", ids)
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
	if _, err := ps.Plans("nope"); err == nil {
		t.Fatal("expected an error for an unknown project id")
	}
}

func TestProjectStoresNilResolverErrors(t *testing.T) {
	ps := NewProjectStores(nil)
	if _, err := ps.Plans("anything"); err == nil {
		t.Fatal("expected an error when no resolver is configured")
	}
}
