package agent

import (
	"testing"
	"testing/fstest"
)

func fakeManifestFS() fstest.MapFS {
	return fstest.MapFS{
		"semar/agent.yaml": &fstest.MapFile{Data: []byte(`
id: semar
name: Semar
version: 1
description: Coordinator
instructions: semar/prompt.md
output_schema: final_plan
tools:
  read_only: false
  allowed: [plan_get, plan_save]
execution:
  can_mutate: true
  requires_evidence: true
  parallel_safe: false
`)},
		"gareng/agent.yaml": &fstest.MapFile{Data: []byte(`
id: gareng
name: Gareng
version: 1
description: Reviewer
instructions: gareng/prompt.md
output_schema: gareng_review
tools:
  read_only: true
  allowed: [plan_get]
execution:
  can_mutate: false
  requires_evidence: true
  parallel_safe: true
`)},
	}
}

func TestLoadSpecsParsesFields(t *testing.T) {
	fsys := fakeManifestFS()
	specs, err := loadSpecs(fsys, []string{"semar/agent.yaml", "gareng/agent.yaml"})
	if err != nil {
		t.Fatalf("loadSpecs: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("len(specs) = %d, want 2", len(specs))
	}

	semar, ok := specs["semar"]
	if !ok {
		t.Fatalf("specs missing %q", "semar")
	}
	if semar.Name != "Semar" {
		t.Errorf("semar.Name = %q, want %q", semar.Name, "Semar")
	}
	if semar.Version != "1" {
		t.Errorf("semar.Version = %q, want %q", semar.Version, "1")
	}
	if semar.Instructions != "semar/prompt.md" {
		t.Errorf("semar.Instructions = %q, want %q", semar.Instructions, "semar/prompt.md")
	}
	if semar.OutputSchemaID != "final_plan" {
		t.Errorf("semar.OutputSchemaID = %q, want %q", semar.OutputSchemaID, "final_plan")
	}
	if semar.ToolPolicy.ReadOnly {
		t.Errorf("semar.ToolPolicy.ReadOnly = true, want false")
	}
	if got, want := len(semar.ToolPolicy.AllowedTools), 2; got != want {
		t.Errorf("len(semar.ToolPolicy.AllowedTools) = %d, want %d", got, want)
	}
	if !semar.ExecutionPolicy.CanMutate {
		t.Errorf("semar.ExecutionPolicy.CanMutate = false, want true")
	}
	if semar.ExecutionPolicy.ParallelSafe {
		t.Errorf("semar.ExecutionPolicy.ParallelSafe = true, want false")
	}

	gareng, ok := specs["gareng"]
	if !ok {
		t.Fatalf("specs missing %q", "gareng")
	}
	if !gareng.ToolPolicy.ReadOnly {
		t.Errorf("gareng.ToolPolicy.ReadOnly = false, want true")
	}
	if gareng.ExecutionPolicy.CanMutate {
		t.Errorf("gareng.ExecutionPolicy.CanMutate = true, want false")
	}
}

func TestLoadSpecsMissingManifest(t *testing.T) {
	fsys := fakeManifestFS()
	if _, err := loadSpecs(fsys, []string{"semar/agent.yaml", "bagong/agent.yaml"}); err == nil {
		t.Fatalf("loadSpecs: got nil error, want error for missing manifest")
	}
}

func TestRegistryListGetReload(t *testing.T) {
	fsys := fakeManifestFS()
	paths := []string{"semar/agent.yaml", "gareng/agent.yaml"}
	r := &registry{fsys: fsys, paths: paths}
	if err := r.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got, want := len(r.List()), 2; got != want {
		t.Fatalf("len(List()) = %d, want %d", got, want)
	}

	spec, err := r.Get("gareng")
	if err != nil {
		t.Fatalf("Get(gareng): %v", err)
	}
	if spec.Name != "Gareng" {
		t.Errorf("Get(gareng).Name = %q, want %q", spec.Name, "Gareng")
	}

	if _, err := r.Get("bagong"); err == nil {
		t.Fatalf("Get(bagong): got nil error, want error for unknown role")
	}

	if err := r.Reload(); err != nil {
		t.Fatalf("second Reload: %v", err)
	}
	if got, want := len(r.List()), 2; got != want {
		t.Fatalf("after Reload, len(List()) = %d, want %d", got, want)
	}
}
