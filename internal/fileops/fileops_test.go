package fileops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/policy"
)

func TestWriteFileCreatesNestedFile(t *testing.T) {
	root := t.TempDir()

	if err := WriteFile(policy.Default(), root, "src/pkg/file.go", []byte("package pkg\n")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "src/pkg/file.go"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "package pkg\n" {
		t.Fatalf("content mismatch: got %q", got)
	}
}

func TestWriteFileRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()

	cases := []string{"../outside.txt", "../../etc/passwd", "a/../../outside.txt"}
	for _, relPath := range cases {
		if err := WriteFile(policy.Default(), root, relPath, []byte("x")); err == nil {
			t.Errorf("WriteFile(%q): expected path-traversal rejection, got nil error", relPath)
		}
	}

	// Confirm nothing escaped: no file named "outside.txt" exists next to root.
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected no file to have escaped root, stat err = %v", err)
	}
}

func TestWriteFileRejectsAbsolutePath(t *testing.T) {
	root := t.TempDir()

	if err := WriteFile(policy.Default(), root, "/etc/passwd", []byte("x")); err == nil {
		t.Fatal("WriteFile: expected rejection of an absolute path")
	}
}

func TestWriteFileRespectsPolicyDenyPattern(t *testing.T) {
	root := t.TempDir()
	pol := &policy.Policy{Capabilities: policy.Capabilities{Filesystem: policy.FilesystemPolicy{
		Deny: []string{"secrets/**"},
	}}}

	if err := WriteFile(pol, root, "secrets/token.txt", []byte("x")); err == nil {
		t.Fatal("WriteFile: expected policy to deny writing under secrets/")
	}
	if _, err := os.Stat(filepath.Join(root, "secrets/token.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected denied write to not create a file, stat err = %v", err)
	}

	if err := WriteFile(pol, root, "src/main.go", []byte("x")); err != nil {
		t.Fatalf("WriteFile: expected write outside the deny pattern to succeed: %v", err)
	}
}

func TestBulkCreateFilesReportsPerFileOutcome(t *testing.T) {
	root := t.TempDir()
	pol := &policy.Policy{Capabilities: policy.Capabilities{Filesystem: policy.FilesystemPolicy{
		Deny: []string{"secrets/**"},
	}}}

	results := BulkCreateFiles(pol, root, []FileSpec{
		{Path: "src/a.go", Content: []byte("a")},
		{Path: "secrets/token.txt", Content: []byte("b")},
		{Path: "../escape.txt", Content: []byte("c")},
	})

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].Error != "" {
		t.Errorf("expected src/a.go to succeed, got error %q", results[0].Error)
	}
	if results[1].Error == "" {
		t.Error("expected secrets/token.txt to fail (policy deny)")
	}
	if results[2].Error == "" {
		t.Error("expected ../escape.txt to fail (path traversal)")
	}

	if _, err := os.Stat(filepath.Join(root, "src/a.go")); err != nil {
		t.Fatalf("expected src/a.go to exist: %v", err)
	}
}
