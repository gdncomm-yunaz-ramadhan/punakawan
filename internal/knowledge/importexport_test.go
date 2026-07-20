package knowledge

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestExportImportRoundTrip(t *testing.T) {
	store := newTestStore(t)

	target := validRecord()
	target.Id = "pkw:req/fixture/REQ-target"
	target.Type = protocol.KnowledgeRecordTypeRequirement
	if err := store.Put(target); err != nil {
		t.Fatalf("Put target: %v", err)
	}

	source := validRecord()
	source.Id = "pkw:req/fixture/REQ-source"
	source.Type = protocol.KnowledgeRecordTypeRequirement
	source.Relations = []protocol.KnowledgeRecordRelationsElem{
		{Type: protocol.KnowledgeRecordRelationsElemTypeDependsOn, Target: target.Id},
	}
	if err := store.Put(source); err != nil {
		t.Fatalf("Put source: %v", err)
	}

	other := validRecord()
	other.Id = "pkw:claim/fixture/CLAIM-1"
	other.Type = protocol.KnowledgeRecordTypeClaim
	if err := store.Put(other); err != nil {
		t.Fatalf("Put other: %v", err)
	}

	var buf bytes.Buffer
	if err := store.Export(&buf); err != nil {
		t.Fatalf("Export: %v", err)
	}

	exported := buf.String()
	lines := strings.Split(strings.TrimRight(exported, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 exported lines, got %d: %q", len(lines), exported)
	}

	// Exporting again must byte-for-byte match, proving the ordering is
	// stable and the export is reproducible/diffable.
	var buf2 bytes.Buffer
	if err := store.Export(&buf2); err != nil {
		t.Fatalf("second Export: %v", err)
	}
	if buf.String() != buf2.String() {
		t.Fatalf("export is not stable across repeated runs:\n--- first ---\n%s\n--- second ---\n%s", buf.String(), buf2.String())
	}

	fresh := newTestStore(t)
	if err := fresh.Import(strings.NewReader(exported)); err != nil {
		t.Fatalf("Import: %v", err)
	}

	gotTarget, err := fresh.Get(target.Id)
	if err != nil {
		t.Fatalf("Get target after import: %v", err)
	}
	if gotTarget.Title != target.Title || gotTarget.Type != target.Type {
		t.Fatalf("imported target mismatch: %+v", gotTarget)
	}

	gotSource, err := fresh.Get(source.Id)
	if err != nil {
		t.Fatalf("Get source after import: %v", err)
	}
	if len(gotSource.Relations) != 1 || gotSource.Relations[0].Target != target.Id {
		t.Fatalf("imported source relations mismatch: %+v", gotSource.Relations)
	}

	requirements, err := fresh.ListByType(protocol.KnowledgeRecordTypeRequirement)
	if err != nil {
		t.Fatalf("ListByType requirement: %v", err)
	}
	if len(requirements) != 2 {
		t.Fatalf("expected 2 requirements after import, got %d", len(requirements))
	}

	claims, err := fresh.ListByType(protocol.KnowledgeRecordTypeClaim)
	if err != nil {
		t.Fatalf("ListByType claim: %v", err)
	}
	if len(claims) != 1 || claims[0].Id != other.Id {
		t.Fatalf("expected 1 claim after import, got %+v", claims)
	}

	related, err := fresh.Related(target.Id)
	if err != nil {
		t.Fatalf("Related after import: %v", err)
	}
	if len(related) != 1 || related[0].Id != source.Id {
		t.Fatalf("expected relation index rebuilt via Put, got %+v", related)
	}
}

func TestImportRejectsMalformedLine(t *testing.T) {
	store := newTestStore(t)

	good := validRecord()
	good.Id = "pkw:req/fixture/REQ-good"
	data := mustEncode(t, good)

	input := data + "\n" + "{not valid json\n"
	err := store.Import(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected Import to fail on malformed JSON line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected error to identify line 2, got: %v", err)
	}
	if !strings.Contains(err.Error(), "1 record") {
		t.Fatalf("expected error to report 1 record imported before failure, got: %v", err)
	}

	// The valid line before the bad one should still have been imported.
	if _, err := store.Get(good.Id); err != nil {
		t.Fatalf("expected first valid record to be imported despite later failure: %v", err)
	}
}

func TestImportRejectsProvenanceInvalidLine(t *testing.T) {
	store := newTestStore(t)

	bad := validRecord()
	bad.Id = "pkw:req/fixture/REQ-bad"
	bad.Title = "" // fails provenance Validate via Put
	data := mustEncode(t, bad)

	err := store.Import(strings.NewReader(data + "\n"))
	if err == nil {
		t.Fatal("expected Import to fail on a provenance-invalid record")
	}
	if !strings.Contains(err.Error(), "line 1") || !strings.Contains(err.Error(), bad.Id) {
		t.Fatalf("expected error to identify line 1 and record id %s, got: %v", bad.Id, err)
	}

	if _, err := store.Get(bad.Id); err == nil {
		t.Fatal("expected invalid record not to have been persisted")
	}
}

func mustEncode(t *testing.T, rec protocol.KnowledgeRecord) string {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("encode fixture record: %v", err)
	}
	return string(data)
}
