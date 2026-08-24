package mcpserver

import (
	"context"
	"testing"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

func TestUpsertAndListProjectsPreserveIdentity(t *testing.T) {
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

	_, listed, err := listProjectsHandler(a)(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(listed.Projects) != 1 || listed.Projects[0].Id != first.Project.Id {
		t.Fatalf("projects = %+v, want one stable project", listed.Projects)
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
