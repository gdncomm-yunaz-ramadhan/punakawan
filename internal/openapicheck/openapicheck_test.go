package openapicheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/evidence"
)

func TestCheckNoChanges(t *testing.T) {
	result, err := Check("testdata/base.yaml", "testdata/base.yaml")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.HasBreakingChanges {
		t.Fatalf("HasBreakingChanges = true comparing a spec to itself, want false; changes: %+v", result.Changes)
	}
	if len(result.BreakingChanges) != 0 {
		t.Fatalf("BreakingChanges = %+v, want none", result.BreakingChanges)
	}
}

func TestCheckCompatibleAddition(t *testing.T) {
	result, err := Check("testdata/base.yaml", "testdata/compatible.yaml")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.HasBreakingChanges {
		t.Fatalf("HasBreakingChanges = true for a new optional response field, want false; breaking: %+v", result.BreakingChanges)
	}
}

func TestCheckRemovedRequiredParamIsBreaking(t *testing.T) {
	result, err := Check("testdata/base.yaml", "testdata/breaking.yaml")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.HasBreakingChanges {
		t.Fatalf("HasBreakingChanges = false after removing a required query param, want true; changes: %+v", result.Changes)
	}

	found := false
	for _, c := range result.BreakingChanges {
		if c.Id == "request-parameter-removed" {
			found = true
			if c.Operation != "GET" {
				t.Errorf("Operation = %q, want GET", c.Operation)
			}
			if c.Path != "/widgets" {
				t.Errorf("Path = %q, want /widgets", c.Path)
			}
			if !c.Breaking {
				t.Errorf("Breaking = false for %s, want true", c.Id)
			}
			if c.Text == "" {
				t.Errorf("Text is empty for %s", c.Id)
			}
		}
	}
	if !found {
		t.Fatalf("expected a request-parameter-removed change, got: %+v", result.Changes)
	}
}

func TestCheckMissingSpec(t *testing.T) {
	if _, err := Check("testdata/does-not-exist.yaml", "testdata/base.yaml"); err == nil {
		t.Fatal("expected an error for a missing base spec")
	}
	if _, err := Check("testdata/base.yaml", "testdata/does-not-exist.yaml"); err == nil {
		t.Fatal("expected an error for a missing head spec")
	}
}

func TestWriteEvidence(t *testing.T) {
	result, err := Check("testdata/base.yaml", "testdata/breaking.yaml")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	bundle, err := evidence.NewBundle(t.TempDir(), "run-1", "task-1")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}

	if err := WriteEvidence(bundle, result); err != nil {
		t.Fatalf("WriteEvidence: %v", err)
	}

	data, err := os.ReadFile(bundle.Path("api-diff.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got Result
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.HasBreakingChanges {
		t.Fatalf("api-diff.json HasBreakingChanges = false, want true")
	}
	if filepath.Base(bundle.Path("api-diff.json")) != "api-diff.json" {
		t.Fatalf("Path(%q) = %q, want basename api-diff.json", "api-diff.json", bundle.Path("api-diff.json"))
	}
}
