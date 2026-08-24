package mcpserver

import (
	"context"
	"errors"
	"testing"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

func TestSaveWorkflowCreatesAndRevisesDefinition(t *testing.T) {
	a := newTestApp(t)
	handler := saveWorkflowDefinitionHandler(a, toolIndexFrom(CapabilityRegistry(a)))
	definition := workflowdef.Definition{
		ID: "delivery", Name: "Delivery", Enabled: true,
		Roles: map[string]workflowdef.RoleRestriction{"petruk": {Required: true}},
	}
	_, created, err := handler(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	if created.Action != "created" || created.Revision != 1 {
		t.Fatalf("created = %+v", created)
	}
	definition.Revision = created.Revision
	definition.Description = "updated"
	_, updated, err := handler(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Action != "updated" || updated.Revision != 2 {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestSaveWorkflowRejectsStaleRevision(t *testing.T) {
	a := newTestApp(t)
	handler := saveWorkflowDefinitionHandler(a, toolIndexFrom(CapabilityRegistry(a)))
	definition := workflowdef.Definition{ID: "delivery", Name: "Delivery", Enabled: true}
	if _, _, err := handler(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: definition}); err != nil {
		t.Fatal(err)
	}
	_, _, err := handler(context.Background(), nil, SaveWorkflowDefinitionInput{Definition: definition})
	if !errors.Is(err, workflowdef.ErrRevisionConflict) {
		t.Fatalf("error = %v, want revision conflict", err)
	}
}
