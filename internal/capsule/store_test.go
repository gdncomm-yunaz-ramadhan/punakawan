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
