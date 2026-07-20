package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunSuccess(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir)

	res, err := sup.Run(context.Background(), Spec{Name: "printf", Args: []string{"hello"}, Dir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code: got %d, want 0", res.ExitCode)
	}
	if string(res.Stdout) != "hello" {
		t.Fatalf("stdout: got %q, want %q", res.Stdout, "hello")
	}
}

func TestRunDisallowedDir(t *testing.T) {
	allowed := t.TempDir()
	other := t.TempDir()
	sup := New(allowed)

	if _, err := sup.Run(context.Background(), Spec{Name: "printf", Args: []string{"x"}, Dir: other}); err == nil {
		t.Fatal("expected error for a working directory outside the allowlist")
	}
}

func TestRunTimeout(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir)

	start := time.Now()
	_, err := sup.Run(context.Background(), Spec{
		Name: "sleep", Args: []string{"5"}, Dir: dir, Timeout: 150 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Run did not terminate promptly on timeout: took %s", elapsed)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir)

	res, err := sup.Run(context.Background(), Spec{Name: "false", Dir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
}

func TestRunOutputTruncation(t *testing.T) {
	dir := t.TempDir()
	sup := New(dir)
	sup.MaxOutputBytes = 5

	res, err := sup.Run(context.Background(), Spec{Name: "printf", Args: []string{"0123456789"}, Dir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Truncated {
		t.Fatal("expected Truncated to be true")
	}
	if len(res.Stdout) != 5 {
		t.Fatalf("stdout length: got %d, want 5", len(res.Stdout))
	}
}

func TestRunEnvAllowlist(t *testing.T) {
	t.Setenv("PUNAKAWAN_TEST_SECRET", "should-not-leak")

	dir := t.TempDir()
	sup := New(dir)

	res, err := sup.Run(context.Background(), Spec{Name: "env", Dir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := string(res.Stdout)
	if strings.Contains(out, "PUNAKAWAN_TEST_SECRET") {
		t.Fatalf("non-allowlisted env var leaked into child process: %s", out)
	}
	if !strings.Contains(out, "PATH=") {
		t.Fatalf("expected allowlisted PATH to be present in child env: %s", out)
	}
}
