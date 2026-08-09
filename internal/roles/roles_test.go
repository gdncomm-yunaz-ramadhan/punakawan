package roles

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/storage"
)

func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return knowledge.New(db, "test-project")
}

func strPtr(s string) *string { return &s }
