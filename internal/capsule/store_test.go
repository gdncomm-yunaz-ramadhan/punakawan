package capsule

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestStorePutGetRoundTrip(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	c := protocol.ContextCapsule{
		Id:               "cap-1",
		TaskId:           "bd-task-1",
		CreatedAt:        time.Now().UTC(),
		Role:             protocol.ContextCapsuleRoleBagong,
		Objective:        "verify the refund flow",
		AllowedTools:     []string{"run_tests"},
		ForbiddenActions: []string{},
	}
	digest, err := Digest(c)
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	c.Digest = digest
	if err := store.Put(c); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := store.Get("cap-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Objective != c.Objective || got.Role != c.Role {
		t.Fatalf("Get = %+v, want %+v", got, c)
	}
}

func TestStoreGetMissingReturnsErrNotFound(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := store.Get("does-not-exist"); err != ErrNotFound {
		t.Fatalf("Get: got err %v, want ErrNotFound", err)
	}
}

func TestStoreGetOnEmptyStoreReturnsErrNotFound(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	if _, err := store.Get("cap-1"); err != ErrNotFound {
		t.Fatalf("Get on never-written store: got err %v, want ErrNotFound", err)
	}
}

func TestListOnEmptyStoreReturnsEmpty(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("List = %+v, want empty", all)
	}
}

func TestListReturnsEveryCapsuleInAppendOrder(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	c1 := protocol.ContextCapsule{Id: "cap-1", TaskId: "bd-task-1", CreatedAt: time.Now().UTC(), Role: protocol.ContextCapsuleRolePetruk, Objective: "implement", AllowedTools: []string{}, ForbiddenActions: []string{}}
	d1, err := Digest(c1)
	if err != nil {
		t.Fatalf("Digest c1: %v", err)
	}
	c1.Digest = d1

	c2 := protocol.ContextCapsule{Id: "cap-2", TaskId: "bd-task-2", CreatedAt: time.Now().UTC(), Role: protocol.ContextCapsuleRoleBagong, Objective: "review", AllowedTools: []string{}, ForbiddenActions: []string{}}
	d2, err := Digest(c2)
	if err != nil {
		t.Fatalf("Digest c2: %v", err)
	}
	c2.Digest = d2

	if err := store.Put(c1); err != nil {
		t.Fatalf("Put c1: %v", err)
	}
	if err := store.Put(c2); err != nil {
		t.Fatalf("Put c2: %v", err)
	}

	all, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 || all[0].Id != "cap-1" || all[1].Id != "cap-2" {
		t.Fatalf("List = %+v, want [cap-1, cap-2] in order", all)
	}
}
