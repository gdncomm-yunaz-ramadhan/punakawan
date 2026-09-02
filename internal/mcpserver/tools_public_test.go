package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

func TestListProjectsUsesRepositoryIdentityWithoutRegistryDump(t *testing.T) {
	a := newTestApp(t)
	upsert := upsertProjectHandler(a)

	_, first, err := upsert(context.Background(), nil, UpsertProjectInput{
		Slug: "payments", RepositoryURL: "https://example.test/payments.git", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, updated, err := upsert(context.Background(), nil, UpsertProjectInput{
		Slug: "payments", RepositoryURL: "https://example.test/payments-v2.git", DefaultBranch: "trunk",
	})
	if err != nil {
		t.Fatalf("update project: %v", err)
	}
	if updated.Project.Id != first.Project.Id {
		t.Fatalf("project id changed across upsert: %q -> %q", first.Project.Id, updated.Project.Id)
	}
	if updated.Project.RepositoryUrl != "https://example.test/payments-v2.git" || updated.Project.DefaultBranch == nil || *updated.Project.DefaultBranch != "trunk" {
		t.Fatalf("updated project = %+v", updated.Project)
	}

	_, listed, err := listProjectsHandler(a)(context.Background(), nil, ListProjectsInput{
		RepositoryURL: "git@example.test:payments-v2.git",
	})
	if err != nil {
		t.Fatalf("lookup project by equivalent SSH remote: %v", err)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].Id != first.Project.Id {
		t.Fatalf("projects = %+v, want one stable project", listed.Projects)
	}
	if _, _, err := listProjectsHandler(a)(context.Background(), nil, ListProjectsInput{}); err == nil {
		t.Fatal("unfiltered list_projects unexpectedly succeeded")
	}
}

func TestListProjectsSupportsExplicitMultiProjectSelectionAndBoundedSearch(t *testing.T) {
	a := newTestApp(t)
	upsert := upsertProjectHandler(a)
	for _, slug := range []string{"alpha", "beta", "gamma"} {
		if _, _, err := upsert(context.Background(), nil, UpsertProjectInput{
			Slug: slug, RepositoryURL: "https://example.test/" + slug + ".git",
		}); err != nil {
			t.Fatalf("create %s: %v", slug, err)
		}
	}

	_, selected, err := listProjectsHandler(a)(context.Background(), nil, ListProjectsInput{Slugs: []string{"beta", "alpha", "alpha"}})
	if err != nil {
		t.Fatalf("lookup selected projects: %v", err)
	}
	if len(selected.Projects) != 2 || selected.Projects[0].Slug != "alpha" || selected.Projects[1].Slug != "beta" {
		t.Fatalf("selected projects = %+v, want alpha and beta", selected.Projects)
	}

	_, searched, err := listProjectsHandler(a)(context.Background(), nil, ListProjectsInput{Query: "a", Limit: 2})
	if err != nil {
		t.Fatalf("search projects: %v", err)
	}
	if len(searched.Projects) != 2 {
		t.Fatalf("search projects = %+v, want bounded result", searched.Projects)
	}
	if _, _, err := listProjectsHandler(a)(context.Background(), nil, ListProjectsInput{Query: "a", Limit: maxProjectSearchLimit + 1}); err == nil {
		t.Fatal("oversized project search unexpectedly succeeded")
	}
}

func TestListProjectsRejectsAmbiguousRepositoryIdentity(t *testing.T) {
	a := newTestApp(t)
	upsert := upsertProjectHandler(a)
	for _, project := range []UpsertProjectInput{
		{Slug: "payments-api", RepositoryURL: "https://github.com/acme/payments.git"},
		{Slug: "payments-worker", RepositoryURL: "git@github.com:acme/payments.git"},
	} {
		if _, _, err := upsert(context.Background(), nil, project); err != nil {
			t.Fatalf("create %s: %v", project.Slug, err)
		}
	}

	if _, _, err := listProjectsHandler(a)(context.Background(), nil, ListProjectsInput{
		RepositoryURL: "ssh://git@github.com/acme/payments.git",
	}); err == nil {
		t.Fatal("ambiguous repository identity unexpectedly succeeded")
	}
}

func TestGetAndListWorkflowsReturnSavedDefinitions(t *testing.T) {
	a := newTestApp(t)
	store, err := workflowdef.Open(a.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(workflowdef.Definition{ID: "delivery", Name: "Delivery", Version: workflowdef.SchemaVersion}); err != nil {
		t.Fatal(err)
	}

	_, got, err := getWorkflowHandler(a)(context.Background(), nil, GetWorkflowInput{ID: "delivery"})
	if err != nil {
		t.Fatalf("get workflow: %v", err)
	}
	if got.Workflow.ID != "delivery" || got.Workflow.Revision != 1 {
		t.Fatalf("workflow = %+v", got.Workflow)
	}
	_, listed, err := listWorkflowsHandler(a)(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("list workflows: %v", err)
	}
	if len(listed.Workflows) != 1 || listed.Workflows[0].ID != "delivery" {
		t.Fatalf("workflows = %+v", listed.Workflows)
	}
}
