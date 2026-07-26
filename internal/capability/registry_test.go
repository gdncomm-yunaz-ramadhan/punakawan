package capability

import (
	"reflect"
	"testing"
)

func TestRegistryAddAndQuery(t *testing.T) {
	r := NewRegistry()
	r.Add(Descriptor{Name: "write_file", Source: SourceMCP})
	r.Add(Descriptor{Name: "editJiraIssue", Source: SourceAdapter, Intent: "update"})
	r.Add(Descriptor{Name: ""}) // ignored

	if r.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r.Len())
	}
	if !r.Has("write_file") || !r.Has("editJiraIssue") {
		t.Fatalf("Has missed a registered capability")
	}
	if r.Has("nope") {
		t.Fatalf("Has returned true for an unregistered capability")
	}

	d, ok := r.Lookup("editJiraIssue")
	if !ok || d.Source != SourceAdapter || d.Intent != "update" {
		t.Fatalf("Lookup = %+v, %v; want adapter/update", d, ok)
	}
}

func TestRegistryFirstRegistrationWins(t *testing.T) {
	r := NewRegistry()
	r.Add(Descriptor{Name: "shared", Source: SourceMCP})
	r.Add(Descriptor{Name: "shared", Source: SourceAdapter})
	if d, _ := r.Lookup("shared"); d.Source != SourceMCP {
		t.Fatalf("second Add clobbered source: got %s, want mcp", d.Source)
	}
}

func TestRegistryNamesSorted(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"c", "a", "b"} {
		r.Add(Descriptor{Name: n, Source: SourceMCP})
	}
	if got := r.Names(); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("Names = %v, want sorted", got)
	}
}
