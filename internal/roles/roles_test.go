package roles

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/knowledge"
	"github.com/ygrip/punakawan/internal/tools"
)

func newTestStore(t *testing.T) *knowledge.Store {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed")
	}

	dir := t.TempDir()
	sup := tools.New(dir)
	store, err := knowledge.Open(sup, filepath.Join(dir, "knowledge"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Logf("Close: %v", err)
		}
	})
	return store
}

func strPtr(s string) *string { return &s }
