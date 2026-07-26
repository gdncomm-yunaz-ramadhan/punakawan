package workcontext

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/search"
	"github.com/ygrip/punakawan/internal/workflowdef"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// writeProject lays down a project.yaml with the given revision and metadata.
func writeProject(t *testing.T, rev int, meta string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".punakawan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := "version: punakawan.project/v1\nid: proj\nname: Proj\nrevision: " +
		itoa(rev) + "\nmetadata:\n" + meta
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func rec(id string, state protocol.KnowledgeRecordValidityState, summary string) protocol.KnowledgeRecord {
	s := summary
	return protocol.KnowledgeRecord{
		Id:       id,
		Title:    id,
		Summary:  &s,
		Validity: protocol.KnowledgeRecordValidity{State: state},
	}
}

func result(r protocol.KnowledgeRecord, explanation ...string) search.Result {
	return search.Result{Id: r.Id, Title: r.Title, Type: string(r.Type), Explanation: explanation, Record: r}
}

func TestPrepareFiltersUnsafeKnowledge(t *testing.T) {
	root := writeProject(t, 1, "")
	stub := func(search.Request) ([]search.Result, error) {
		return []search.Result{
			result(rec("k-verified", protocol.KnowledgeRecordValidityStateVerified, "v"), "Verified"),
			result(rec("k-observed", protocol.KnowledgeRecordValidityStateObserved, "o")),
			result(rec("k-inferred", protocol.KnowledgeRecordValidityStateInferred, "i")),
			result(rec("k-assumed", protocol.KnowledgeRecordValidityStateAssumed, "a")),
			result(rec("k-disputed", protocol.KnowledgeRecordValidityStateDisputed, "d")),
			result(rec("k-superseded", protocol.KnowledgeRecordValidityStateSuperseded, "s")),
		}, nil
	}
	res, err := Prepare(Request{WorkspaceRoot: root, RetrievalQuery: "q", Now: time.Now()}, stub, nil)
	if err != nil {
		t.Fatal(err)
	}
	accepted := map[string]bool{}
	for _, k := range res.Knowledge {
		accepted[k.Id] = true
	}
	// verified + observed accepted; assumed excluded (not requested); disputed/superseded never.
	if !accepted["k-verified"] || !accepted["k-observed"] {
		t.Fatalf("verified/observed should be accepted: %v", accepted)
	}
	for _, bad := range []string{"k-assumed", "k-disputed", "k-superseded", "k-inferred"} {
		if accepted[bad] {
			t.Fatalf("%s must not be accepted guidance", bad)
		}
	}
	// inferred goes to caution channel.
	if len(res.Caution) != 1 || res.Caution[0].Id != "k-inferred" {
		t.Fatalf("inferred should be caution: %+v", res.Caution)
	}
	// every accepted item carries a reason.
	for _, k := range res.Knowledge {
		if k.Reason == "" {
			t.Fatalf("knowledge item %s has no selection reason", k.Id)
		}
	}
}

func TestPrepareIncludeAssumed(t *testing.T) {
	root := writeProject(t, 1, "")
	stub := func(search.Request) ([]search.Result, error) {
		return []search.Result{result(rec("k-assumed", protocol.KnowledgeRecordValidityStateAssumed, "a"))}, nil
	}
	res, _ := Prepare(Request{WorkspaceRoot: root, RetrievalQuery: "q", IncludeAssumed: true, Now: time.Now()}, stub, nil)
	if len(res.Knowledge) != 1 || res.Knowledge[0].Id != "k-assumed" {
		t.Fatalf("assumed should be accepted when requested: %+v", res.Knowledge)
	}
}

func TestPrepareRequiredMetadata(t *testing.T) {
	// present required key -> metadata item; absent -> missing entry.
	root := writeProject(t, 5, "  - key: test.command\n    description: how to test\n    value: go test ./...\n")
	def := workflowdef.Definition{
		Version: workflowdef.SchemaVersion, ID: "d", Name: "D", Enabled: true, Revision: 1,
		RequiredMetadata: []string{"test.command", "release.owner"},
	}
	res, err := Prepare(Request{
		WorkspaceRoot: root,
		Definitions:   []workflowdef.Definition{def},
		WorkflowID:    "d",
		Now:           time.Now(),
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var haveTestCmd bool
	for _, m := range res.Metadata {
		if m.Key == "test.command" {
			haveTestCmd = true
			if m.Reason != ReasonRequiredByWorkflow {
				t.Fatalf("test.command reason = %q", m.Reason)
			}
		}
	}
	if !haveTestCmd {
		t.Fatal("present required metadata not surfaced")
	}
	if len(res.Missing) != 1 || res.Missing[0].Key != "release.owner" {
		t.Fatalf("absent required metadata should be missing: %+v", res.Missing)
	}
	if res.MetadataRevision != 5 {
		t.Fatalf("metadata revision = %d, want 5", res.MetadataRevision)
	}
}

func TestPrepareDigestDeterministic(t *testing.T) {
	root := writeProject(t, 2, "  - key: k1\n    description: d\n    value: v1\n")
	stub := func(search.Request) ([]search.Result, error) {
		return []search.Result{result(rec("k-verified", protocol.KnowledgeRecordValidityStateVerified, "v"))}, nil
	}
	req := Request{WorkspaceRoot: root, RetrievalQuery: "q", RequestedMetadataKeys: []string{"k1"}, Now: time.Now()}
	a, _ := Prepare(req, stub, nil)
	// Different Now, same inputs+revision => same digest.
	req.Now = req.Now.Add(time.Hour)
	b, _ := Prepare(req, stub, nil)
	if a.Digest == "" || a.Digest != b.Digest {
		t.Fatalf("digest not deterministic: %q vs %q", a.Digest, b.Digest)
	}
	if a.Snapshot.Digest == nil || *a.Snapshot.Digest != a.Digest {
		t.Fatal("snapshot digest mismatch")
	}
}
