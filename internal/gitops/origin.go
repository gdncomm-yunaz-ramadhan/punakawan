package gitops

import (
	"context"
	"os/exec"
	"strings"
)

// OriginRemote returns the "origin" remote URL of the checkout at dir.
//
// ok is false for anything that means "this is not a checkout of
// something": no directory, not a git repository, no origin remote. None
// of those is an error a caller can act on differently, and every caller
// here is asking the same question - is this the repository I am looking
// for - so they are one answer.
//
// It resolves git by name only, never from a configured absolute path, so
// a stale setting elsewhere can never point this at a different git than
// the environment's own.
func OriginRemote(ctx context.Context, dir string) (string, bool) {
	if strings.TrimSpace(dir) == "" {
		return "", false
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", false
	}
	remote := strings.TrimSpace(string(out))
	return remote, remote != ""
}
