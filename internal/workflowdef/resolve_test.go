package workflowdef

import (
	"errors"
	"testing"
)

func def(id string, enabled bool, sels ...Selector) Definition {
	return Definition{Version: SchemaVersion, ID: id, Name: id, Enabled: enabled, Selectors: sels, Revision: 1}
}

func TestResolveExplicitID(t *testing.T) {
	defs := []Definition{def("a", true), def("b", false)}
	got, _, err := Resolve(defs, Query{ID: "b"})
	if err != nil || got.ID != "b" {
		t.Fatalf("explicit id ignored enabled state? got %q err %v", got.ID, err)
	}
	if _, _, err := Resolve(defs, Query{ID: "missing"}); !errors.Is(err, ErrNoSelectorMatch) {
		t.Fatalf("missing id: want ErrNoSelectorMatch, got %v", err)
	}
}

func TestResolveExplicitRevision(t *testing.T) {
	defs := []Definition{def("a", true)}
	rev := 1
	if _, _, err := Resolve(defs, Query{ID: "a", Revision: &rev}); err != nil {
		t.Fatalf("matching revision errored: %v", err)
	}
	bad := 2
	if _, _, err := Resolve(defs, Query{ID: "a", Revision: &bad}); !errors.Is(err, ErrRevisionMismatch) {
		t.Fatalf("want ErrRevisionMismatch, got %v", err)
	}
}

func TestResolveSelectorExactMatch(t *testing.T) {
	defs := []Definition{
		def("review", true, Selector{Capability: "github.pull_request", Intent: "review"}),
		def("other", true, Selector{Capability: "github.pull_request", Intent: "merge"}),
	}
	got, _, err := Resolve(defs, Query{Capability: "github.pull_request", Intent: "review"})
	if err != nil || got.ID != "review" {
		t.Fatalf("selector match: got %q err %v", got.ID, err)
	}
	if _, _, err := Resolve(defs, Query{Capability: "github.pull_request", Intent: "close"}); !errors.Is(err, ErrNoSelectorMatch) {
		t.Fatalf("no-match: want ErrNoSelectorMatch, got %v", err)
	}
}

func TestResolveSelectorAmbiguous(t *testing.T) {
	sel := Selector{Capability: "c", Intent: "i"}
	defs := []Definition{def("x", true, sel), def("y", true, sel)}
	_, cands, err := Resolve(defs, Query{Capability: "c", Intent: "i"})
	if !errors.Is(err, ErrAmbiguousSelector) || len(cands) != 2 {
		t.Fatalf("want ambiguous with 2 candidates, got %d err %v", len(cands), err)
	}
}

func TestResolveSelectorSkipsDisabled(t *testing.T) {
	sel := Selector{Capability: "c", Intent: "i"}
	defs := []Definition{def("x", false, sel)}
	if _, _, err := Resolve(defs, Query{Capability: "c", Intent: "i"}); !errors.Is(err, ErrNoSelectorMatch) {
		t.Fatalf("disabled def should not match: %v", err)
	}
}

func TestResolveInputsDefaultsAndMissing(t *testing.T) {
	d := Definition{Inputs: []Input{
		{Name: "repo", Required: true},
		{Name: "depth", Required: false, Default: 3},
	}}
	got, err := ResolveInputs(d, map[string]any{"repo": "x"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got["repo"] != "x" || got["depth"] != 3 {
		t.Fatalf("defaults not applied: %+v", got)
	}
	if _, err := ResolveInputs(d, map[string]any{}); !errors.Is(err, ErrMissingInput) {
		t.Fatalf("missing required input: want ErrMissingInput, got %v", err)
	}
}

func TestContentHashStableAndSensitive(t *testing.T) {
	a := def("a", true)
	if a.ContentHash() != a.ContentHash() {
		t.Fatal("ContentHash not stable for identical content")
	}
	b := a
	b.Name = "changed"
	if a.ContentHash() == b.ContentHash() {
		t.Fatal("ContentHash did not change with content")
	}
}
