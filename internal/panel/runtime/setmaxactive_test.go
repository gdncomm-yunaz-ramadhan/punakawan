package runtime

import (
	"context"
	"testing"
)

// TestSetMaxActiveShrinksImmediately verifies that lowering the cap at
// runtime evicts the LRU idle non-primary runtimes right away (closing their
// Apps → releasing their cached project context: SQLite handles, search
// index, adapters), rather than waiting for the next admission.
func TestSetMaxActiveShrinksImmediately(t *testing.T) {
	f := newFakeEnv()
	m := f.manager(WithMaxActive(4)) // primary + up to 3 non-primary
	ctx := context.Background()

	// Admit three non-primary runtimes and release them (idle, evictable).
	for _, id := range []string{"a", "b", "c"} {
		_, rel, err := m.Acquire(ctx, id, "/"+id)
		if err != nil {
			t.Fatal(err)
		}
		f.advance(1) // stagger lastUsedAt so LRU order is deterministic
		rel()
	}
	if got := len(m.pool); got != 4 {
		t.Fatalf("pool = %d, want 4 (primary + a,b,c)", got)
	}

	// Shrink to 2 (primary + 1). Two LRU idle runtimes must be closed now.
	m.SetMaxActive(2)
	if got := m.MaxActive(); got != 2 {
		t.Fatalf("MaxActive = %d, want 2", got)
	}
	if got := len(m.pool); got != 2 {
		t.Fatalf("pool = %d after shrink, want 2", got)
	}
	if got := f.closedCount(); got != 2 {
		t.Fatalf("closed = %d, want 2 (the two LRU idle runtimes)", got)
	}
	// The primary is never evicted.
	if _, ok := m.pool["primary"]; !ok {
		t.Fatal("primary must survive a shrink")
	}
	// SetMaxActive ignores values < 1.
	m.SetMaxActive(0)
	if m.MaxActive() != 2 {
		t.Fatal("SetMaxActive(0) should be ignored")
	}
}
