// This file extends Inspector with the git-capability detection
// punakawan-architecture-enhancement-plan.md §7.2-7.5 (AEP-M3) describes:
// remote/provider classification (punokawan-hp7), default branch/auth/push
// detection (punokawan-a44), and assembling + merging the resulting
// protocol.GitCapabilities/GitExecutionPolicy (punokawan-nzg).
package gitops

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// Remotes runs `git remote -v` in repoPath and returns one entry per remote
// name, merging its fetch and push URLs (git prints a separate line for
// each, usually identical).
func (i *Inspector) Remotes(ctx context.Context, repoPath string) ([]protocol.GitCapabilitiesRemotesElem, error) {
	res, err := i.run(ctx, repoPath, "remote", "-v")
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("gitops: git remote -v: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return parseRemotes(string(res.Stdout)), nil
}

// parseRemotes parses `git remote -v` output, e.g.:
//
//	origin	git@github.com:acme/widgets.git (fetch)
//	origin	git@github.com:acme/widgets.git (push)
func parseRemotes(out string) []protocol.GitCapabilitiesRemotesElem {
	order := []string{}
	byName := map[string]*protocol.GitCapabilitiesRemotesElem{}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		name, remoteURL, kind := fields[0], fields[1], strings.Trim(fields[2], "()")

		r, ok := byName[name]
		if !ok {
			r = &protocol.GitCapabilitiesRemotesElem{Name: name}
			byName[name] = r
			order = append(order, name)
		}
		switch kind {
		case "fetch":
			r.FetchUrl = remoteURL
		case "push":
			pushURL := remoteURL
			r.PushUrl = &pushURL
		}
	}

	out2 := make([]protocol.GitCapabilitiesRemotesElem, 0, len(order))
	for _, name := range order {
		out2 = append(out2, *byName[name])
	}
	return out2
}

// DetectProvider classifies a remote URL's host into a known provider
// (§7.3's GitCapabilities.provider), recognizing both SSH
// (git@host:owner/repo.git) and HTTPS (https://host/owner/repo.git) remote
// URL forms. Anything it cannot confidently classify (including a URL it
// cannot parse at all) is "generic" - a self-hosted GitLab/Bitbucket
// instance at a custom domain looks identical to a truly unknown host from
// the URL alone, and guessing wrong is worse than admitting "generic".
func DetectProvider(remoteURL string) protocol.GitCapabilitiesProvider {
	host := remoteHost(remoteURL)
	switch {
	case strings.Contains(host, "github"):
		return protocol.GitCapabilitiesProviderGithub
	case strings.Contains(host, "gitlab"):
		return protocol.GitCapabilitiesProviderGitlab
	case strings.Contains(host, "bitbucket"):
		return protocol.GitCapabilitiesProviderBitbucket
	default:
		return protocol.GitCapabilitiesProviderGeneric
	}
}

