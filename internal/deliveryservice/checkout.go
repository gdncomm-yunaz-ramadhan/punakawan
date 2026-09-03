package deliveryservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/gitops"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// cloneTimeout bounds the one network operation reconciliation performs.
// A clone that has not finished by then is abandoned rather than holding
// start_delivery open indefinitely; the delivery is already recorded, and
// the next call for the same source tries again.
const cloneTimeout = 10 * time.Minute

// resolveProjectCheckout answers where project is checked out on this
// machine, and remembers the answer so the next delivery - started from
// anywhere - does not have to work it out again.
//
// A delivery names a repository URL and nothing else, so until now the
// only directory it could work in was whichever one the MCP server
// happened to be started in. The ladder is: the path already recorded,
// then a path the caller stated, then the caller's own workspace, and -
// only when allowClone says so - a clone punakawan makes itself. Every
// candidate is checked by its origin remote rather than by existing: a
// directory that is no longer that repository is not an answer.
//
// Starting a delivery does not clone. Cloning reaches the network and
// writes a whole repository to disk, which is not something to do as a
// side effect of recording work; it happens when the delivery is actually
// given somewhere to work, which is a step somebody has agreed to.
func (s *Service) resolveProjectCheckout(ctx context.Context, project *protocol.DeliveryProject, draft ProjectDraft, workspaceRoot string, allowClone bool) (path string, cloned bool, err error) {
	identity := delivery.RepositoryIdentity(project.RepositoryUrl)

	if profile, perr := s.deliveries.GetDeliveryProfile(ctx, project.Id); perr == nil {
		if profile.LocalPath != nil && matchesRepository(ctx, *profile.LocalPath, identity) {
			return *profile.LocalPath, false, nil
		}
	} else if !errors.Is(perr, delivery.ErrNotFound) {
		return "", false, perr
	}

	for _, candidate := range []string{draft.LocalPath, workspaceRoot} {
		if matchesRepository(ctx, candidate, identity) {
			abs, aerr := filepath.Abs(candidate)
			if aerr != nil {
				return "", false, aerr
			}
			return abs, false, s.rememberCheckout(ctx, project, draft, abs)
		}
	}

	if !allowClone {
		return "", false, fmt.Errorf("no checkout of %s is known on this machine", project.RepositoryUrl)
	}
	if strings.TrimSpace(project.RepositoryUrl) == "" {
		return "", false, fmt.Errorf("project %q names no repository url to clone", project.Slug)
	}
	target, cerr := cloneCheckout(ctx, project.RepositoryUrl, project.Slug)
	if cerr != nil {
		return "", false, cerr
	}
	return target, true, s.rememberCheckout(ctx, project, draft, target)
}

// rememberCheckout records path against the project so it is found from
// any directory next time.
func (s *Service) rememberCheckout(ctx context.Context, project *protocol.DeliveryProject, draft ProjectDraft, path string) error {
	baseBranch := strings.TrimSpace(draft.DefaultBranch)
	if baseBranch == "" && project.DefaultBranch != nil {
		baseBranch = *project.DefaultBranch
	}
	if baseBranch == "" {
		baseBranch = "main"
	}
	return s.deliveries.RememberProjectCheckout(ctx, "checkout:"+project.Id+":"+path, project.Id, path, project.RepositoryUrl, baseBranch)
}

// matchesRepository reports whether dir is a checkout of the repository
// identity names. A path that exists but points somewhere else is not a
// match: the recorded path outliving the checkout it named is exactly the
// case that would otherwise send a delivery's work into the wrong tree.
func matchesRepository(ctx context.Context, dir, identity string) bool {
	if strings.TrimSpace(dir) == "" || identity == "" {
		return false
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return false
	}
	remote, ok := gitops.OriginRemote(ctx, dir)
	if !ok {
		return false
	}
	return delivery.RepositoryIdentity(remote) == identity
}

// cloneCheckout clones repositoryURL into the machine's checkouts
// directory, under a name a human can recognize plus a hash of the URL so
// two repositories with the same last path segment never collide. An
// existing clone at that path is reused.
//
// Credentials are whatever the ambient git setup provides: punakawan
// injects no token into a clone it did not have to authenticate itself.
func cloneCheckout(ctx context.Context, repositoryURL, slug string) (string, error) {
	root, err := storage.CheckoutsDir()
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, checkoutDirName(repositoryURL, slug))
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return target, nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("clone %s: git is not available: %w", repositoryURL, err)
	}
	cloneCtx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()
	out, err := exec.CommandContext(cloneCtx, "git", "clone", repositoryURL, target).CombinedOutput()
	if err != nil {
		// A failed clone leaves a partial directory behind, which would
		// then be reused as if it were a checkout.
		_ = os.RemoveAll(target)
		return "", fmt.Errorf("clone %s: %w: %s", repositoryURL, err, strings.TrimSpace(string(out)))
	}
	return target, nil
}

// checkoutDirName is a readable, collision-free directory name for one
// repository.
func checkoutDirName(repositoryURL, slug string) string {
	name := strings.TrimSpace(slug)
	if name == "" {
		name = "repository"
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, name)
	sum := sha256.Sum256([]byte(delivery.RepositoryIdentity(repositoryURL)))
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}
