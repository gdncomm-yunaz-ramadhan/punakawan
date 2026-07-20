package evidence

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestJournalAppendAndList(t *testing.T) {
	root := t.TempDir()
	journal, err := OpenJournal(root, "run-1")
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}

	events := []protocol.Event{
		{Id: "evt-1", Type: "command", Timestamp: time.Now().UTC(), RunId: "run-1", Operation: "git.status", Result: "success"},
		{Id: "evt-2", Type: "command", Timestamp: time.Now().UTC(), RunId: "run-1", Operation: "git.log", Result: "success"},
	}
	for _, e := range events {
		if err := journal.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := journal.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Id != "evt-1" || got[1].Id != "evt-2" {
		t.Fatalf("unexpected event order: %+v", got)
	}
}

func TestJournalListOnEmpty(t *testing.T) {
	journal, err := OpenJournal(t.TempDir(), "run-empty")
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	events, err := journal.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if events != nil {
		t.Fatalf("expected nil for an empty journal, got %+v", events)
	}
}