// remoteHost extracts the host from a git remote URL, handling both the
// scp-like SSH shorthand (user@host:path, no scheme) that url.Parse cannot
// parse correctly and normal scheme://host/path URLs (https, ssh, git).
func remoteHost(remoteURL string) string {
	if at := strings.Index(remoteURL, "@"); at != -1 && !strings.Contains(remoteURL, "://") {
		rest := remoteURL[at+1:]
		if colon := strings.Index(rest, ":"); colon != -1 {
			return strings.ToLower(rest[:colon])
		}
		if slash := strings.Index(rest, "/"); slash != -1 {
			return strings.ToLower(rest[:slash])
		}
		return strings.ToLower(rest)
	}
	u, err := url.Parse(remoteURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// RepositoryRoot runs `git rev-parse --show-toplevel` in repoPath. For a
// bare repository (which has no working tree) this fails; callers that
// need to handle bare repositories should check IsBareRepository first.
func (i *Inspector) RepositoryRoot(ctx context.Context, repoPath string) (string, error) {
	res, err := i.run(ctx, repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("gitops: git rev-parse --show-toplevel: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// IsBareRepository runs `git rev-parse --is-bare-repository` in repoPath.
func (i *Inspector) IsBareRepository(ctx context.Context, repoPath string) (bool, error) {
	res, err := i.run(ctx, repoPath, "rev-parse", "--is-bare-repository")
	if err != nil {
		return false, err
	}
	if res.ExitCode != 0 {
		return false, fmt.Errorf("gitops: git rev-parse --is-bare-repository: exit %d: %s", res.ExitCode, res.Stderr)
	}
	return strings.TrimSpace(string(res.Stdout)) == "true", nil
}

// IsWorktree reports whether repoPath is a linked worktree (as created by
// `git worktree add`) rather than a repository's main working tree: a
// linked worktree's `.git` is a file containing a "gitdir:" pointer, while
// a main working tree's `.git` is a directory. `git rev-parse --git-dir`
// resolves to that pointer's target either way, so comparing it against
// `--git-common-dir` (which always resolves to the shared repository's own
// .git) tells them apart: they differ only for a linked worktree.
func (i *Inspector) IsWorktree(ctx context.Context, repoPath string) (bool, error) {
	gitDir, err := i.gitDirVariant(ctx, repoPath, "--git-dir")
	if err != nil {
		return false, err
	}
	commonDir, err := i.gitDirVariant(ctx, repoPath, "--git-common-dir")
	if err != nil {
		return false, err
	}
	return gitDir != commonDir, nil
}

func (i *Inspector) gitDirVariant(ctx context.Context, repoPath, flag string) (string, error) {
	res, err := i.run(ctx, repoPath, "rev-parse", flag)
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("gitops: git rev-parse %s: exit %d: %s", flag, res.ExitCode, res.Stderr)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// DefaultBranch resolves remote's default branch via `git symbolic-ref
// refs/remotes/<remote>/HEAD`, which git sets automatically on clone and
// updates via `git remote set-head`. Returns "", nil (not an error) if it
// cannot be resolved - e.g. no such remote, or origin/HEAD was never set
// (common for a repository created via `git init` rather than `git
// clone`) - since the caller decides whether that absence is fatal.
func (i *Inspector) DefaultBranch(ctx context.Context, repoPath, remote string) (string, error) {
	res, err := i.run(ctx, repoPath, "symbolic-ref", "refs/remotes/"+remote+"/HEAD")
	if err != nil {
		return "", err
	}
	if res.ExitCode != 0 {
		return "", nil
	}
	ref := strings.TrimSpace(string(res.Stdout))
	return strings.TrimPrefix(ref, "refs/remotes/"+remote+"/"), nil
}

// CanPush reports whether repoPath currently has push access to remote for
// branch, tested via `git push --dry-run` - a dry run negotiates with the
// remote and surfaces an authentication/permission failure without
// changing anything on either side, which is the only way to answer this
// accurately short of parsing provider-specific token scopes (which
// Punakawan has no adapter for yet; AEP-M4 provider adapters cover the
// PR-permission side of §7.2 instead). On failure, reason is the remote's
// own stderr, trimmed, so a caller can show *why* push access is missing
// (no auth vs. no permission vs. unknown ref) instead of just that it is.
func (i *Inspector) CanPush(ctx context.Context, repoPath, remote, branch string) (bool, string, error) {
	res, err := i.run(ctx, repoPath, "push", "--dry-run", remote, branch)
	if err != nil {
		return false, "", err
	}
	if res.ExitCode == 0 {
		return true, "", nil
	}
	return false, strings.TrimSpace(string(res.Stderr)), nil
}

// DetectCapabilities assembles a protocol.GitCapabilities for repoPath,
// per §7.2's inspection checklist. remote defaults to "origin" if empty.
// PR-related capabilities (create/read/comment) are always false with a
// limitation noting why: no provider adapter exists yet to check them
// (AEP-M4 builds that on top of the provider this detects).
func (i *Inspector) DetectCapabilities(ctx context.Context, repoPath, remote string) (protocol.GitCapabilities, error) {
	if remote == "" {
		remote = "origin"
	}

	caps := protocol.GitCapabilities{
		Detected:    true,
		Remotes:     []protocol.GitCapabilitiesRemotesElem{},
		Limitations: []string{},
	}

	root, err := i.RepositoryRoot(ctx, repoPath)
	if err == nil {
		caps.RepositoryRoot = &root
	}

	isBare, err := i.IsBareRepository(ctx, repoPath)
	if err != nil {
		return protocol.GitCapabilities{}, err
	}
	caps.IsBareRepository = &isBare

	if !isBare {
		isWorktree, err := i.IsWorktree(ctx, repoPath)
		if err != nil {
			return protocol.GitCapabilities{}, err
		}
		caps.IsWorktree = &isWorktree

		status, err := i.Status(ctx, repoPath)
		if err != nil {
			return protocol.GitCapabilities{}, err
		}
		caps.HasUncommittedChanges = len(status.ChangedFiles) > 0
		caps.HasUntrackedFiles = len(status.UntrackedFiles) > 0
		caps.DetachedHead = status.Branch == ""
		if status.Branch != "" {
			branch := status.Branch
			caps.CurrentBranch = &branch
		}
	}

	remotes, err := i.Remotes(ctx, repoPath)
	if err != nil {
		return protocol.GitCapabilities{}, err
	}
	caps.Remotes = remotes

	var origin *protocol.GitCapabilitiesRemotesElem
	for idx := range remotes {
		if remotes[idx].Name == remote {
			origin = &remotes[idx]
			break
		}
	}

	pushCapable := false
	if origin != nil {
		provider := DetectProvider(origin.FetchUrl)
		caps.Provider = &provider

		if defaultBranch, err := i.DefaultBranch(ctx, repoPath, remote); err == nil && defaultBranch != "" {
			caps.DefaultBranch = &defaultBranch
		}

		branchToTest := remote
		if caps.CurrentBranch != nil {
			branchToTest = *caps.CurrentBranch
		}
		if ok, reason, err := i.CanPush(ctx, repoPath, remote, branchToTest); err == nil {
			pushCapable = ok
			if !ok && reason != "" {
				caps.Limitations = append(caps.Limitations, fmt.Sprintf("push to %q: %s", remote, reason))
			}
		}
	} else {
		caps.Limitations = append(caps.Limitations, fmt.Sprintf("no %q remote configured", remote))
	}

	caps.Limitations = append(caps.Limitations, "PR read/create/comment permissions cannot be checked: no provider adapter is configured yet")

	caps.Capabilities = protocol.GitCapabilitiesCapabilities{
		InspectHistory:     true,
		CreateBranch:       !isBare,
		CreateWorktree:     !isBare,
		Commit:             !isBare,
		Push:               pushCapable,
		CreatePullRequest:  false,
		ReadPullRequest:    false,
		CommentPullRequest: false,
	}
	return caps, nil
}

// MergeExecutionPolicy computes §7.4's effective behavior = detected
// capabilities ∩ repository policy ∩ user permission: each
// GitExecutionPolicy field is true only when detected, repoAllows, and
// userAllows all agree (any explicit false anywhere wins). source and
// reason are taken from whichever input is the most restrictive on
// allow_push - the field most callers (AEP-M4's create_pr in particular)
// actually branch on - falling back to "default" when detected, repo
// policy, and user permission all agree at their own defaults.
func MergeExecutionPolicy(detected protocol.GitCapabilities, repoPolicy, userPermission protocol.GitExecutionPolicy) protocol.GitExecutionPolicy {
	merged := protocol.GitExecutionPolicy{
		SkipGit:                  repoPolicy.SkipGit || userPermission.SkipGit,
		AllowBranchCreation:      detected.Capabilities.CreateBranch && repoPolicy.AllowBranchCreation && userPermission.AllowBranchCreation,
		AllowWorktreeCreation:    detected.Capabilities.CreateWorktree && repoPolicy.AllowWorktreeCreation && userPermission.AllowWorktreeCreation,
		AllowCommit:              detected.Capabilities.Commit && repoPolicy.AllowCommit && userPermission.AllowCommit,
		AllowPush:                detected.Capabilities.Push && repoPolicy.AllowPush && userPermission.AllowPush,
		AllowPullRequestCreation: detected.Capabilities.CreatePullRequest && repoPolicy.AllowPullRequestCreation && userPermission.AllowPullRequestCreation,
	}

	switch {
	case !userPermission.AllowPush:
		merged.Source = protocol.GitExecutionPolicySourceUser
		merged.Reason = restrictionReason(userPermission, "user permission")
	case !repoPolicy.AllowPush:
		merged.Source = protocol.GitExecutionPolicySourceRepositoryPolicy
		merged.Reason = restrictionReason(repoPolicy, "repository policy")
	default:
		merged.Source = protocol.GitExecutionPolicySourceDefault
	}
	return merged
}

func restrictionReason(p protocol.GitExecutionPolicy, sourceLabel string) *string {
	if p.Reason != nil && *p.Reason != "" {
		return p.Reason
	}
	reason := fmt.Sprintf("push disallowed by %s", sourceLabel)
	return &reason
}
