package recipe

import (
	"fmt"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// BenchmarkRepositorySearchAtScale exercises Repository.Search (a full
// ListByType scan followed by in-process filtering) against a few hundred
// recipe records - task q9r.7 #5's "a recipe repository with a realistic
// number of records (100s) doesn't fall over" concern. No
// RecipeStore.lineage() function exists anywhere in this package (the
// task brief's reference to one and its "not a real cost" doc comment
// don't match anything in this codebase); Search is this package's actual
// full-scan-then-filter operation, so it is the meaningful thing to
// benchmark instead. This is a real, measured cost (reported as ns/op by
// `go test -bench`), not a benchmark that runs without measuring
// anything: it times Search scanning ~300 records of which only a
// handful structurally match, mirroring a workspace that has accumulated
// many recipes over time while one caller searches for a single
// capability.
func BenchmarkRepositorySearchAtScale(b *testing.B) {
	store := newTestStore(b)
	repo := &Repository{Store: store}

	const total = 300
	for i := 0; i < total; i++ {
		capability := "confluence.page.search"
		workspace := fmt.Sprintf("workspace-%d", i%20)
		if workspace == "workspace-5" {
			capability = "jira.issue.search"
		}
		f := recipeFixture{
			id:           fmt.Sprintf("pkw:recipe/bench/r-%d", i),
			capability:   capability,
			workspaceIDs: []string{workspace},
			state:        protocol.KnowledgeRecordValidityStateVerified,
		}
		if err := store.Put(f.build()); err != nil {
			b.Fatalf("Put(%d): %v", i, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := repo.Search(Query{Capability: "jira.issue.search", WorkspaceID: "workspace-5"})
		if err != nil {
			b.Fatalf("Search: %v", err)
		}
		if len(got) == 0 {
			b.Fatal("Search returned no candidates, want the jira.issue.search fixtures scoped to workspace-5")
		}
	}
}
