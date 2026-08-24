package mcpserver

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

func repoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func TestShippedWorkflowsValidateAgainstPublicSurface(t *testing.T) {
	a := newTestApp(t)
	store, err := workflowdef.Open(repoRoot())
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].ID != "feature-delivery" {
		t.Fatalf("shipped workflows = %+v, want only feature-delivery", definitions)
	}
	caps := workflowdef.NewCapabilitySet(CapabilityRegistry(a).Names(), nil)
	for _, definition := range definitions {
		if err := workflowdef.Validate(definition, caps); err != nil {
			t.Errorf("workflow %s: %v", definition.ID, err)
		}
	}
}
