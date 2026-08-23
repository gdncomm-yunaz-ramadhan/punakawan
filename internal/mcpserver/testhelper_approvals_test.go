package mcpserver

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/approvals"
	"github.com/ygrip/punakawan/internal/storage"
)

// newTestApprovalStore opens the shared SQLite storage kernel in a temp dir and
// scopes a standalone approval store to a fixed test project id, for helpers
// that build a Gate directly rather than going through an *app.App.
func newTestApprovalStore(t *testing.T) *approvals.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return approvals.New(db, "test-project")
}
