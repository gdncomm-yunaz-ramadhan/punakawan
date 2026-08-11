package profile

import (
	"testing"

	"github.com/ygrip/punakawan/internal/learning"
	"github.com/ygrip/punakawan/internal/project"
)

func TestResolveRepoOwnedWinsOverOverlay(t *testing.T) {
	repo := &RepoProfile{Entries: []Entry{{Key: "git.push_policy", Value: "deny"}}}
	overlay := &project.Project{Metadata: []project.MetadataEntry{
		{Key: "git.push_policy", Value: "allow"},
	}}
	m := Merge{Repo: repo, Overlay: overlay}

	v, source, ok := m.Resolve("git.push_policy")
	if !ok {
		t.Fatalf("Resolve: expected ok=true")
	}
	if v != "deny" {
		t.Fatalf("Resolve: value = %v, want %q (repo-owned must win)", v, "deny")
	}
	if source != "repo-owned" {
		t.Fatalf("Resolve: source = %q, want repo-owned", source)
	}
}

func TestResolveFallsBackToOverlay(t *testing.T) {
	repo := &RepoProfile{}
	overlay := &project.Project{Metadata: []project.MetadataEntry{
		{Key: "build.tool", Value: "bazel"},
	}}
	m := Merge{Repo: repo, Overlay: overlay}

	v, source, ok := m.Resolve("build.tool")
	if !ok || v != "bazel" || source != "global-overlay" {
		t.Fatalf("Resolve = %v, %q, %v; want bazel, global-overlay, true", v, source, ok)
	}
}

func TestResolveNotFoundInEitherLayer(t *testing.T) {
	m := Merge{Repo: &RepoProfile{}, Overlay: &project.Project{}}

	if _, _, ok := m.Resolve("nowhere"); ok {
		t.Fatalf("Resolve: expected ok=false when neither layer has the key")
	}
}

func TestConflictsDetectsRepoOwnedOverridingAcceptedProposal(t *testing.T) {
	repo := &RepoProfile{Entries: []Entry{{Key: "git.push_policy", Value: "deny"}}}
	overlay := &project.Project{Metadata: []project.MetadataEntry{
		{Key: "git.push_policy", Value: "allow"},
	}}
	proposals := []learning.Proposal{
		{
			Id:           "prop-1",
			ArtifactType: learning.TypeMetadata,
			TargetId:     "git.push_policy",
			Status:       learning.StatusAccepted,
		},
	}
	m := Merge{Repo: repo, Overlay: overlay, Proposals: proposals}

	conflicts := m.Conflicts()
	if len(conflicts) != 1 {
		t.Fatalf("Conflicts: got %d, want 1: %+v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.Key != "git.push_policy" || c.ProposalId != "prop-1" {
		t.Fatalf("Conflicts[0] = %+v; unexpected key/proposal id", c)
	}
	if c.RepoOwnedValue != "deny" {
		t.Fatalf("Conflicts[0].RepoOwnedValue = %v, want deny", c.RepoOwnedValue)
	}
	if c.LearnedValue != "allow" {
		t.Fatalf("Conflicts[0].LearnedValue = %v, want allow", c.LearnedValue)
	}
	if c.Reason == "" {
		t.Fatalf("Conflicts[0].Reason: expected a non-empty explanation")
	}
}

func TestConflictsIgnoresPendingProposal(t *testing.T) {
	repo := &RepoProfile{Entries: []Entry{{Key: "git.push_policy", Value: "deny"}}}
	proposals := []learning.Proposal{
		{Id: "prop-1", ArtifactType: learning.TypeMetadata, TargetId: "git.push_policy", Status: learning.StatusPending},
	}
	m := Merge{Repo: repo, Overlay: &project.Project{}, Proposals: proposals}

	if got := m.Conflicts(); len(got) != 0 {
		t.Fatalf("Conflicts: expected none for a pending proposal, got %+v", got)
	}
}

func TestConflictsIgnoresNonMetadataArtifactType(t *testing.T) {
	repo := &RepoProfile{Entries: []Entry{{Key: "git.push_policy", Value: "deny"}}}
	proposals := []learning.Proposal{
		{Id: "prop-1", ArtifactType: learning.TypeWorkflow, TargetId: "git.push_policy", Status: learning.StatusAccepted},
	}
	m := Merge{Repo: repo, Overlay: &project.Project{}, Proposals: proposals}

	if got := m.Conflicts(); len(got) != 0 {
		t.Fatalf("Conflicts: expected none for a non-metadata proposal, got %+v", got)
	}
}

func TestConflictsIgnoresKeyAbsentFromRepoProfile(t *testing.T) {
	repo := &RepoProfile{} // no repo-owned entries at all
	proposals := []learning.Proposal{
		{Id: "prop-1", ArtifactType: learning.TypeMetadata, TargetId: "git.push_policy", Status: learning.StatusAccepted},
	}
	m := Merge{Repo: repo, Overlay: &project.Project{}, Proposals: proposals}

	if got := m.Conflicts(); len(got) != 0 {
		t.Fatalf("Conflicts: expected none when the repo-owned layer has no matching key, got %+v", got)
	}
}

func TestConflictsSortedByKey(t *testing.T) {
	repo := &RepoProfile{Entries: []Entry{
		{Key: "z.key", Value: 1},
		{Key: "a.key", Value: 2},
	}}
	proposals := []learning.Proposal{
		{Id: "prop-z", ArtifactType: learning.TypeMetadata, TargetId: "z.key", Status: learning.StatusAccepted},
		{Id: "prop-a", ArtifactType: learning.TypeMetadata, TargetId: "a.key", Status: learning.StatusAccepted},
	}
	m := Merge{Repo: repo, Overlay: &project.Project{}, Proposals: proposals}

	got := m.Conflicts()
	if len(got) != 2 {
		t.Fatalf("Conflicts: got %d, want 2", len(got))
	}
	if got[0].Key != "a.key" || got[1].Key != "z.key" {
		t.Fatalf("Conflicts: not sorted by key: %+v", got)
	}
}
