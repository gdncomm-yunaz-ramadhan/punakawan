package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewBundleCreatesSkeleton(t *testing.T) {
	root := t.TempDir()

	b, err := NewBundle(root, "run-1", "bd-a8f3")
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}

	wantDir := filepath.Join(root, ".punakawan", "evidence", "run-1", "bd-a8f3")
	if b.Dir != wantDir {
		t.Fatalf("Dir: got %q, want %q", b.Dir, wantDir)
	}

	for _, sub := range []string{"logs", "screenshots", "traces"} {
		info, err := os.Stat(filepath.Join(wantDir, sub))
		if err != nil {
			t.Fatalf("expected %s to exist: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s should be a directory", sub)
		}
	}
}

func TestBundlePath(t *testing.T) {
	b := &Bundle{Dir: "/tmp/example"}
	if got, want := b.Path("task.yaml"), "/tmp/example/task.yaml"; got != want {
		t.Fatalf("Path: got %q, want %q", got, want)
	}
	if got, want := b.Path("logs/build.log"), "/tmp/example/logs/build.log"; got != want {
		t.Fatalf("Path: got %q, want %q", got, want)
	}
}
