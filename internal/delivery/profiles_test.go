package delivery

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/tools"
	"github.com/ygrip/punakawan/pkg/protocol"
)

func TestSetAndGetDeliveryProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	proj := registerProject(t, s, "profile-project")

	profile, err := s.SetDeliveryProfile(ctx, "set-1", NewID(), proj.Id, ProfileInput{
		BaseBranch:           "main",
		BuildCommand:         "make build",
		TestCommand:          "make test",
		RequiredExecutables:  []string{"git"},
		MaxConcurrentWorkers: 2,
	})
	if err != nil {
		t.Fatalf("SetDeliveryProfile: %v", err)
	}
	if profile.BaseBranch != "main" || profile.MaxConcurrentWorkers == nil || *profile.MaxConcurrentWorkers != 2 {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	updated, err := s.SetDeliveryProfile(ctx, "set-2", profile.Id, proj.Id, ProfileInput{BaseBranch: "develop"})
	if err != nil {
		t.Fatalf("SetDeliveryProfile (update): %v", err)
	}
	if updated.BaseBranch != "develop" || updated.Revision <= profile.Revision {
		t.Fatalf("expected update to change base_branch and advance revision: %+v", updated)
	}

	if _, err := s.SetDeliveryProfile(ctx, "set-3", NewID(), "does-not-exist", ProfileInput{BaseBranch: "main"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound setting profile for unknown project, got %v", err)
	}
}

func TestRunPreflightReportsRealChecksHonestly(t *testing.T) {
	dir := t.TempDir()
	sup := tools.New(dir)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.test", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", ".")
	run("commit", "-m", "init")

	profile := &protocol.ProjectDeliveryProfile{
		BaseBranch:          "main",
		LocalPath:           &dir,
		RequiredExecutables: []string{"git"},
		RequiredServices:    []string{"some-service"},
	}
	checks := RunPreflight(context.Background(), profile, gitops.NewInspector(sup))

	byName := map[string]protocol.PreflightCheck{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if c, ok := byName["executable:git"]; !ok || c.Status != protocol.PreflightCheckStatusPass {
		t.Fatalf("expected executable:git to pass, got %+v", byName["executable:git"])
	}
	if c, ok := byName["repository-access"]; !ok || c.Status != protocol.PreflightCheckStatusPass {
		t.Fatalf("expected repository-access to pass, got %+v", c)
	}
	if c, ok := byName["pr-permissions"]; !ok || c.Status != protocol.PreflightCheckStatusSkipped {
		t.Fatalf("expected pr-permissions to be honestly reported skipped, not faked, got %+v", c)
	}
	if c, ok := byName["external-service:some-service"]; !ok || c.Status != protocol.PreflightCheckStatusSkipped {
		t.Fatalf("expected external-service check to be honestly reported skipped, got %+v", c)
	}
}

func TestRunPreflightMissingExecutableFails(t *testing.T) {
	profile := &protocol.ProjectDeliveryProfile{
		BaseBranch:          "main",
		RequiredExecutables: []string{"definitely-not-a-real-binary-xyz"},
	}
	checks := RunPreflight(context.Background(), profile, nil)
	found := false
	for _, c := range checks {
		if c.Name == "executable:definitely-not-a-real-binary-xyz" {
			found = true
			if c.Status != protocol.PreflightCheckStatusFail {
				t.Fatalf("expected fail status for missing executable, got %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("expected a check for the missing executable")
	}
}
