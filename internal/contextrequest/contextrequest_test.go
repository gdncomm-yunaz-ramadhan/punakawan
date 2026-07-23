package contextrequest

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestAppendAndGetRoundTrip(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	rec := protocol.MissingContextRequest{
		Id:        "mcr-1",
		CapsuleId: "cap-1",
		Query:     "refund SLA",
		Reason:    "not included in the capsule",
		Blocking:  true,
		Status:    protocol.MissingContextRequestStatusPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.Append(rec); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := store.Get(rec.Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Query != rec.Query || got.Status != protocol.MissingContextRequestStatusPending {
		t.Fatalf("got = %+v, want %+v", got, rec)
	}
}

func TestGetReturnsErrNotFoundForMissingID(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := store.Get("does-not-exist"); err != ErrNotFound {
		t.Fatalf("Get: err = %v, want ErrNotFound", err)
	}
}

func TestCurrentFoldsToLatestResolutionPerID(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	pending := protocol.MissingContextRequest{
		Id: "mcr-1", CapsuleId: "cap-1", Query: "q", Reason: "r", Blocking: false,
		Status: protocol.MissingContextRequestStatusPending, CreatedAt: time.Now().UTC(),
	}
	if err := store.Append(pending); err != nil {
		t.Fatalf("Append pending: %v", err)
	}

	resolved := pending
	resolved.Status = protocol.MissingContextRequestStatusRejected
	note := "not relevant to this task"
	resolved.ResolutionNote = &note
	if err := store.Append(resolved); err != nil {
		t.Fatalf("Append resolved: %v", err)
	}

	current, err := store.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if len(current) != 1 || current["mcr-1"].Status != protocol.MissingContextRequestStatusRejected {
		t.Fatalf("current = %+v, want mcr-1 folded to rejected", current)
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List = %+v, want both the pending and resolved records (full history)", all)
	}
}
